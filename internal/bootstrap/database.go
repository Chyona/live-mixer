package bootstrap

import (
	"fmt"
	"time"

	"live-mixer/internal/config"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// InitDatabase 初始化 PostgreSQL 数据库连接（GORM）。
func InitDatabase(cfg config.DatabaseConfig, logger *zap.Logger) (*gorm.DB, error) {
	gormLog := gormlogger.Default.LogMode(gormlogger.Info)

	// PreferSimpleProtocol 关闭隐式预处理语句缓存，避免表结构变更后出现
	// "cached plan must not change result type (SQLSTATE 0A000)"。
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  cfg.DSN(),
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: gormLog,
	})
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取底层 sql.DB 失败: %w", err)
	}

	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)

	logger.Info("PostgreSQL 连接成功",
		zap.String("host", cfg.Host),
		zap.Int("port", cfg.Port),
		zap.String("dbname", cfg.DBName),
	)
	return db, nil
}
