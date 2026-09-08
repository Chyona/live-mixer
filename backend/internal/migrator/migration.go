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
	if err := ensureLiveMaterialIngestSchema(db, logger); err != nil {
		if migrateErr != nil {
			return fmt.Errorf("数据库表初始化失败: %w; %v", migrateErr, err)
		}
		return fmt.Errorf("数据库表初始化失败: %w", err)
	}
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

var liveMaterialIngestColumns = []struct {
	name string
	pg   string
}{
	{name: "m3u8_url", pg: "VARCHAR(1024) NOT NULL DEFAULT ''"},
	{name: "record_uuid", pg: "VARCHAR(64) NOT NULL DEFAULT ''"},
	{name: "record_playlist_url", pg: "VARCHAR(2048) NOT NULL DEFAULT ''"},
	{name: "source_mode", pg: "VARCHAR(16) NOT NULL DEFAULT 'replay'"},
	{name: "scheduled_at", pg: "TIMESTAMPTZ"},
	{name: "wait_deadline_at", pg: "TIMESTAMPTZ"},
	{name: "connect_deadline_at", pg: "TIMESTAMPTZ"},
	{name: "stream_started_at", pg: "TIMESTAMPTZ"},
	{name: "asr_cursor_ms", pg: "BIGINT NOT NULL DEFAULT 0"},
	{name: "ingest_epoch", pg: "BIGINT NOT NULL DEFAULT 0"},
	{name: "next_seg", pg: "BIGINT NOT NULL DEFAULT 0"},
	{name: "last_heartbeat_at", pg: "TIMESTAMPTZ"},
	{name: "ingest_error_msg", pg: "TEXT"},
}

// ensureLiveMaterialIngestSchema 补齐跟播列，并把 live_url/m3u8_url 改为部分唯一（失败记录可重试）。
func ensureLiveMaterialIngestSchema(db *gorm.DB, logger *zap.Logger) error {
	lm := &model.LiveMaterial{}
	if !db.Migrator().HasTable(lm) {
		return fmt.Errorf("live_material 表不存在，无法补齐跟播字段")
	}

	if db.Dialector.Name() == "postgres" {
		for _, col := range liveMaterialIngestColumns {
			if err := db.Exec(
				"ALTER TABLE ? ADD COLUMN IF NOT EXISTS ? "+col.pg,
				clause.Table{Name: "live_material"},
				clause.Column{Name: col.name},
			).Error; err != nil {
				return fmt.Errorf("补齐 live_material.%s 失败: %w", col.name, err)
			}
		}
		if err := db.Exec(`ALTER TABLE live_material ALTER COLUMN live_url SET DEFAULT ''`).Error; err != nil {
			return fmt.Errorf("设置 live_url 默认值失败: %w", err)
		}
		_ = db.Exec(`ALTER TABLE live_material DROP CONSTRAINT IF EXISTS chk_live_material_live_status`).Error
		if err := db.Exec(`ALTER TABLE live_material ADD CONSTRAINT chk_live_material_live_status CHECK (live_status IN ('none', 'waiting', 'connecting', 'live', 'ending', 'ended', 'failed'))`).Error; err != nil {
			logger.Warn("更新 live_status 约束失败（可能已是新约束）", zap.Error(err))
		}
		_ = db.Exec(`ALTER TABLE live_material DROP CONSTRAINT IF EXISTS chk_live_material_source_mode`).Error
		if err := db.Exec(`ALTER TABLE live_material ADD CONSTRAINT chk_live_material_source_mode CHECK (source_mode IN ('upcoming', 'live', 'replay'))`).Error; err != nil {
			logger.Warn("更新 source_mode 约束失败（可能已是新约束）", zap.Error(err))
		}
		if err := db.Exec(`DROP INDEX IF EXISTS idx_live_material_live_url`).Error; err != nil {
			return fmt.Errorf("删除旧 live_url 唯一索引失败: %w", err)
		}
		if err := db.Exec(`DROP INDEX IF EXISTS uni_live_material_live_url`).Error; err != nil {
			return fmt.Errorf("删除 GORM live_url 唯一索引失败: %w", err)
		}
		if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_live_material_live_url ON live_material (live_url) WHERE live_url <> '' AND live_status <> 'failed'`).Error; err != nil {
			return fmt.Errorf("创建 live_url 部分唯一索引失败: %w", err)
		}
		if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_live_material_m3u8_url ON live_material (m3u8_url) WHERE m3u8_url <> '' AND live_status <> 'failed'`).Error; err != nil {
			return fmt.Errorf("创建 m3u8_url 部分唯一索引失败: %w", err)
		}
		if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_live_material_ingest_claim ON live_material (live_status, scheduled_at, last_heartbeat_at)`).Error; err != nil {
			return fmt.Errorf("创建跟播抢占索引失败: %w", err)
		}
	}

	for _, col := range liveMaterialIngestColumns {
		if db.Migrator().HasColumn(lm, col.name) {
			continue
		}
		if err := db.Migrator().AddColumn(lm, col.name); err != nil {
			return fmt.Errorf("补齐 live_material.%s 失败: %w", col.name, err)
		}
		logger.Info("已补齐 live_material 列", zap.String("column", col.name))
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
