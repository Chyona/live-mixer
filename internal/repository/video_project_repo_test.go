package repository

import (
	"context"
	"testing"
	"time"

	"live-mixer/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupVideoProjectTestDB 创建内存 SQLite 数据库并迁移剪辑项目相关表。
func setupVideoProjectTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}, &model.VideoProject{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

// seedLiveMaterialForProject 插入一条直播素材供剪辑项目关联。
func seedLiveMaterialForProject(t *testing.T, db *gorm.DB) *model.LiveMaterial {
	t.Helper()
	material := &model.LiveMaterial{
		Name: "素材", LiveURL: "https://example.com/a.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	if err := db.Create(material).Error; err != nil {
		t.Fatalf("create live_material: %v", err)
	}
	return material
}

// TestVideoProjectRepository_CreateAndGetByID 验证创建与按 ID 查询。
func TestVideoProjectRepository_CreateAndGetByID(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()
	material := seedLiveMaterialForProject(t, db)

	project := &model.VideoProject{
		Name: "项目A", Remark: "备注", LiveID: material.ID,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1,
	}
	if err := repo.Create(ctx, project); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.GetByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Name != "项目A" {
		t.Errorf("Name = %q, want 项目A", got.Name)
	}
}

// TestVideoProjectRepository_Update_OnlyUpdatesAllowedFields 验证仅更新允许编辑的字段。
func TestVideoProjectRepository_Update_OnlyUpdatesAllowedFields(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()
	material := seedLiveMaterialForProject(t, db)

	project := &model.VideoProject{
		Name: "旧名称", LiveID: material.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1,
	}
	if err := repo.Create(ctx, project); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	project.Name = "新名称"
	project.Remark = "新备注"
	project.PromptID = 8
	project.ProjectSource = "manual"
	project.LiveID = 999
	project.CreatedBy = 99
	if err := repo.Update(ctx, project); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, _ := repo.GetByID(ctx, project.ID)
	if got.Name != "新名称" || got.Remark != "新备注" {
		t.Errorf("unexpected updated fields: %+v", got)
	}
	if got.PromptID != 8 {
		t.Errorf("PromptID = %d, want 8", got.PromptID)
	}
	if got.ProjectSource != "manual" {
		t.Errorf("ProjectSource = %q, want manual", got.ProjectSource)
	}
	if got.LiveID != material.ID || got.CreatedBy != 1 {
		t.Errorf("live_id/created_by should remain unchanged: %+v", got)
	}
}

// TestVideoProjectRepository_List_KeywordAndDateFilter 验证关键词与日期筛选。
func TestVideoProjectRepository_List_KeywordAndDateFilter(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()
	material := seedLiveMaterialForProject(t, db)

	inRange := &model.VideoProject{
		Name: "发布会剪辑", Remark: "2026春季", LiveID: material.ID,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1,
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	outRange := &model.VideoProject{
		Name: "其它项目", Remark: "无关", LiveID: material.ID,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1,
		CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, p := range []*model.VideoProject{inRange, outRange} {
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	startAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	projects, total, err := repo.List(ctx, VideoProjectListFilter{
		StartAt:  &startAt,
		EndAt:    &endAt,
		Keywords: []string{"发布会", "2026"},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(projects) != 1 || projects[0].Name != "发布会剪辑" {
		t.Errorf("unexpected result: total=%d projects=%+v", total, projects)
	}
	if projects[0].LiveName != "素材" {
		t.Errorf("LiveName = %q, want 素材", projects[0].LiveName)
	}
	if projects[0].LiveID != material.ID {
		t.Errorf("LiveID = %d, want %d", projects[0].LiveID, material.ID)
	}
}

// TestVideoProjectRepository_Delete 验证物理删除。
func TestVideoProjectRepository_Delete(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()
	material := seedLiveMaterialForProject(t, db)

	project := &model.VideoProject{
		Name: "待删除", LiveID: material.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1,
	}
	if err := repo.Create(ctx, project); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Delete(ctx, project.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.GetByID(ctx, project.ID); err == nil {
		t.Fatal("project should be deleted")
	}
}
