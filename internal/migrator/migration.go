// Package migrator 数据库表初始化，仅由 envinit 调用。
package migrator

import (
	"fmt"

	"live-mixer/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// InitSchema 使用 GORM AutoMigrate 初始化数据库表结构。
func InitSchema(db *gorm.DB, logger *zap.Logger) error {
	logger.Info("开始初始化数据库表结构...")
	err := db.AutoMigrate(
		&model.Account{},
		&model.LiveMaterial{},
		&model.VideoProject{},
		&model.LLMSystemPrompt{},
		&model.Task{},
	)
	if err != nil {
		return fmt.Errorf("数据库表初始化失败: %w", err)
	}
	logger.Info("数据库表初始化完成")
	return nil
}
