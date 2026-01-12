package model

// PublishedArticle 博客项目的已发表文章模型（只读）
type PublishedArticle struct {
	ID       int64  `gorm:"primaryKey"`
	Title    string `gorm:"type:longtext"`
	Content  string `gorm:"type:longtext"`
	AuthorID int64  `gorm:"index"`
	Status   uint8
	Ctime    int64
	Utime    int64 `gorm:"index"`
}

// TableName 指定表名
func (PublishedArticle) TableName() string {
	return "published_articles"
}
