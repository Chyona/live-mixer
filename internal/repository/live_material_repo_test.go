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
			LiveURL:     fmt.Sprintf("https://example.com/live-%d.mp4", i+1),
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
	if materials[0].ProjectCount != 0 {
		t.Errorf("ProjectCount = %d, want 0 when no video_project", materials[0].ProjectCount)
	}
}

// TestLiveMaterialRepository_List_ProjectCount 验证列表返回关联 video_project 数量。
func TestLiveMaterialRepository_List_ProjectCount(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	m1 := &model.LiveMaterial{Name: "有项目", LiveURL: "https://example.com/a.mp4", ASRStatus: model.ASRStatusPending, CreatedBy: 1}
	m2 := &model.LiveMaterial{Name: "无项目", LiveURL: "https://example.com/b.mp4", ASRStatus: model.ASRStatusPending, CreatedBy: 1}
	if err := repo.Create(ctx, m1); err != nil {
		t.Fatalf("Create m1: %v", err)
	}
	if err := repo.Create(ctx, m2); err != nil {
		t.Fatalf("Create m2: %v", err)
	}
	for i := 0; i < 3; i++ {
		p := &model.VideoProject{
			Name:      fmt.Sprintf("项目-%d", i+1),
			LiveID:    m1.ID,
			PromptID:  model.DefaultVideoProjectPromptID,
			CreatedBy: 1,
		}
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("Create project: %v", err)
		}
	}

	materials, total, err := repo.List(ctx, LiveMaterialListFilter{}, 0, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	byName := map[string]model.LiveMaterialListItem{}
	for _, m := range materials {
		byName[m.Name] = m
	}
	if byName["有项目"].ProjectCount != 3 {
		t.Errorf("有项目 ProjectCount = %d, want 3", byName["有项目"].ProjectCount)
	}
	if byName["无项目"].ProjectCount != 0 {
		t.Errorf("无项目 ProjectCount = %d, want 0", byName["无项目"].ProjectCount)
	}
}

// TestLiveMaterialRepository_ClaimPendingASR 验证乐观锁抢占 pending → processing。
func TestLiveMaterialRepository_ClaimPendingASR(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	first := &model.LiveMaterial{
		Name: "先创建", LiveURL: "https://example.com/a.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
		CreatedAt: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	}
	second := &model.LiveMaterial{
		Name: "后创建", LiveURL: "https://example.com/b.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
		CreatedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
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
	if claimed.ASRVersion != 1 {
		t.Errorf("claimed.ASRVersion = %d, want 1", claimed.ASRVersion)
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

// TestLiveMaterialRepository_ClaimPendingASR_OptimisticLockConcurrent 验证多 goroutine 乐观锁互斥抢占 ASR。
func TestLiveMaterialRepository_ClaimPendingASR_OptimisticLockConcurrent(t *testing.T) {
	// SQLite :memory: 每连接独立库；共享 cache 才能在并发下看到同一张表。
	db, err := gorm.Open(sqlite.Open("file:asr_claim_concurrent?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}, &model.VideoProject{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	const n = 20
	for i := 0; i < n; i++ {
		m := &model.LiveMaterial{
			Name: fmt.Sprintf("m-%d", i), LiveURL: fmt.Sprintf("https://example.com/a-%d.mp4", i),
			LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
		}
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
	}

	type result struct {
		id  uint
		err error
	}
	ch := make(chan result, n*2)
	for i := 0; i < n*2; i++ {
		go func() {
			claimed, err := repo.ClaimPendingASR(ctx)
			if err != nil {
				ch <- result{err: err}
				return
			}
			if claimed == nil {
				ch <- result{}
				return
			}
			ch <- result{id: claimed.ID}
		}()
	}

	seen := make(map[uint]struct{})
	var claimedCount int
	for i := 0; i < n*2; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("claim error: %v", r.err)
		}
		if r.id == 0 {
			continue
		}
		if _, ok := seen[r.id]; ok {
			t.Fatalf("duplicate claim for material id=%d", r.id)
		}
		seen[r.id] = struct{}{}
		claimedCount++
	}
	if claimedCount != n {
		t.Fatalf("claimedCount = %d, want %d", claimedCount, n)
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
	if err := repo.UpdateASRProcessing(ctx, material.ID); err != nil {
		t.Fatalf("UpdateASRProcessing() error = %v", err)
	}
	before, _ := repo.GetByID(ctx, material.ID)
	if before.ASRStartedAt == nil {
		t.Fatal("asr_started_at should be set before requeue")
	}
	startedAt := *before.ASRStartedAt

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
	if got.ASRVersion != 1 {
		t.Errorf("ASRVersion = %d, want 1 after requeue", got.ASRVersion)
	}
	if got.ASRStartedAt == nil {
		t.Fatal("asr_started_at should be preserved after stale requeue")
	}
	if !got.ASRStartedAt.Equal(startedAt) {
		t.Errorf("asr_started_at = %v, want preserved %v", got.ASRStartedAt, startedAt)
	}

	// 再次抢占应刷新 asr_started_at。
	time.Sleep(time.Millisecond)
	claimed, err := repo.ClaimPendingASR(ctx)
	if err != nil {
		t.Fatalf("ClaimPendingASR() error = %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimPendingASR() = nil, want claimed material")
	}
	if claimed.ASRStartedAt == nil {
		t.Fatal("claimed asr_started_at should be set")
	}
	if claimed.ASRStartedAt.Before(startedAt) {
		t.Errorf("claimed asr_started_at = %v, want >= previous %v", claimed.ASRStartedAt, startedAt)
	}
}

// TestLiveMaterialRepository_ASRWrites_VersionCAS 验证过期 asr_version 的进度/完成/失败写回被忽略。
func TestLiveMaterialRepository_ASRWrites_VersionCAS(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "版本CAS", LiveURL: "https://example.com/cas.mp4",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	if err := repo.Create(ctx, material); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	claimed, err := repo.ClaimPendingASR(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimPendingASR() = %v, %v", claimed, err)
	}
	oldVersion := claimed.ASRVersion
	startedAt := *claimed.ASRStartedAt

	// 模拟超时回收：status→pending、version+1，保留 started_at。
	stale := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&model.LiveMaterial{}).Where("id = ?", material.ID).
		Update("asr_updated_at", stale).Error; err != nil {
		t.Fatalf("backdate asr_updated_at error = %v", err)
	}
	if _, err := repo.RequeueStaleProcessingASR(ctx, time.Hour); err != nil {
		t.Fatalf("RequeueStaleProcessingASR() error = %v", err)
	}

	// 旧 Worker 用过期 version 写回，应全部被忽略。
	if err := repo.UpdateASRProgress(ctx, material.ID, oldVersion, 80); err != nil {
		t.Fatalf("UpdateASRProgress(stale) error = %v", err)
	}
	if err := repo.UpdateASRCompleted(ctx, material.ID, oldVersion, `{"ok":true}`, 1000, 1280, 720, nil, nil); err != nil {
		t.Fatalf("UpdateASRCompleted(stale) error = %v", err)
	}
	if err := repo.UpdateASRFailed(ctx, material.ID, oldVersion, 40, "旧失败"); err != nil {
		t.Fatalf("UpdateASRFailed(stale) error = %v", err)
	}

	got, _ := repo.GetByID(ctx, material.ID)
	if got.ASRStatus != model.ASRStatusPending {
		t.Errorf("ASRStatus = %q, want pending (stale writes ignored)", got.ASRStatus)
	}
	if got.ASRProgress != 0 {
		t.Errorf("ASRProgress = %d, want 0", got.ASRProgress)
	}
	if got.ASRErrorMsg != "ASR 处理超时，已自动重新排队" {
		t.Errorf("ASRErrorMsg = %q", got.ASRErrorMsg)
	}
	if got.ASRStartedAt == nil || !got.ASRStartedAt.Equal(startedAt) {
		t.Errorf("asr_started_at should remain %v, got %v", startedAt, got.ASRStartedAt)
	}
	if got.LiveASR == `{"ok":true}` {
		t.Error("stale UpdateASRCompleted should not overwrite live_asr")
	}

	// 新一轮抢占后，正确 version 可写完成。
	claimed2, err := repo.ClaimPendingASR(ctx)
	if err != nil || claimed2 == nil {
		t.Fatalf("second ClaimPendingASR() = %v, %v", claimed2, err)
	}
	if err := repo.UpdateASRCompleted(ctx, material.ID, claimed2.ASRVersion, `{"result":"ok"}`, 2000, 1920, 1080, nil, nil); err != nil {
		t.Fatalf("UpdateASRCompleted(current) error = %v", err)
	}
	final, _ := repo.GetByID(ctx, material.ID)
	if final.ASRStatus != model.ASRStatusCompleted {
		t.Errorf("final ASRStatus = %q, want completed", final.ASRStatus)
	}
	if final.ASRStartedAt == nil {
		t.Error("final asr_started_at should not be empty")
	}
}

// TestLiveMaterialRepository_ResetASRToPending_OnlyFailed 验证仅 failed 可重置。
func TestLiveMaterialRepository_ResetASRToPending_OnlyFailed(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	failed := &model.LiveMaterial{
		Name: "失败", LiveURL: "https://example.com/f.mp4",
		LiveASR: `{"leftover":true}`, ASRStatus: model.ASRStatusFailed, ASRErrorMsg: "x", CreatedBy: 1,
		ASRSummaries: []model.ASRSummarySegment{{Title: "旧题", StartTime: 0, EndTime: 100}},
		ASRParagraphs: []model.ASRParagraph{{Speaker: "1", Text: "旧段", StartTime: 0, EndTime: 100}},
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
	if got.ASRVersion != 1 {
		t.Errorf("ASRVersion = %d, want 1 after reset", got.ASRVersion)
	}
	if got.LiveASR != "{}" {
		t.Errorf("LiveASR = %q, want {}", got.LiveASR)
	}
	if len(got.ASRSummaries) != 0 {
		t.Errorf("ASRSummaries = %+v, want empty", got.ASRSummaries)
	}
	if len(got.ASRParagraphs) != 0 {
		t.Errorf("ASRParagraphs = %+v, want empty", got.ASRParagraphs)
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

	if err := repo.UpdateASRProgress(ctx, material.ID, got.ASRVersion, 50); err != nil {
		t.Fatalf("UpdateASRProgress() error = %v", err)
	}
	got, _ = repo.GetByID(ctx, material.ID)
	if got.ASRProgress != 50 {
		t.Errorf("ASRProgress = %d, want 50", got.ASRProgress)
	}

	asrJSON := `{"audio_info":{"duration":3000},"result":{"text":"ok"}}`
	if err := repo.UpdateASRCompleted(ctx, material.ID, got.ASRVersion, asrJSON, 3000, 1920, 1080, nil, nil); err != nil {
		t.Fatalf("UpdateASRCompleted() error = %v", err)
	}
	got, _ = repo.GetByID(ctx, material.ID)
	if got.ASRStatus != model.ASRStatusCompleted || got.ASRProgress != 100 {
		t.Errorf("completed state = %s/%d", got.ASRStatus, got.ASRProgress)
	}
	if got.Duration != 3000 {
		t.Errorf("Duration = %d, want 3000", got.Duration)
	}
	if got.Width != 1920 || got.Height != 1080 {
		t.Errorf("Width/Height = %d/%d, want 1920x1080", got.Width, got.Height)
	}
	if got.LiveASR != asrJSON {
		t.Errorf("LiveASR = %q", got.LiveASR)
	}
	if got.ASRCompletedAt == nil {
		t.Error("asr_completed_at should be set on completed")
	}

	material2 := &model.LiveMaterial{
		Name: "失败素材", LiveURL: "https://example.com/a.mp3",
		LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1,
	}
	_ = repo.Create(ctx, material2)
	if err := repo.UpdateASRProcessing(ctx, material2.ID); err != nil {
		t.Fatalf("UpdateASRProcessing(material2) error = %v", err)
	}
	got2prep, _ := repo.GetByID(ctx, material2.ID)
	if err := repo.UpdateASRFailed(ctx, material2.ID, got2prep.ASRVersion, 20, "网络错误"); err != nil {
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

// TestLiveMaterialRepository_List_KeywordFilter 验证标题关键词按「与」匹配 name/remark。
func TestLiveMaterialRepository_List_KeywordFilter(t *testing.T) {
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
		Keywords: KeywordGroups{{"游戏", "周末"}},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(materials) != 1 || materials[0].Name != "游戏直播" {
		t.Errorf("unexpected result: total=%d materials=%+v", total, materials)
	}
}

// TestLiveMaterialRepository_List_KeywordORFilter 验证 "|" 组间为「或」。
func TestLiveMaterialRepository_List_KeywordORFilter(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	seed := []model.LiveMaterial{
		{Name: "游戏直播", Remark: "周末场", LiveURL: "https://a.com/1.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1},
		{Name: "美食分享", Remark: "厨房", LiveURL: "https://a.com/2.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1},
		{Name: "其它", Remark: "无", LiveURL: "https://a.com/3.mp4", LiveASR: "{}", ASRStatus: model.ASRStatusPending, CreatedBy: 1},
	}
	for i := range seed {
		if err := repo.Create(ctx, &seed[i]); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	materials, total, err := repo.List(ctx, LiveMaterialListFilter{
		// (游戏 AND 周末) OR 美食
		Keywords: KeywordGroups{{"游戏", "周末"}, {"美食"}},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || len(materials) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2", total, len(materials))
	}
	names := map[string]bool{}
	for _, m := range materials {
		names[m.Name] = true
	}
	if !names["游戏直播"] || !names["美食分享"] {
		t.Errorf("names = %v", names)
	}
}

// TestLiveMaterialRepository_List_ASRKeywordFilter 验证 ASR 关键词仅匹配 asr_paragraphs，并返回命中段落。
func TestLiveMaterialRepository_List_ASRKeywordFilter(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveMaterialRepository(db)
	ctx := context.Background()

	m1 := &model.LiveMaterial{
		Name: "素材A", LiveURL: "https://launch.com/2026.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusCompleted, CreatedBy: 1,
		ASRParagraphs: []model.ASRParagraph{
			{Speaker: "1", Text: "今天天气不错", StartTime: 0, EndTime: 1000},
			{Speaker: "1", Text: "欢迎来到发布会现场看看2026新品", StartTime: 1000, EndTime: 3000},
		},
	}
	m2 := &model.LiveMaterial{
		Name: "发布会回顾", Remark: "2026", LiveURL: "https://other.com/a.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
		ASRParagraphs: []model.ASRParagraph{
			{Speaker: "1", Text: "只有发布会没有年份", StartTime: 0, EndTime: 500},
		},
	}
	m3 := &model.LiveMaterial{
		Name: "其它", Remark: "无", LiveURL: "https://other.com/b.mp4", LiveASR: "{}",
		ASRStatus: model.ASRStatusPending, CreatedBy: 1,
		ASRParagraphs: []model.ASRParagraph{
			{Speaker: "1", Text: "无关内容", StartTime: 0, EndTime: 100},
		},
	}
	for _, m := range []*model.LiveMaterial{m1, m2, m3} {
		if err := repo.Create(ctx, m); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	materials, total, err := repo.List(ctx, LiveMaterialListFilter{
		ASRKeywords: KeywordGroups{{"发布会", "2026"}},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(materials) != 1 || materials[0].Name != "素材A" {
		t.Fatalf("unexpected result: total=%d materials=%+v", total, materials)
	}
	if len(materials[0].MatchedParagraphs) != 1 {
		t.Fatalf("MatchedParagraphs = %+v, want 1 hit", materials[0].MatchedParagraphs)
	}
	if materials[0].MatchedParagraphs[0].Text != "欢迎来到发布会现场看看2026新品" {
		t.Errorf("matched text = %q", materials[0].MatchedParagraphs[0].Text)
	}
	if materials[0].MatchedParagraphs[0].Words != nil {
		t.Errorf("matched paragraph should omit words")
	}
}

func TestFilterMatchedASRParagraphs(t *testing.T) {
	paras := []model.ASRParagraph{
		{Text: "Hello World", StartTime: 0, EndTime: 1},
		{Text: "发布会开场", StartTime: 1, EndTime: 2},
	}
	got := filterMatchedASRParagraphs(paras, KeywordGroups{{"发布会"}})
	if len(got) != 1 || got[0].Text != "发布会开场" {
		t.Fatalf("got = %+v", got)
	}
	// 组间 OR：命中「Hello」或同时命中「发布会」+「开场」
	gotOR := filterMatchedASRParagraphs(paras, KeywordGroups{{"hello"}, {"发布会", "开场"}})
	if len(gotOR) != 2 {
		t.Fatalf("OR groups matched = %+v, want 2", gotOR)
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
