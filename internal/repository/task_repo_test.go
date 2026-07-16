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

// setupTaskTestDB 创建内存 SQLite 并迁移任务相关表。
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
	if task.ID == 0 {
		t.Fatal("Create() should set ID")
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
		if err := db.Create(task).Error; err != nil {
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
	list, total, err := repo.List(ctx, TaskListFilter{Keywords: []string{"发布会", "精剪"}}, 0, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != hit.ID {
		t.Fatalf("unexpected keywords result: total=%d list=%+v want id=%d", total, list, hit.ID)
	}
	if list[0].VideoProjectName != "发布会精剪" {
		t.Errorf("VideoProjectName = %q, want 发布会精剪", list[0].VideoProjectName)
	}
}

func TestTaskRepository_ClaimPendingByType(t *testing.T) {
	repo := NewTaskRepository(setupTaskTestDB(t))
	ctx := context.Background()

	_ = repo.Create(ctx, &model.Task{Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1})
	first := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1, Ext: `{"live_id":1}`}
	second := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1, Ext: `{"live_id":2}`}
	_ = repo.Create(ctx, first)
	_ = repo.Create(ctx, second)

	claimed, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil {
		t.Fatalf("ClaimPendingByType() error = %v", err)
	}
	if claimed == nil || claimed.ID != first.ID {
		t.Fatalf("claimed = %#v, want id=%d", claimed, first.ID)
	}
	if claimed.Status != model.TaskStatusProcessing || claimed.StartedAt == nil {
		t.Errorf("claimed status/started = %s/%v", claimed.Status, claimed.StartedAt)
	}

	// draft 类型不应被 ai_slice 抢占。
	draftClaim, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil {
		t.Fatalf("second claim error = %v", err)
	}
	if draftClaim == nil || draftClaim.ID != second.ID {
		t.Fatalf("second claimed = %#v, want id=%d", draftClaim, second.ID)
	}

	none, err := repo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil {
		t.Fatalf("empty claim error = %v", err)
	}
	if none != nil {
		t.Errorf("empty claim = %#v, want nil", none)
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
