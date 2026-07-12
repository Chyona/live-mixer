package repository

import (
	"context"
	"testing"

	"live-mixer/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupLiveMaterialTestDB 创建内存 SQLite 数据库并迁移直播素材表。
func setupLiveMaterialTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// TestLiveMaterialRepository_CreateAndGetByID 验证创建与按 ID 查询。
func TestLiveMaterialRepository_CreateAndGetByID(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name:        "测试素材",
		LiveURL:     "https://example.com/live.mp4",
		Remark:      "备注",
		LiveASR:     "{}",
		ASRStatus:   model.ASRStatusPending,
		ASRProgress: 0,
		CreatedBy:   1,
	}
	if err := repo.Create(ctx, material); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if material.ID == 0 {
		t.Fatal("Create() should set ID")
	}

	got, err := repo.GetByID(ctx, material.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "测试素材" {
		t.Errorf("Name = %q, want 测试素材", got.Name)
	}
}

// TestLiveMaterialRepository_UpdateNameRemark_OnlyUpdatesAllowedFields 验证仅更新 name、remark。
func TestLiveMaterialRepository_UpdateNameRemark_OnlyUpdatesAllowedFields(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name:        "旧名称",
		LiveURL:     "https://example.com/old.mp4",
		Remark:      "旧备注",
		LiveASR:     "{}",
		ASRStatus:   model.ASRStatusPending,
		ASRProgress: 0,
		CreatedBy:   1,
	}
	if err := repo.Create(ctx, material); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// 尝试在内存对象中修改 live_url，但 UpdateNameRemark 不应写入数据库。
	material.Name = "新名称"
	material.Remark = "新备注"
	material.LiveURL = "https://example.com/hacked.mp4"
	if err := repo.UpdateNameRemark(ctx, material); err != nil {
		t.Fatalf("UpdateNameRemark() error = %v", err)
	}

	got, err := repo.GetByID(ctx, material.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "新名称" {
		t.Errorf("Name = %q, want 新名称", got.Name)
	}
	if got.Remark != "新备注" {
		t.Errorf("Remark = %q, want 新备注", got.Remark)
	}
	if got.LiveURL != "https://example.com/old.mp4" {
		t.Errorf("LiveURL = %q, want unchanged old url", got.LiveURL)
	}
}

// TestLiveMaterialRepository_GetByID_NotFound 验证记录不存在时返回 ErrRecordNotFound。
func TestLiveMaterialRepository_GetByID_NotFound(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)

	_, err := repo.GetByID(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for missing record")
	}
	if err != gorm.ErrRecordNotFound {
		t.Errorf("error = %v, want ErrRecordNotFound", err)
	}
}
