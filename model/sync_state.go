package model

import "time"

// SyncState 同步状态记录
type SyncState struct {
	ID        int64     `gorm:"primaryKey"`
	TableName string    `gorm:"type:varchar(100);uniqueIndex"` // 同步的表名
	LastUtime int64     // 上次同步的 utime
	UpdatedAt time.Time // 更新时间
}
