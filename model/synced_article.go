package model

import "time"

// SyncedArticle 同步到本地的博客文章
type SyncedArticle struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	ExternalID    int64     `gorm:"uniqueIndex" json:"external_id"`  // 外部文章 ID
	AuthorID      int64     `gorm:"index" json:"author_id"`          // 外部作者 ID
	Title         string    `gorm:"type:text" json:"title"`          // 文章标题
	Content       string    `gorm:"type:longtext" json:"content"`    // 文章内容
	Status        uint8     `json:"status"`                          // 发布状态
	ExternalCtime int64     `json:"external_ctime"`                  // 外部创建时间
	ExternalUtime int64     `json:"external_utime"`                  // 外部更新时间
	IsIndexed     bool      `gorm:"default:false" json:"is_indexed"` // 是否已向量化
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TableName 指定表名
func (SyncedArticle) TableName() string {
	return "synced_articles"
}
