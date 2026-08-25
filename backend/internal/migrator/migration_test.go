package migrator

import (
	"testing"

	"live-mixer/internal/model"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

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
