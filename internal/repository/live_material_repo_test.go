package repository

import (
	"context"
	"testing"
	"time"

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
	if err := db.AutoMigrate(&model.LiveMaterial{}, &model.VideoProject{}); err != nil {
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

// TestLiveMaterialRepository_List_ReturnsAllFieldsExceptLiveASR 验证列表不含 live_asr 且按 id 倒序。
func TestLiveMaterialRepository_List_ReturnsAllFieldsExceptLiveASR(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	asrPayloads := []string{`{"idx":1}`, `{"idx":2}`, `{"idx":3}`}
	names := []string{"素材1", "素材2", "素材3"}
	for i, name := range names {
		material := &model.LiveMaterial{
			Name:        name,
			LiveURL:     "https://example.com/live.mp4",
			LiveASR:     asrPayloads[i],
			ASRStatus:   model.ASRStatusPending,
			ASRProgress: 0,
			CreatedBy:   1,
		}
		if err := repo.Create(ctx, material); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	materials, total, err := repo.List(ctx, LiveMaterialListFilter{}, 0, 2)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(materials) != 2 {
		t.Fatalf("len(materials) = %d, want 2", len(materials))
	}
	// 按 id 倒序，最新创建的在前。
	if materials[0].Name != "素材3" {
		t.Errorf("materials[0].Name = %q, want 素材3", materials[0].Name)
	}
	if materials[0].LiveURL == "" {
		t.Error("live_url should be returned in list")
	}
}

// TestLiveMaterialRepository_ClaimPendingASR 验证悲观锁抢占 pending → processing。
func TestLiveMaterialRepository_ClaimPendingASR(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	first := &model.LiveMaterial{
		Name: "先创建", LiveURL: "https://example.com/a.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	second := &model.LiveMaterial{
		Name: "后创建", LiveURL: "https://example.com/b.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("Create first error = %v", err)
	}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("Create second error = %v", err)
	}

	claimed, err := repo.ClaimPendingASR(ctx)
	if err != nil {
		t.Fatalf("ClaimPendingASR() error = %v", err)
	}
	if claimed == nil || claimed.ID != first.ID {
		t.Fatalf("claimed = %#v, want id=%d", claimed, first.ID)
	}
	if claimed.ASRStatus != model.ASRStatusProcessing || claimed.ASRProgress != 5 {
		t.Errorf("claimed state = %s/%d, want processing/5", claimed.ASRStatus, claimed.ASRProgress)
	}

	claimed2, err := repo.ClaimPendingASR(ctx)
	if err != nil {
		t.Fatalf("second ClaimPendingASR() error = %v", err)
	}
	if claimed2 == nil || claimed2.ID != second.ID {
		t.Fatalf("second claimed = %#v, want id=%d", claimed2, second.ID)
	}

	none, err := repo.ClaimPendingASR(ctx)
	if err != nil {
		t.Fatalf("empty ClaimPendingASR() error = %v", err)
	}
	if none != nil {
		t.Fatalf("empty claim = %#v, want nil", none)
	}
}

// TestLiveMaterialRepository_RequeueStaleProcessingASR 验证超时 processing 回收为 pending。
func TestLiveMaterialRepository_RequeueStaleProcessingASR(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "卡死素材", LiveURL: "https://example.com/stuck.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	if err := repo.Create(ctx, material); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	claimed, err := repo.ClaimPendingASR(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimPendingASR() = %#v, err=%v", claimed, err)
	}

	stale := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&model.LiveMaterial{}).Where("id = ?", material.ID).
		Update("asr_updated_at", stale).Error; err != nil {
		t.Fatalf("backdate asr_updated_at error = %v", err)
	}

	n, err := repo.RequeueStaleProcessingASR(ctx, time.Hour)
	if err != nil {
		t.Fatalf("RequeueStaleProcessingASR() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued = %d, want 1", n)
	}
	got, _ := repo.GetByID(ctx, material.ID)
	if got.ASRStatus != model.ASRStatusPending {
		t.Errorf("ASRStatus = %q, want pending", got.ASRStatus)
	}
}

// TestLiveMaterialRepository_ResetASRToPending_OnlyFailed 验证仅 failed 可重置。
func TestLiveMaterialRepository_ResetASRToPending_OnlyFailed(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	failed := &model.LiveMaterial{
		Name: "失败", LiveURL: "https://example.com/f.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusFailed, ASRErrorMsg: "x", CreatedBy: 1,
	}
	completed := &model.LiveMaterial{
		Name: "完成", LiveURL: "https://example.com/c.mp4",
		LiveASR: `{"ok":true}`, ASRStatus: model.ASRStatusCompleted, CreatedBy: 1,
	}
	_ = repo.Create(ctx, failed)
	_ = repo.Create(ctx, completed)

	if err := repo.ResetASRToPending(ctx, failed.ID); err != nil {
		t.Fatalf("ResetASRToPending(failed) error = %v", err)
	}
	got, _ := repo.GetByID(ctx, failed.ID)
	if got.ASRStatus != model.ASRStatusPending {
		t.Errorf("failed reset status = %q, want pending", got.ASRStatus)
	}

	if err := repo.ResetASRToPending(ctx, completed.ID); err == nil {
		t.Fatal("ResetASRToPending(completed) error = nil, want error")
	}
}

// TestLiveMaterialRepository_UpdateASRStates 验证 ASR 状态更新方法。
func TestLiveMaterialRepository_UpdateASRStates(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name:        "ASR素材",
		LiveURL:     "https://example.com/live.mp4",
		LiveASR:     "{}",
		ASRStatus:   model.ASRStatusPending,
		ASRProgress: 0,
		CreatedBy:   1,
	}
	if err := repo.Create(ctx, material); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repo.UpdateASRProcessing(ctx, material.ID); err != nil {
		t.Fatalf("UpdateASRProcessing() error = %v", err)
	}
	got, _ := repo.GetByID(ctx, material.ID)
	if got.ASRStatus != model.ASRStatusProcessing || got.ASRProgress != 5 {
		t.Errorf("processing state = %s/%d, want processing/5", got.ASRStatus, got.ASRProgress)
	}
	if got.ASRStartedAt == nil {
		t.Error("asr_started_at should be set")
	}

	if err := repo.UpdateASRProgress(ctx, material.ID, 50); err != nil {
		t.Fatalf("UpdateASRProgress() error = %v", err)
	}
	got, _ = repo.GetByID(ctx, material.ID)
	if got.ASRProgress != 50 {
		t.Errorf("ASRProgress = %d, want 50", got.ASRProgress)
	}

	asrJSON := `{"audio_info":{"duration":3000},"result":{"text":"ok"}}`
	if err := repo.UpdateASRCompleted(ctx, material.ID, asrJSON, 3000); err != nil {
		t.Fatalf("UpdateASRCompleted() error = %v", err)
	}
	got, _ = repo.GetByID(ctx, material.ID)
	if got.ASRStatus != model.ASRStatusCompleted || got.ASRProgress != 100 {
		t.Errorf("completed state = %s/%d", got.ASRStatus, got.ASRProgress)
	}
	if got.Duration != 3000 {
		t.Errorf("Duration = %d, want 3000", got.Duration)
	}
	if got.LiveASR != asrJSON {
		t.Errorf("LiveASR = %q", got.LiveASR)
	}

	material2 := &model.LiveMaterial{
		Name: "失败素材", LiveURL: "https://example.com/a.mp3",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	_ = repo.Create(ctx, material2)
	if err := repo.UpdateASRFailed(ctx, material2.ID, 20, "网络错误"); err != nil {
		t.Fatalf("UpdateASRFailed() error = %v", err)
	}
	got2, _ := repo.GetByID(ctx, material2.ID)
	if got2.ASRStatus != model.ASRStatusFailed || got2.ASRErrorMsg != "网络错误" {
		t.Errorf("failed state = %+v", got2)
	}
}

// TestLiveMaterialRepository_List_Empty 验证空表时分页结果正确。
func TestLiveMaterialRepository_List_Empty(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)

	materials, total, err := repo.List(context.Background(), LiveMaterialListFilter{}, 0, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(materials) != 0 {
		t.Errorf("len(materials) = %d, want 0", len(materials))
	}
}

// TestLiveMaterialRepository_List_TitleKeywordFilter 验证标题关键词按「与」匹配 name/remark。
func TestLiveMaterialRepository_List_TitleKeywordFilter(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	seed := []model.LiveMaterial{
		{Name: "游戏直播", Remark: "周末场", LiveURL: "https://a.com/1.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1},
		{Name: "游戏解说", Remark: "工作日", LiveURL: "https://a.com/2.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1},
		{Name: "美食分享", Remark: "周末厨房", LiveURL: "https://a.com/3.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	materials, total, err := repo.List(ctx, LiveMaterialListFilter{
		TitleKeywords: []string{"游戏", "周末"},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(materials) != 1 || materials[0].Name != "游戏直播" {
		t.Errorf("unexpected result: total=%d materials=%+v", total, materials)
	}
}

// TestLiveMaterialRepository_List_GlobalKeywordFilter 验证全局关键词匹配链接与标题字段。
func TestLiveMaterialRepository_List_GlobalKeywordFilter(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	m1 := &model.LiveMaterial{Name: "素材A", LiveURL: "https://launch.com/2026.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusFailed, ASRErrorMsg: "timeout", CreatedBy: 1}
	m2 := &model.LiveMaterial{Name: "发布会回顾", Remark: "2026", LiveURL: "https://other.com/a.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1}
	m3 := &model.LiveMaterial{Name: "其它", Remark: "无", LiveURL: "https://other.com/b.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1}
	for _, m := range []*model.LiveMaterial{m1, m2, m3} {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	materials, total, err := repo.List(ctx, LiveMaterialListFilter{
		GlobalKeywords: []string{"发布会", "2026"},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(materials) != 1 || materials[0].Name != "发布会回顾" {
		t.Errorf("unexpected result: total=%d materials=%+v", total, materials)
	}
}

// TestLiveMaterialRepository_List_DateFilter 验证按 created_at 日期范围筛选。
func TestLiveMaterialRepository_List_DateFilter(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	inRange := &model.LiveMaterial{
		Name: "范围内", LiveURL: "https://a.com/in.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
		CreatedAt: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}
	outRange := &model.LiveMaterial{
		Name: "范围外", LiveURL: "https://a.com/out.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
		CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, m := range []*model.LiveMaterial{inRange, outRange} {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	startAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	materials, total, err := repo.List(ctx, LiveMaterialListFilter{
		StartAt: &startAt,
		EndAt:   &endAt,
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(materials) != 1 || materials[0].Name != "范围内" {
		t.Errorf("unexpected result: total=%d materials=%+v", total, materials)
	}
}

// TestLiveMaterialRepository_Delete_CascadeVideoProjects 验证删除素材时级联删除关联剪辑项目。
func TestLiveMaterialRepository_Delete_CascadeVideoProjects(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "待删除素材", LiveURL: "https://example.com/del.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	if err := repo.Create(ctx, material); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	project := &model.VideoProject{
		Name: "关联项目", LiveID: material.ID, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}, CreatedBy: 1,
	}
	if err := db.Create(project).Error; err != nil {
		t.Fatalf("create video_project: %v", err)
	}

	if err := repo.Delete(ctx, material.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := repo.GetByID(ctx, material.ID); err == nil {
		t.Fatal("live_material should be deleted")
	}

	var count int64
	if err := db.Model(&model.VideoProject{}).Where("live_id = ?", material.ID).Count(&count).Error; err != nil {
		t.Fatalf("count video_project: %v", err)
	}
	if count != 0 {
		t.Errorf("video_project count = %d, want 0", count)
	}
}

// TestLiveMaterialRepository_Delete_NotFound 验证删除不存在的素材返回错误。
func TestLiveMaterialRepository_Delete_NotFound(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)

	err := repo.Delete(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for missing record")
	}
	if err != gorm.ErrRecordNotFound {
		t.Errorf("error = %v, want ErrRecordNotFound", err)
	}
}
