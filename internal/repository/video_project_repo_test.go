package repository

import (
	"context"
	"fmt"
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
	if err := db.AutoMigrate(&model.LiveMaterial{}, &model.VideoProject{}, &model.Task{}); err != nil {
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
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
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
	if got.EnableCaptions != model.EnableCaptionsOn {
		t.Errorf("EnableCaptions = %d, want 1", got.EnableCaptions)
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
	project.EnableCaptions = model.EnableCaptionsOff
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
	if got.EnableCaptions != model.EnableCaptionsOff {
		t.Errorf("EnableCaptions = %d, want 0", got.EnableCaptions)
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
		Keywords: KeywordGroups{{"发布会", "2026"}},
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
	if projects[0].TaskCount != 0 {
		t.Errorf("TaskCount = %d, want 0 when no task", projects[0].TaskCount)
	}
}

// TestVideoProjectRepository_List_KeywordMatchesLiveName 验证 keywords 可匹配源视频名 live_name。
func TestVideoProjectRepository_List_KeywordMatchesLiveName(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()

	m1 := &model.LiveMaterial{
		Name: "春季发布会源视频", LiveURL: "https://example.com/spring.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	m2 := &model.LiveMaterial{
		Name: "其它素材", LiveURL: "https://example.com/other.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	for _, m := range []*model.LiveMaterial{m1, m2} {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create material: %v", err)
		}
	}
	p1 := &model.VideoProject{
		Name: "项目A", Remark: "无关备注", LiveID: m1.ID,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
	}
	p2 := &model.VideoProject{
		Name: "项目B", Remark: "无关备注", LiveID: m2.ID,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
	}
	for _, p := range []*model.VideoProject{p1, p2} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create project: %v", err)
		}
	}

	projects, total, err := repo.List(ctx, VideoProjectListFilter{
		Keywords: KeywordGroups{{"发布会"}},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(projects) != 1 {
		t.Fatalf("total=%d len=%d, want 1 hit by live_name", total, len(projects))
	}
	if projects[0].Name != "项目A" || projects[0].LiveName != "春季发布会源视频" {
		t.Fatalf("got = %+v", projects[0])
	}
}

// TestVideoProjectRepository_List_TaskCount 验证列表返回关联 task 数量。
func TestVideoProjectRepository_List_TaskCount(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()
	material := seedLiveMaterialForProject(t, db)

	p1 := &model.VideoProject{
		Name: "有任务", LiveID: material.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
	}
	p2 := &model.VideoProject{
		Name: "无任务", LiveID: material.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
	}
	if err := repo.Create(ctx, p1); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	if err := repo.Create(ctx, p2); err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	for i := 0; i < 2; i++ {
		pid := p1.ID
		task := &model.Task{
			ID:             fmt.Sprintf("task-%d", i+1),
			Type:           model.TaskTypeDraft,
			Status:         model.TaskStatusPending,
			VideoProjectID: &pid,
			CreatedBy:      1,
		}
		if err := db.Create(task).Error; err != nil {
			t.Fatalf("Create task: %v", err)
		}
	}

	items, total, err := repo.List(ctx, VideoProjectListFilter{}, 0, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	byName := map[string]model.VideoProjectListItem{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if byName["有任务"].TaskCount != 2 {
		t.Errorf("有任务 TaskCount = %d, want 2", byName["有任务"].TaskCount)
	}
	if byName["无任务"].TaskCount != 0 {
		t.Errorf("无任务 TaskCount = %d, want 0", byName["无任务"].TaskCount)
	}
}

// TestVideoProjectRepository_List_FilterByLiveID 验证按 live_id 精确筛选关联项目。
func TestVideoProjectRepository_List_FilterByLiveID(t *testing.T) {
	db := setupVideoProjectTestDB(t)
	repo := NewVideoProjectRepository(db)
	ctx := context.Background()

	m1 := seedLiveMaterialForProject(t, db)
	m2 := &model.LiveMaterial{
		Name: "其它素材", LiveURL: "https://example.com/other.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	if err := db.Create(m2).Error; err != nil {
		t.Fatalf("create material: %v", err)
	}

	p1 := &model.VideoProject{
		Name: "素材1项目", LiveID: m1.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
	}
	p2 := &model.VideoProject{
		Name: "素材2项目", LiveID: m2.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
		EnableCaptions: model.EnableCaptionsOn, CreatedBy: 1,
	}
	for _, p := range []*model.VideoProject{p1, p2} {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("Create project: %v", err)
		}
	}

	liveID := m1.ID
	projects, total, err := repo.List(ctx, VideoProjectListFilter{LiveID: &liveID}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(projects) != 1 {
		t.Fatalf("total=%d len=%d, want 1", total, len(projects))
	}
	if projects[0].Name != "素材1项目" || projects[0].LiveID != m1.ID {
		t.Fatalf("got = %+v", projects[0])
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
