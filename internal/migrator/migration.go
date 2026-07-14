// Package migrator 数据库表初始化，仅由 envinit 调用。
package migrator

import (
	"fmt"

	"live-mixer/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// allModels 返回需要管理的全部业务表模型（按依赖顺序：被引用方在前）。
// 建表时按此顺序 AutoMigrate；删表时按逆序 Drop，避免外键约束冲突。
func allModels() []any {
	return []any{
		&model.Account{},
		&model.LiveMaterial{},
		&model.VideoProject{},
		&model.LLMSystemPrompt{},
		&model.Task{},
	}
}

// InitSchema 使用 GORM AutoMigrate 初始化数据库表结构。
func InitSchema(db *gorm.DB, logger *zap.Logger) error {
	logger.Info("开始初始化数据库表结构...")
	err := db.AutoMigrate(allModels()...)
	if err != nil {
		return fmt.Errorf("数据库表初始化失败: %w", err)
	}
	logger.Info("数据库表初始化完成")
	return nil
}

// DropAllTables 删除全部业务表及其数据。
// 按依赖逆序删除，确保外键引用表先于被引用表删除。
func DropAllTables(db *gorm.DB, logger *zap.Logger) error {
	logger.Info("开始删除全部数据库表...")
	models := allModels()
	// 逆序删除，避免外键约束导致删除失败
	for i := len(models) - 1; i >= 0; i-- {
		if err := db.Migrator().DropTable(models[i]); err != nil {
			return fmt.Errorf("删除数据表失败: %w", err)
		}
	}
	logger.Info("全部数据库表删除完成")
	return nil
}
