package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"live-mixer/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupTaskTestDB 创建内存 SQLite 并迁移任务表。
func setupTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db
}

func TestTaskRepository_CreateGetList(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	task := &model.Task{
		Type:      model.TaskTypeAISlice,
		Status:    model.TaskStatusPending,
		Progress:  0,
		CreatedBy: 1,
		Ext:       `{"live_id":1}`,
	}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.ID == "" {
		t.Fatal("Create() should set UUID ID")
	}

	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Type != model.TaskTypeAISlice || got.Progress != 0 {
		t.Errorf("got type/progress = %s/%d", got.Type, got.Progress)
	}

	list, total, err := repo.List(ctx, TaskListFilter{Type: model.TaskTypeAISlice}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Errorf("List total/len = %d/%d, want 1/1", total, len(list))
	}
}

func TestTaskRepository_List_DateFilter(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	inRange := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		CreatedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
	}
	outRange := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, task := range []*model.Task{inRange, outRange} {
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	startAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	list, total, err := repo.List(ctx, TaskListFilter{StartAt: &startAt, EndAt: &endAt}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != inRange.ID {
		t.Errorf("unexpected result: total=%d list=%+v", total, list)
	}
}

func TestTaskRepository_List_Keywords(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	hit := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectName: "发布会精剪",
	}
	miss := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectName: "游戏高光",
	}
	if err := repo.Create(ctx, hit); err != nil {
		t.Fatalf("create hit: %v", err)
	}
	if err := repo.Create(ctx, miss); err != nil {
		t.Fatalf("create miss: %v", err)
	}

	// 仅匹配 task.video_project_name；多词 AND。
	list, total, err := repo.List(ctx, TaskListFilter{Keywords: KeywordGroups{{"发布会", "精剪"}}}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != hit.ID {
		t.Fatalf("unexpected keywords result: total=%d list=%+v want id=%s", total, list, hit.ID)
	}
	if list[0].VideoProjectName != "发布会精剪" {
		t.Errorf("VideoProjectName = %q, want 发布会精剪", list[0].VideoProjectName)
	}
}

// TestTaskRepository_List_ByVideoProjectAndStatuses 验证按项目 ID + 多状态筛选。
func TestTaskRepository_List_ByVideoProjectAndStatuses(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	projectA := uint(10)
	projectB := uint(20)
	pending := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: &projectA,
	}
	processing := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing, CreatedBy: 1,
		VideoProjectID: &projectA,
	}
	completed := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusCompleted, CreatedBy: 1,
		VideoProjectID: &projectA,
	}
	otherProject := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: &projectB,
	}
	for _, task := range []*model.Task{pending, processing, completed, otherProject} {
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	list, total, err := repo.List(ctx, TaskListFilter{
		VideoProjectID: &projectA,
		Statuses:       []string{model.TaskStatusPending, model.TaskStatusProcessing},
	}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2", total, len(list))
	}
	for _, item := range list {
		if model.UintValue(item.VideoProjectID) != projectA {
			t.Errorf("video_project_id = %v, want %d", item.VideoProjectID, projectA)
		}
		if item.Status != model.TaskStatusPending && item.Status != model.TaskStatusProcessing {
			t.Errorf("unexpected status %q", item.Status)
		}
	}
}

// TestTaskRepository_List_ReturnsStoredLiveURLAndCanvasSize 验证列表直接返回 task 上冗余的 live_url / live_name / width / height。
func TestTaskRepository_List_ReturnsStoredLiveURLAndCanvasSize(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	withSnapshot := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectName: "项目",
		LiveURL:          "https://example.com/live.mp4",
		LiveName:         "春季发布会",
		Width:            1920,
		Height:           1080,
	}
	orphan := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectName: "无快照任务",
	}
	if err := repo.Create(ctx, withSnapshot); err != nil {
		t.Fatalf("create withSnapshot: %v", err)
	}
	if err := repo.Create(ctx, orphan); err != nil {
		t.Fatalf("create orphan: %v", err)
	}

	list, total, err := repo.List(ctx, TaskListFilter{}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("List total/len = %d/%d, want 2/2", total, len(list))
	}

	byID := map[string]model.TaskListItem{}
	for _, item := range list {
		byID[item.ID] = item
	}
	got := byID[withSnapshot.ID]
	if got.LiveURL != "https://example.com/live.mp4" {
		t.Errorf("LiveURL = %q, want https://example.com/live.mp4", got.LiveURL)
	}
	if got.LiveName != "春季发布会" {
		t.Errorf("LiveName = %q, want 春季发布会", got.LiveName)
	}
	if got.Width != 1920 || got.Height != 1080 {
		t.Errorf("Width/Height = %d/%d, want 1920/1080", got.Width, got.Height)
	}
	orphaned := byID[orphan.ID]
	if orphaned.LiveURL != "" || orphaned.Width != 0 || orphaned.Height != 0 {
		t.Errorf("orphan should have empty live_url and zero canvas, got %+v", orphaned)
	}
}

func TestTaskRepository_ClaimPendingByType(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	_ = repo.Create(ctx, &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1})
	first := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1, Ext: `{"live_id":1}`,
		CreatedAt: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	}
	second := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1, Ext: `{"live_id":2}`,
		CreatedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
	}
	_ = repo.Create(ctx, first)
	_ = repo.Create(ctx, second)

	claimed, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil {
		t.Fatalf("ClaimPendingByType() error = %v", err)
	}
	if claimed == nil || claimed.ID != first.ID {
		t.Fatalf("claimed = %#v, want id=%s", claimed, first.ID)
	}
	if claimed.Status != model.TaskStatusProcessing || claimed.StartedAt == nil {
		t.Errorf("claimed status/started = %s/%v", claimed.Status, claimed.StartedAt)
	}
	// 乐观锁：抢占成功后 version 应从 0 增至 1。
	if claimed.Version != 1 {
		t.Errorf("claimed.Version = %d, want 1", claimed.Version)
	}

	// draft 类型不应被 ai_slice 抢占。
	draftClaim, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil {
		t.Fatalf("second claim error = %v", err)
	}
	if draftClaim == nil || draftClaim.ID != second.ID {
		t.Fatalf("second claimed = %#v, want id=%s", draftClaim, second.ID)
	}

	none, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil {
		t.Fatalf("empty claim error = %v", err)
	}
	if none != nil {
		t.Errorf("empty claim = %#v, want nil", none)
	}
}

// TestTaskRepository_ClaimPendingByType_OptimisticLockConcurrent 验证多 goroutine 乐观锁互斥抢占。
func TestTaskRepository_ClaimPendingByType_OptimisticLockConcurrent(t *testing.T) {
	// SQLite :memory: 每连接独立库；共享 cache 才能在并发下看到同一张表。
	db, err := gorm.Open(sqlite.Open("file:task_claim_concurrent?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	repo := NewTaskRepository(db)
	ctx := context.Background()

	const n = 20
	for i := 0; i < n; i++ {
		task := &model.Task{
			Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
			CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
		}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create[%d]: %v", i, err)
		}
	}

	type result struct {
		id  string
		err error
	}
	ch := make(chan result, n*2)
	for i := 0; i < n*2; i++ {
		go func() {
			claimed, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
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

	seen := make(map[string]struct{})
	var claimedCount int
	for i := 0; i < n*2; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("claim error: %v", r.err)
		}
		if r.id == "" {
			continue
		}
		if _, ok := seen[r.id]; ok {
			t.Fatalf("duplicate claim for task id=%s", r.id)
		}
		seen[r.id] = struct{}{}
		claimedCount++
	}
	if claimedCount != n {
		t.Fatalf("claimedCount = %d, want %d", claimedCount, n)
	}
}

func TestTaskRepository_ProgressCompleteFail(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	task := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1}
	_ = repo.Create(ctx, task)
	claimed, _ := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)

	if err := repo.UpdateProgress(ctx, claimed.ID, 55); err != nil {
		t.Fatalf("UpdateProgress() error = %v", err)
	}
	got, _ := repo.GetByID(ctx, claimed.ID)
	if got.Progress != 55 {
		t.Errorf("Progress = %d, want 55", got.Progress)
	}

	ext := `{"live_id":1,"video_project_id":9}`
	if err := repo.MarkCompleted(ctx, claimed.ID, 100, ext); err != nil {
		t.Fatalf("MarkCompleted() error = %v", err)
	}
	got, _ = repo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 || got.Ext != ext || got.CompletedAt == nil {
		t.Errorf("completed task = %#v", got)
	}

	failed := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1}
	_ = repo.Create(ctx, failed)
	claimedFail, _ := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err := repo.MarkFailed(ctx, claimedFail.ID, 40, "LLM 超时"); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	got, _ = repo.GetByID(ctx, claimedFail.ID)
	if got.Status != model.TaskStatusFailed || got.Progress != 40 || got.ErrorMessage != "LLM 超时" {
		t.Errorf("failed task = %#v", got)
	}
}

func TestTaskRepository_UpdatePrompts(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	task := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing, CreatedBy: 1}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdatePrompts(ctx, task.ID, "系统提示", "## 视频ASR\n[0] (1.00 - 2.00) 你好"); err != nil {
		t.Fatalf("UpdatePrompts: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.SysPrompt != "系统提示" {
		t.Errorf("SysPrompt = %q", got.SysPrompt)
	}
	if !strings.Contains(got.UsrPrompt, "## 视频ASR") {
		t.Errorf("UsrPrompt = %q", got.UsrPrompt)
	}
}

func TestTaskRepository_CountProcessingByTypes(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	_ = repo.Create(ctx, &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing, CreatedBy: 1})
	_ = repo.Create(ctx, &model.Task{Type: model.TaskTypeAISliceDraft, Status: model.TaskStatusProcessing, CreatedBy: 1})
	_ = repo.Create(ctx, &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing, CreatedBy: 1})

	n, err := repo.CountProcessingByTypes(ctx, []string{model.TaskTypeDraft, model.TaskTypeAISliceDraft})
	if err != nil {
		t.Fatalf("CountProcessingByTypes() error = %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}

// TestTaskRepository_RequeueStaleProcessingByType 验证超时 processing 按类型回收为 pending。
func TestTaskRepository_RequeueStaleProcessingByType(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := NewTaskRepository(db)
	ctx := context.Background()

	staleAI := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing,
		Progress: 40, CreatedBy: 1,
	}
	freshAI := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing,
		Progress: 20, CreatedBy: 1,
	}
	staleDraft := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing,
		Progress: 50, CreatedBy: 1,
	}
	pendingAI := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending,
		CreatedBy: 1,
	}
	for _, task := range []*model.Task{staleAI, freshAI, staleDraft, pendingAI} {
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// 仅将 staleAI 的 updated_at 回拨到 2 小时前；freshAI 保持当前时间。
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&model.Task{}).Where("id = ?", staleAI.ID).
		Update("updated_at", staleAt).Error; err != nil {
		t.Fatalf("backdate staleAI: %v", err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", staleDraft.ID).
		Update("updated_at", staleAt).Error; err != nil {
		t.Fatalf("backdate staleDraft: %v", err)
	}

	n, err := repo.RequeueStaleProcessingByType(ctx, model.TaskTypeAISlice, time.Hour)
	if err != nil {
		t.Fatalf("RequeueStaleProcessingByType() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("requeued = %d, want 1", n)
	}

	got, _ := repo.GetByID(ctx, staleAI.ID)
	if got.Status != model.TaskStatusPending {
		t.Errorf("staleAI Status = %q, want pending", got.Status)
	}
	if got.Progress != 0 {
		t.Errorf("staleAI Progress = %d, want 0", got.Progress)
	}
	if got.StartedAt != nil {
		t.Errorf("staleAI StartedAt = %v, want nil", got.StartedAt)
	}
	if got.ErrorMessage != "任务处理超时，已自动重新排队" {
		t.Errorf("staleAI ErrorMessage = %q", got.ErrorMessage)
	}
	if got.Version != 1 {
		t.Errorf("staleAI Version = %d, want 1 (requeue increments version)", got.Version)
	}

	// 未超时的同类型 processing 不应被回收。
	gotFresh, _ := repo.GetByID(ctx, freshAI.ID)
	if gotFresh.Status != model.TaskStatusProcessing {
		t.Errorf("freshAI Status = %q, want processing", gotFresh.Status)
	}

	// 其它类型即使超时也不应被本类型回收影响。
	gotDraft, _ := repo.GetByID(ctx, staleDraft.ID)
	if gotDraft.Status != model.TaskStatusProcessing {
		t.Errorf("staleDraft Status = %q, want processing (different type)", gotDraft.Status)
	}

	// olderThan <= 0 或空类型应 noop。
	if n, err := repo.RequeueStaleProcessingByType(ctx, model.TaskTypeDraft, 0); err != nil || n != 0 {
		t.Errorf("olderThan=0: n=%d err=%v, want 0/nil", n, err)
	}
	if n, err := repo.RequeueStaleProcessingByType(ctx, "", time.Hour); err != nil || n != 0 {
		t.Errorf("empty type: n=%d err=%v, want 0/nil", n, err)
	}
}

// TestTaskRepository_UpdateDraftURL 验证草稿 URL 回写。
func TestTaskRepository_UpdateDraftURL(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	task := &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing, CreatedBy: 1}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateDraftURL(ctx, task.ID, "http://example.com/draft"); err != nil {
		t.Fatalf("UpdateDraftURL: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DraftURL != "http://example.com/draft" {
		t.Errorf("DraftURL = %q", got.DraftURL)
	}
	if got.VideoURL != "" {
		t.Errorf("VideoURL should remain empty, got %q", got.VideoURL)
	}
}

// TestTaskRepository_UpdateVideoURL 验证成片视频 URL 回写。
func TestTaskRepository_UpdateVideoURL(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	task := &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing, CreatedBy: 1}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateVideoURL(ctx, task.ID, "http://example.com/out.mp4"); err != nil {
		t.Fatalf("UpdateVideoURL: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.VideoURL != "http://example.com/out.mp4" {
		t.Errorf("VideoURL = %q", got.VideoURL)
	}
}

// TestTaskRepository_UpdateErrorMessage 验证错误信息可独立回写。
func TestTaskRepository_UpdateErrorMessage(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	task := &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusCompleted, CreatedBy: 1}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateErrorMessage(ctx, task.ID, "视频生成失败"); err != nil {
		t.Fatalf("UpdateErrorMessage: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ErrorMessage != "视频生成失败" {
		t.Errorf("ErrorMessage = %q", got.ErrorMessage)
	}
	if got.Status != model.TaskStatusCompleted {
		t.Errorf("Status = %q, want completed", got.Status)
	}
}

// TestTaskRepository_UpdateClipsTarURL 验证切片 tar 包 URL 回写。
func TestTaskRepository_UpdateClipsTarURL(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()
	task := &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing, CreatedBy: 1}
	if err := repo.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := "https://oss.example/temp/draft/" + task.ID + "/" + task.ID + ".tar"
	if err := repo.UpdateClipsTarURL(ctx, task.ID, want); err != nil {
		t.Fatalf("UpdateClipsTarURL: %v", err)
	}
	got, err := repo.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ClipsTarURL != want {
		t.Errorf("ClipsTarURL = %q, want %q", got.ClipsTarURL, want)
	}
}
