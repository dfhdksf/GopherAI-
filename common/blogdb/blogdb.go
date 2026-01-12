package blogdb

import (
	"GopherAI/config"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// BlogDB 博客数据库连接（只读）
var BlogDB *gorm.DB

// InitBlogDB 初始化博客数据库连接
func InitBlogDB() error {
	cfg := config.GetConfig()
	host := cfg.BlogMysqlHost
	port := cfg.BlogMysqlPort
	dbname := cfg.BlogMysqlDatabaseName
	username := cfg.BlogMysqlUser
	password := cfg.BlogMysqlPassword
	charset := cfg.BlogMysqlCharset

	// 如果没有配置博客数据库，跳过初始化
	if host == "" || dbname == "" {
		return nil
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=Local",
		username, password, host, port, dbname, charset)

	var log logger.Interface
	if gin.Mode() == "debug" {
		log = logger.Default.LogMode(logger.Info)
	} else {
		log = logger.Default
	}

	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       dsn,
		DefaultStringSize:         256,
		DisableDatetimePrecision:  true,
		DontSupportRenameIndex:    true,
		DontSupportRenameColumn:   true,
		SkipInitializeWithVersion: false,
	}), &gorm.Config{
		Logger: log,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to blog database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxLifetime(time.Hour)

	BlogDB = db
	return nil
}

// IsInitialized 检查博客数据库是否已初始化
func IsInitialized() bool {
	return BlogDB != nil
}
