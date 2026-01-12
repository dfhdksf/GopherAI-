package articlesync

import (
	"GopherAI/common/blogdb"
	"GopherAI/common/mysql"
	"GopherAI/common/rag"
	"GopherAI/config"
	"GopherAI/model"
	"context"
	"fmt"
	"log"
	"time"
)

const (
	tableName       = "published_articles"
	statusPublished = 2 // 已发表状态
)

// StartSyncService 启动文章同步服务
func StartSyncService() {
	if !blogdb.IsInitialized() {
		log.Println("[ArticleSync] Blog database not configured, sync service disabled")
		return
	}

	log.Println("[ArticleSync] Starting article sync service...")

	// 先执行一次同步
	if err := SyncArticles(); err != nil {
		log.Printf("[ArticleSync] Initial sync failed: %v", err)
	}

	// 定时同步（每分钟）
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			if err := SyncArticles(); err != nil {
				log.Printf("[ArticleSync] Sync failed: %v", err)
			}
		}
	}()
}

// SyncArticles 同步博客文章到本地数据库，然后向量化
func SyncArticles() error {
	ctx := context.Background()

	// 1. 获取上次同步时间
	var syncState model.SyncState
	result := mysql.DB.Where("table_name = ?", tableName).First(&syncState)
	if result.Error != nil {
		// 如果记录不存在，创建新记录
		syncState = model.SyncState{
			TableName: tableName,
			LastUtime: 0,
		}
	}

	// 2. 从外部数据库查询自上次同步后更新的已发表文章
	var externalArticles []model.PublishedArticle
	err := blogdb.BlogDB.
		Where("status = ? AND utime > ?", statusPublished, syncState.LastUtime).
		Order("utime ASC").
		Find(&externalArticles).Error
	if err != nil {
		return fmt.Errorf("failed to query external articles: %w", err)
	}

	if len(externalArticles) == 0 {
		log.Println("[ArticleSync] No new articles to sync")
		return nil
	}

	log.Printf("[ArticleSync] Found %d articles to sync from external database", len(externalArticles))

	// 3. 同步到本地数据库
	var maxUtime int64
	for _, extArticle := range externalArticles {
		// 检查本地是否已存在该文章
		var localArticle model.SyncedArticle
		result := mysql.DB.Where("external_id = ?", extArticle.ID).First(&localArticle)

		if result.Error != nil {
			// 不存在，创建新记录
			localArticle = model.SyncedArticle{
				ExternalID:    extArticle.ID,
				AuthorID:      extArticle.AuthorID,
				Title:         extArticle.Title,
				Content:       extArticle.Content,
				Status:        extArticle.Status,
				ExternalCtime: extArticle.Ctime,
				ExternalUtime: extArticle.Utime,
				IsIndexed:     false,
			}
			if err := mysql.DB.Create(&localArticle).Error; err != nil {
				log.Printf("[ArticleSync] Failed to create local article %d: %v", extArticle.ID, err)
				continue
			}
			log.Printf("[ArticleSync] Created local article: id=%d, title=%s", localArticle.ID, localArticle.Title)
		} else {
			// 已存在，更新内容
			localArticle.Title = extArticle.Title
			localArticle.Content = extArticle.Content
			localArticle.Status = extArticle.Status
			localArticle.ExternalUtime = extArticle.Utime
			localArticle.IsIndexed = false // 内容变化，需要重新索引
			if err := mysql.DB.Save(&localArticle).Error; err != nil {
				log.Printf("[ArticleSync] Failed to update local article %d: %v", extArticle.ID, err)
				continue
			}
			log.Printf("[ArticleSync] Updated local article: id=%d, title=%s", localArticle.ID, localArticle.Title)
		}

		if extArticle.Utime > maxUtime {
			maxUtime = extArticle.Utime
		}
	}

	// 4. 更新同步状态
	if maxUtime > 0 {
		syncState.LastUtime = maxUtime
		syncState.UpdatedAt = time.Now()
		if err := mysql.DB.Save(&syncState).Error; err != nil {
			return fmt.Errorf("failed to update sync state: %w", err)
		}
	}

	// 5. 为未索引的文章创建向量索引
	if err := IndexPendingArticles(ctx); err != nil {
		log.Printf("[ArticleSync] Failed to index articles: %v", err)
	}

	return nil
}

// IndexPendingArticles 为未索引的本地文章创建向量索引
func IndexPendingArticles(ctx context.Context) error {
	// 查询未索引的文章
	var articles []model.SyncedArticle
	if err := mysql.DB.Where("is_indexed = ?", false).Find(&articles).Error; err != nil {
		return fmt.Errorf("failed to query pending articles: %w", err)
	}

	if len(articles) == 0 {
		log.Println("[ArticleSync] No pending articles to index")
		return nil
	}

	log.Printf("[ArticleSync] Indexing %d pending articles", len(articles))

	cfg := config.GetConfig()

	for _, article := range articles {
		// 创建索引名称
		indexName := fmt.Sprintf("article_%d_%d", article.AuthorID, article.ExternalID)

		// 创建 RAG 索引器
		indexer, err := rag.NewRAGIndexer(indexName, cfg.RagModelConfig.RagEmbeddingModel)
		if err != nil {
			log.Printf("[ArticleSync] Failed to create indexer for article %d: %v", article.ID, err)
			continue
		}

		// 索引内容
		content := fmt.Sprintf("标题: %s\n\n%s", article.Title, article.Content)
		if err := indexer.IndexContent(ctx, indexName, content); err != nil {
			log.Printf("[ArticleSync] Failed to index article %d: %v", article.ID, err)
			continue
		}

		// 标记为已索引
		article.IsIndexed = true
		if err := mysql.DB.Save(&article).Error; err != nil {
			log.Printf("[ArticleSync] Failed to mark article %d as indexed: %v", article.ID, err)
			continue
		}

		log.Printf("[ArticleSync] Indexed article: id=%d, external_id=%d, title=%s",
			article.ID, article.ExternalID, article.Title)
	}

	return nil
}

// GetLocalArticles 获取本地同步的文章（用于调试）
func GetLocalArticles() ([]model.SyncedArticle, error) {
	var articles []model.SyncedArticle
	if err := mysql.DB.Find(&articles).Error; err != nil {
		return nil, err
	}
	return articles, nil
}

// GetLocalArticlesByAuthor 获取指定作者的本地文章
func GetLocalArticlesByAuthor(authorID int64) ([]model.SyncedArticle, error) {
	var articles []model.SyncedArticle
	if err := mysql.DB.Where("author_id = ?", authorID).Find(&articles).Error; err != nil {
		return nil, err
	}
	return articles, nil
}
