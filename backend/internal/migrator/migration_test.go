package migrator

import (
	"sync"
	"testing"
	"time"

	"live-mixer/internal/model"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// legacyVideoProject 模拟升级前不含 title/description/topics 的表结构。
type legacyVideoProject struct {
	ID             uint   `gorm:"primaryKey"`
	Name           string `gorm:"size:64"`
	Remark         string `gorm:"size:256"`
	LiveID         uint   `gorm:"column:live_id;not null;index"`
	PromptID       uint   `gorm:"column:prompt_id;not null;default:1"`
	Clips0         string `gorm:"column:clips0"`
	Clips1         string `gorm:"column:clips1"`
	Width          int    `gorm:"not null;default:0"`
	Height         int    `gorm:"not null;default:0"`
	ProjectSource  string `gorm:"column:project_source;size:32"`
	EnableCaptions int    `gorm:"column:enable_captions;not null;default:1"`
	CreatedBy      uint   `gorm:"not null;index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Ext            string `gorm:"size:1024"`
}

func (legacyVideoProject) TableName() string { return "video_project" }

// setupMigratorTestDB 创建内存 SQLite，供 migrator 单元测试使用。
func setupMigratorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// TestInitSchema 验证建表后所有业务表均存在。
func TestInitSchema(t *testing.T) {
	db := setupMigratorTestDB(t)
	logger := zap.NewNop()

	if err := InitSchema(db, logger); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	for _, m := range allModels() {
		if !db.Migrator().HasTable(m) {
			t.Errorf("InitSchema() 后表 %T 不存在", m)
		}
	}
	assertVideoProjectMetaColumns(t, db)
}

func assertVideoProjectMetaColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	vp := &model.VideoProject{}
	for _, col := range []string{"title", "description", "topics"} {
		if !db.Migrator().HasColumn(vp, col) {
			t.Errorf("video_project 缺少列 %s", col)
		}
	}
}

// TestVideoProjectSchemaIncludesMetaColumns 确认 GORM 模型会迁移 title/description/topics。
func TestVideoProjectSchemaIncludesMetaColumns(t *testing.T) {
	s, err := schema.Parse(&model.VideoProject{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("Parse VideoProject: %v", err)
	}
	for _, name := range []string{"title", "description", "topics"} {
		if _, ok := s.FieldsByDBName[name]; !ok {
			t.Errorf("VideoProject schema 不含列 %s", name)
		}
	}
}

// TestInitSchema_AddsVideoProjectMetaColumnsToExistingTable 模拟旧库只有旧表结构，init 必须补齐 3 列。
func TestInitSchema_AddsVideoProjectMetaColumnsToExistingTable(t *testing.T) {
	db := setupMigratorTestDB(t)
	logger := zap.NewNop()

	if err := db.AutoMigrate(&legacyVideoProject{}); err != nil {
		t.Fatalf("create old video_project: %v", err)
	}

	if db.Migrator().HasColumn(&model.VideoProject{}, "title") {
		t.Fatal("前置条件失败：旧表不应已有 title")
	}

	if err := InitSchema(db, logger); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}
	assertVideoProjectMetaColumns(t, db)
}

// TestDropAllTables 验证删表后所有业务表均不存在，且不影响再次建表。
func TestDropAllTables(t *testing.T) {
	db := setupMigratorTestDB(t)
	logger := zap.NewNop()

	if err := InitSchema(db, logger); err != nil {
		t.Fatalf("InitSchema() error = %v", err)
	}

	// 插入数据，确认删表会连带清空数据
	if err := db.Create(&model.Account{
		Username: "u1",
		Email:    "u1@example.com",
		Password: "hashed",
		IsActive: 1,
	}).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	if err := DropAllTables(db, logger); err != nil {
		t.Fatalf("DropAllTables() error = %v", err)
	}

	for _, m := range allModels() {
		if db.Migrator().HasTable(m) {
			t.Errorf("DropAllTables() 后表 %T 仍存在", m)
		}
	}

	// 再次建表应成功
	if err := InitSchema(db, logger); err != nil {
		t.Fatalf("再次 InitSchema() error = %v", err)
	}
	assertVideoProjectMetaColumns(t, db)
	var count int64
	if err := db.Model(&model.Account{}).Count(&count).Error; err != nil {
		t.Fatalf("count account: %v", err)
	}
	if count != 0 {
		t.Errorf("重建表后账号数 = %d, want 0", count)
	}
}

// TestDropAllTables_EmptyDB 验证在空库上删表不报错（幂等）。
func TestDropAllTables_EmptyDB(t *testing.T) {
	db := setupMigratorTestDB(t)
	logger := zap.NewNop()

	if err := DropAllTables(db, logger); err != nil {
		t.Fatalf("DropAllTables() on empty db error = %v", err)
	}
}
