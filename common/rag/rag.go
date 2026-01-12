package rag

import (
	"GopherAI/common/redis"
	redisPkg "GopherAI/common/redis"
	"GopherAI/config"
	"context"
	"fmt"
	"os"
	"strings"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	redisCli "github.com/redis/go-redis/v9"
)

type RAGIndexer struct {
	embedding embedding.Embedder
	indexer   *redisIndexer.Indexer
}

type RAGQuery struct {
	embedding  embedding.Embedder
	retriever  retriever.Retriever   // 单索引检索器（向后兼容）
	retrievers []retriever.Retriever // 多索引检索器
	indexNames []string              // 索引名称列表
}

// 构建知识库索引
// 文本解析、文本切块、向量化、存储向量
func NewRAGIndexer(filename, embeddingModel string) (*RAGIndexer, error) {
	ctx := context.Background()
	apiKey := os.Getenv("OPENAI_API_KEY")
	dimension := config.GetConfig().RagModelConfig.RagDimension

	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: config.GetConfig().RagModelConfig.RagBaseUrl,
		APIKey:  apiKey,
		Model:   embeddingModel,
	}
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	if err := redisPkg.InitRedisIndex(ctx, filename, dimension); err != nil {
		return nil, fmt.Errorf("failed to init redis index: %w", err)
	}

	rdb := redisPkg.Rdb

	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    rdb,
		KeyPrefix: redis.GenerateIndexNamePrefix(filename),
		BatchSize: 10,
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {
			source := ""
			if s, ok := doc.MetaData["source"].(string); ok {
				source = s
			}
			return &redisIndexer.Hashes{
				Key: fmt.Sprintf("%s:%s", filename, doc.ID),
				Field2Value: map[string]redisIndexer.FieldValue{
					"content":  {Value: doc.Content, EmbedKey: "vector"},
					"metadata": {Value: source},
				},
			}, nil
		},
	}
	indexerConfig.Embedding = embedder

	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	return &RAGIndexer{
		embedding: embedder,
		indexer:   idx,
	}, nil
}

// IndexFile 读取文件内容并创建向量索引
func (r *RAGIndexer) IndexFile(ctx context.Context, filePath string) error {
	// 读取文件内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// 将文件内容转换为文档
	// TODO: 这里可以根据需要进行文本切块，目前简单处理为一个文档
	doc := &schema.Document{
		ID:      "doc_1", // 可以使用 UUID 或其他唯一标识
		Content: string(content),
		MetaData: map[string]any{
			"source": filePath,
		},
	}

	// 使用 indexer 存储文档（会自动进行向量化）
	_, err = r.indexer.Store(ctx, []*schema.Document{doc})
	if err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}

	return nil
}

// IndexContent 直接索引文本内容（用于数据库文章等）
func (r *RAGIndexer) IndexContent(ctx context.Context, docID string, content string) error {
	doc := &schema.Document{
		ID:      docID,
		Content: content,
		MetaData: map[string]any{
			"source": "database",
		},
	}

	_, err := r.indexer.Store(ctx, []*schema.Document{doc})
	if err != nil {
		return fmt.Errorf("failed to store document: %w", err)
	}

	return nil
}

// DeleteIndex 删除指定文件的知识库索引（静态方法，不依赖实例）
func DeleteIndex(ctx context.Context, filename string) error {
	if err := redisPkg.DeleteRedisIndex(ctx, filename); err != nil {
		return fmt.Errorf("failed to delete redis index: %w", err)
	}
	return nil
}

// NewRAGQuery 创建 RAG 查询器（用于向量检索和问答）
// 支持从上传文件和同步的博客文章多个来源检索，合并结果
func NewRAGQuery(ctx context.Context, username string) (*RAGQuery, error) {
	cfg := config.GetConfig()
	apiKey := os.Getenv("OPENAI_API_KEY")

	// 创建 embedding 模型
	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: cfg.RagModelConfig.RagBaseUrl,
		APIKey:  apiKey,
		Model:   cfg.RagModelConfig.RagEmbeddingModel,
	}
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// 收集用户的所有索引名称
	var indexNames []string

	// 1. 检查上传的文件
	userDir := fmt.Sprintf("uploads/%s", username)
	files, err := os.ReadDir(userDir)
	if err == nil && len(files) > 0 {
		for _, f := range files {
			if !f.IsDir() {
				indexNames = append(indexNames, redis.GenerateIndexName(f.Name()))
			}
		}
	}

	// 2. 检查用户关联的博客文章索引
	articleIndexes := getUserArticleIndexes(username)
	indexNames = append(indexNames, articleIndexes...)

	if len(indexNames) == 0 {
		return nil, fmt.Errorf("no knowledge base found for user %s (no uploaded file or synced articles)", username)
	}

	rdb := redisPkg.Rdb

	// 为每个索引创建检索器
	var retrievers []retriever.Retriever
	for _, indexName := range indexNames {
		retrieverConfig := &redisRetriever.RetrieverConfig{
			Client:       rdb,
			Index:        indexName,
			Dialect:      2,
			ReturnFields: []string{"content", "metadata", "distance"},
			TopK:         3, // 每个索引取 Top 3
			VectorField:  "vector",
			DocumentConverter: func(ctx context.Context, doc redisCli.Document) (*schema.Document, error) {
				resp := &schema.Document{
					ID:       doc.ID,
					Content:  "",
					MetaData: map[string]any{},
				}
				for field, val := range doc.Fields {
					if field == "content" {
						resp.Content = val
					} else {
						resp.MetaData[field] = val
					}
				}
				return resp, nil
			},
		}
		retrieverConfig.Embedding = embedder

		rtr, err := redisRetriever.NewRetriever(ctx, retrieverConfig)
		if err != nil {
			// 跳过无法创建的检索器，继续尝试其他索引
			continue
		}
		retrievers = append(retrievers, rtr)
	}

	if len(retrievers) == 0 {
		return nil, fmt.Errorf("failed to create any retriever for user %s", username)
	}

	return &RAGQuery{
		embedding:  embedder,
		retrievers: retrievers,
		indexNames: indexNames,
	}, nil
}

// getUserArticleIndexes 获取用户关联的博客文章索引名称
func getUserArticleIndexes(username string) []string {
	// 文章索引命名格式：rag_docs:article_{author_id}_{article_id}:idx
	ctx := context.Background()
	rdb := redisPkg.Rdb

	// 使用 FT._LIST 命令列出所有 RediSearch 索引
	result, err := rdb.Do(ctx, "FT._LIST").Result()
	if err != nil {
		return nil
	}

	// 解析结果
	indexList, ok := result.([]interface{})
	if !ok {
		return nil
	}

	var indexes []string
	for _, idx := range indexList {
		indexName, ok := idx.(string)
		if !ok {
			continue
		}
		// 过滤出 article 开头的索引
		if strings.Contains(indexName, "article_") {
			indexes = append(indexes, indexName)
		}
	}

	return indexes
}

// RetrieveDocuments 从多个索引检索并合并结果
func (r *RAGQuery) RetrieveDocuments(ctx context.Context, query string) ([]*schema.Document, error) {
	var allDocs []*schema.Document

	// 从所有检索器获取结果
	for _, rtr := range r.retrievers {
		docs, err := rtr.Retrieve(ctx, query)
		if err != nil {
			// 单个检索器失败不影响其他
			continue
		}
		allDocs = append(allDocs, docs...)
	}

	if len(allDocs) == 0 {
		return nil, fmt.Errorf("no documents retrieved from any index")
	}

	// 去重（根据内容）并限制返回数量
	seen := make(map[string]bool)
	var uniqueDocs []*schema.Document
	for _, doc := range allDocs {
		if !seen[doc.Content] && doc.Content != "" {
			seen[doc.Content] = true
			uniqueDocs = append(uniqueDocs, doc)
			if len(uniqueDocs) >= 5 { // 最多返回 5 个文档
				break
			}
		}
	}

	return uniqueDocs, nil
}

// BuildRAGPrompt 构建包含检索文档的提示词
func BuildRAGPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}

	contextText := ""
	for i, doc := range docs {
		contextText += fmt.Sprintf("[文档 %d]: %s\n\n", i+1, doc.Content)
	}

	prompt := fmt.Sprintf(`基于以下参考文档回答用户的问题。如果文档中没有相关信息，请说明无法找到相关信息。

参考文档：
%s

用户问题：%s

请提供准确、完整的回答：`, contextText, query)

	return prompt
}
