// Package migrator 数据库表初始化，仅由 envinit 调用。
package migrator

import (
	"fmt"

	"live-mixer/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// videoProjectMetaColumns 为 video_project 短视频标题/描述/话题列。
// envinit 不执行 migrations/*.sql；已有表上 GORM AutoMigrate 可能漏加列，需显式补齐。
var videoProjectMetaColumns = []struct {
	name string
	pg   string
}{
	{name: "title", pg: "VARCHAR(64) NOT NULL DEFAULT ''"},
	{name: "description", pg: "VARCHAR(512) NOT NULL DEFAULT ''"},
	{name: "topics", pg: "JSONB NOT NULL DEFAULT '[]'"},
}

// InitSchema 使用 GORM AutoMigrate 初始化数据库表结构，并补齐 video_project 元数据列。
func InitSchema(db *gorm.DB, logger *zap.Logger) error {
	logger.Info("开始初始化数据库表结构...")
	migrateErr := db.AutoMigrate(allModels()...)
	if err := ensureVideoProjectMetaColumns(db, logger); err != nil {
		if migrateErr != nil {
			return fmt.Errorf("数据库表初始化失败: %w; %v", migrateErr, err)
		}
		return fmt.Errorf("数据库表初始化失败: %w", err)
	}
	if migrateErr != nil {
		return fmt.Errorf("数据库表初始化失败: %w", migrateErr)
	}
	logger.Info("数据库表初始化完成")
	return nil
}

// ensureVideoProjectMetaColumns 在 AutoMigrate 之后强制补齐 title/description/topics。
// PostgreSQL 使用 ADD COLUMN IF NOT EXISTS，不依赖 GORM 是否判定列已存在。
func ensureVideoProjectMetaColumns(db *gorm.DB, logger *zap.Logger) error {
	vp := &model.VideoProject{}
	if !db.Migrator().HasTable(vp) {
		return fmt.Errorf("video_project 表不存在，无法补齐 title/description/topics")
	}

	if db.Dialector.Name() == "postgres" {
		for _, col := range videoProjectMetaColumns {
			existed := db.Migrator().HasColumn(vp, col.name)
			if err := db.Exec(
				"ALTER TABLE ? ADD COLUMN IF NOT EXISTS ? "+col.pg,
				clause.Table{Name: "video_project"},
				clause.Column{Name: col.name},
			).Error; err != nil {
				return fmt.Errorf("补齐 video_project.%s 失败: %w", col.name, err)
			}
			if !existed {
				logger.Info("已补齐 video_project 列", zap.String("column", col.name))
			}
		}
	}

	for _, col := range videoProjectMetaColumns {
		if db.Migrator().HasColumn(vp, col.name) {
			continue
		}
		logger.Warn("video_project 缺少列，开始补齐", zap.String("column", col.name))
		if err := db.Migrator().AddColumn(vp, col.name); err != nil {
			return fmt.Errorf("补齐 video_project.%s 失败: %w", col.name, err)
		}
		if !db.Migrator().HasColumn(vp, col.name) {
			return fmt.Errorf("补齐后 video_project 仍缺少列 %s（请确认 envinit 已用当前代码重新编译）", col.name)
		}
		logger.Info("已补齐 video_project 列", zap.String("column", col.name))
	}
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
