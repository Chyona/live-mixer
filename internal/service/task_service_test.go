package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type mockTaskRepo struct {
	repository.TaskRepository
	createFn func(ctx context.Context, task *model.Task) error
	created  *model.Task
}

func (m *mockTaskRepo) Create(ctx context.Context, task *model.Task) error {
	if m.createFn != nil {
		return m.createFn(ctx, task)
	}
	m.created = task
	task.ID = 42
	return nil
}

type mockLiveRepoForTask struct {
	material *model.LiveMaterial
	err      error
}

func (m *mockLiveRepoForTask) Create(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *mockLiveRepoForTask) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.material == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return m.material, nil
}
func (m *mockLiveRepoForTask) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *mockLiveRepoForTask) ClaimPendingASR(ctx context.Context) (*model.LiveMaterial, error) {
	return nil, nil
}
func (m *mockLiveRepoForTask) RequeueStaleProcessingASR(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (m *mockLiveRepoForTask) UpdateASRProcessing(ctx context.Context, id uint) error  { return nil }
func (m *mockLiveRepoForTask) UpdateASRProgress(ctx context.Context, id uint, progress int16) error {
	return nil
}
func (m *mockLiveRepoForTask) UpdateASRCompleted(ctx context.Context, id uint, liveASR string, duration int64) error {
	return nil
}
func (m *mockLiveRepoForTask) UpdateASRFailed(ctx context.Context, id uint, progress int16, errorMsg string) error {
	return nil
}
func (m *mockLiveRepoForTask) ResetASRToPending(ctx context.Context, id uint) error { return nil }
func (m *mockLiveRepoForTask) List(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	return nil, 0, nil
}
func (m *mockLiveRepoForTask) Delete(ctx context.Context, id uint) error { return nil }

type mockPromptRepo struct {
	prompt *model.LLMSystemPrompt
}

func (m *mockPromptRepo) Create(ctx context.Context, prompt *model.LLMSystemPrompt) error {
	return nil
}
func (m *mockPromptRepo) GetByID(ctx context.Context, id uint) (*model.LLMSystemPrompt, error) {
	if m.prompt == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return m.prompt, nil
}
func (m *mockPromptRepo) Update(ctx context.Context, prompt *model.LLMSystemPrompt) error {
	return nil
}
func (m *mockPromptRepo) Delete(ctx context.Context, id uint) error { return nil }
func (m *mockPromptRepo) List(ctx context.Context, filter repository.LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error) {
	return nil, 0, nil
}

type mockAISliceWorkerEnqueue struct {
	enqueued int
}

func (m *mockAISliceWorkerEnqueue) Enqueue() { m.enqueued++ }
func (m *mockAISliceWorkerEnqueue) Process(ctx context.Context, task *model.Task) error {
	return nil
}
func (m *mockAISliceWorkerEnqueue) ProcessWithOptions(ctx context.Context, task *model.Task, opts PhaseOptions) error {
	return nil
}
func (m *mockAISliceWorkerEnqueue) Start(ctx context.Context) {}

type stubVideoProjectRepo struct{}

func (stubVideoProjectRepo) Create(ctx context.Context, project *model.VideoProject) error {
	return nil
}
func (stubVideoProjectRepo) GetByID(ctx context.Context, id uint) (*model.VideoProject, error) {
	return nil, gorm.ErrRecordNotFound
}
func (stubVideoProjectRepo) Update(ctx context.Context, project *model.VideoProject) error {
	return nil
}
func (stubVideoProjectRepo) Delete(ctx context.Context, id uint) error { return nil }
func (stubVideoProjectRepo) List(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error) {
	return nil, 0, nil
}

type mockVideoProjectRepoForDraft struct {
	project *model.VideoProject
	err     error
}

func (m *mockVideoProjectRepoForDraft) Create(ctx context.Context, project *model.VideoProject) error {
	return nil
}
func (m *mockVideoProjectRepoForDraft) GetByID(ctx context.Context, id uint) (*model.VideoProject, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.project == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return m.project, nil
}
func (m *mockVideoProjectRepoForDraft) Update(ctx context.Context, project *model.VideoProject) error {
	return nil
}
func (m *mockVideoProjectRepoForDraft) Delete(ctx context.Context, id uint) error { return nil }
func (m *mockVideoProjectRepoForDraft) List(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error) {
	return nil, 0, nil
}

type mockDraftWorkerEnqueue struct {
	enqueued int
}

func (m *mockDraftWorkerEnqueue) Enqueue() { m.enqueued++ }
func (m *mockDraftWorkerEnqueue) Process(ctx context.Context, task *model.Task) error {
	return nil
}
func (m *mockDraftWorkerEnqueue) ProcessWithOptions(ctx context.Context, task *model.Task, opts PhaseOptions) error {
	return nil
}
func (m *mockDraftWorkerEnqueue) Start(ctx context.Context) {}

type mockAISliceDraftWorkerEnqueue struct {
	enqueued int
}

func (m *mockAISliceDraftWorkerEnqueue) Enqueue() { m.enqueued++ }
func (m *mockAISliceDraftWorkerEnqueue) Process(ctx context.Context, task *model.Task) error {
	return nil
}
func (m *mockAISliceDraftWorkerEnqueue) Start(ctx context.Context) {}

func TestTaskService_CreateAISlice_EnqueuesWorker(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusCompleted,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 5, Name: "slice-project-A", LiveID: 1, PromptID: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 1000}},
		Clips1: []model.ClipWithText{},
	}}
	tasks := &mockTaskRepo{}
	worker := &mockAISliceWorkerEnqueue{}
	prompts := &mockPromptRepo{prompt: &model.LLMSystemPrompt{ID: 1, Content: "sys-prompt"}}
	svc := NewTaskService(tasks, live, projects, prompts, worker, nil, nil)

	got, err := svc.CreateAISlice(context.Background(), 7, CreateAISliceInput{
		VideoProjectID: 5,
	})
	if err != nil {
		t.Fatalf("CreateAISlice() error = %v", err)
	}
	if got.ID != 42 || got.Status != model.TaskStatusPending || got.Progress != 0 {
		t.Errorf("task = %#v", got)
	}
	if worker.enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", worker.enqueued)
	}
	if tasks.created == nil || model.UintValue(tasks.created.VideoProjectID) != 5 {
		t.Errorf("VideoProjectID = %v, want 5", tasks.created.VideoProjectID)
	}
	if tasks.created == nil || tasks.created.VideoProjectName != "slice-project-A" {
		t.Errorf("VideoProjectName = %q, want slice-project-A", tasks.created.VideoProjectName)
	}
	if tasks.created == nil || tasks.created.SysPrompt != "sys-prompt" {
		t.Errorf("SysPrompt = %q", tasks.created.SysPrompt)
	}
	if tasks.created == nil || !strings.Contains(tasks.created.Ext, `"video_project_id":5`) {
		t.Errorf("ext = %v", tasks.created)
	}
}

func TestTaskService_CreateAISlice_PromptNotFound(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusCompleted,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 1, LiveID: 1, PromptID: 99,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 100}},
	}}
	svc := NewTaskService(&mockTaskRepo{}, live, projects, &mockPromptRepo{}, nil, nil, nil)
	_, err := svc.CreateAISlice(context.Background(), 1, CreateAISliceInput{VideoProjectID: 1})
	if !errors.Is(err, ErrLLMSystemPromptNotFound) {
		t.Fatalf("error = %v, want ErrLLMSystemPromptNotFound", err)
	}
}

func TestTaskService_CreateAISlice_EmptyClips0(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusCompleted,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 1, LiveID: 1, PromptID: 1, Clips0: []model.ClipRange{},
	}}
	svc := NewTaskService(&mockTaskRepo{}, live, projects, &mockPromptRepo{prompt: &model.LLMSystemPrompt{ID: 1, Content: "sys"}}, nil, nil, nil)
	_, err := svc.CreateAISlice(context.Background(), 1, CreateAISliceInput{VideoProjectID: 1})
	if err == nil || !strings.Contains(err.Error(), "clips0") {
		t.Fatalf("error = %v, want clips0 error", err)
	}
}

func TestTaskService_CreateAISlice_ASRNotReady(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusProcessing,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 1, LiveID: 1, PromptID: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 100}},
	}}
	svc := NewTaskService(&mockTaskRepo{}, live, projects, &mockPromptRepo{prompt: &model.LLMSystemPrompt{ID: 1, Content: "sys"}}, nil, nil, nil)
	_, err := svc.CreateAISlice(context.Background(), 1, CreateAISliceInput{VideoProjectID: 1})
	if !errors.Is(err, ErrTaskASRNotReady) {
		t.Fatalf("error = %v, want ErrTaskASRNotReady", err)
	}
}

func TestTaskService_CreateAISlice_MissingProjectID(t *testing.T) {
	svc := NewTaskService(&mockTaskRepo{}, &mockLiveRepoForTask{}, stubVideoProjectRepo{}, &mockPromptRepo{}, nil, nil, nil)
	_, err := svc.CreateAISlice(context.Background(), 1, CreateAISliceInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskService_CreateDraft_EnqueuesWorker(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{ID: 9, LiveURL: "https://x/a.mp4"}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 3, Name: "draft-project-B", LiveID: 9,
		Clips1: []model.ClipWithText{{Text: "a", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}}},
		Clips0: []model.ClipRange{},
	}}
	tasks := &mockTaskRepo{}
	draftWorker := &mockDraftWorkerEnqueue{}
	svc := NewTaskService(tasks, live, projects, &mockPromptRepo{}, nil, draftWorker, nil)

	got, err := svc.CreateDraft(context.Background(), 7, CreateDraftInput{VideoProjectID: 3})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if got.Type != model.TaskTypeDraft || got.Status != model.TaskStatusPending {
		t.Errorf("task = %#v", got)
	}
	if draftWorker.enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", draftWorker.enqueued)
	}
	if tasks.created == nil || model.UintValue(tasks.created.VideoProjectID) != 3 {
		t.Errorf("VideoProjectID = %v, want 3", tasks.created.VideoProjectID)
	}
	if tasks.created == nil || tasks.created.VideoProjectName != "draft-project-B" {
		t.Errorf("VideoProjectName = %q, want draft-project-B", tasks.created.VideoProjectName)
	}
	if tasks.created == nil || !strings.Contains(tasks.created.Ext, `"video_project_id":3`) {
		t.Errorf("ext = %v", tasks.created)
	}
}

func TestTaskService_CreateDraft_MissingProjectID(t *testing.T) {
	svc := NewTaskService(&mockTaskRepo{}, &mockLiveRepoForTask{}, stubVideoProjectRepo{}, &mockPromptRepo{}, nil, nil, nil)
	_, err := svc.CreateDraft(context.Background(), 1, CreateDraftInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskService_CreateDraft_EmptyClips(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{ID: 1}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 1, LiveID: 1, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
	}}
	svc := NewTaskService(&mockTaskRepo{}, live, projects, &mockPromptRepo{}, nil, nil, nil)
	_, err := svc.CreateDraft(context.Background(), 1, CreateDraftInput{VideoProjectID: 1})
	if err == nil {
		t.Fatal("expected empty clips error")
	}
}

func TestTaskService_CreateAISliceDraft_EnqueuesWorker(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusCompleted,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 8, Name: "one-click-project", LiveID: 1, PromptID: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 2000}},
		Clips1: []model.ClipWithText{},
	}}
	tasks := &mockTaskRepo{}
	worker := &mockAISliceDraftWorkerEnqueue{}
	prompts := &mockPromptRepo{prompt: &model.LLMSystemPrompt{ID: 1, Content: "sys-prompt"}}
	svc := NewTaskService(tasks, live, projects, prompts, nil, nil, worker)

	got, err := svc.CreateAISliceDraft(context.Background(), 3, CreateAISliceDraftInput{
		VideoProjectID: 8,
		CanvasWidth:    720,
		CanvasHeight:   1280,
	})
	if err != nil {
		t.Fatalf("CreateAISliceDraft() error = %v", err)
	}
	if got.Type != model.TaskTypeAISliceDraft || got.Status != model.TaskStatusPending {
		t.Errorf("task = %#v", got)
	}
	if worker.enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", worker.enqueued)
	}
	if tasks.created == nil || model.UintValue(tasks.created.VideoProjectID) != 8 {
		t.Errorf("VideoProjectID = %v", tasks.created.VideoProjectID)
	}
	if tasks.created == nil || tasks.created.SysPrompt != "sys-prompt" {
		t.Errorf("SysPrompt = %q", tasks.created.SysPrompt)
	}
	if tasks.created == nil || !strings.Contains(tasks.created.Ext, `"canvas_width":720`) {
		t.Errorf("ext = %v", tasks.created.Ext)
	}
}

func TestTaskService_CreateAISliceDraft_PromptNotFound(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{ID: 1, ASRStatus: model.ASRStatusCompleted}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 1, LiveID: 1, PromptID: 9,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 100}},
	}}
	svc := NewTaskService(&mockTaskRepo{}, live, projects, &mockPromptRepo{}, nil, nil, nil)
	_, err := svc.CreateAISliceDraft(context.Background(), 1, CreateAISliceDraftInput{VideoProjectID: 1})
	if !errors.Is(err, ErrLLMSystemPromptNotFound) {
		t.Fatalf("error = %v, want ErrLLMSystemPromptNotFound", err)
	}
}

func TestTaskService_GetFromDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&model.Task{})
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()
	task := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing, Progress: 66, CreatedBy: 1}
	_ = repo.Create(ctx, task)

	svc := NewTaskService(repo, &mockLiveRepoForTask{}, stubVideoProjectRepo{}, &mockPromptRepo{}, nil, nil, nil)
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Progress != 66 || got.Status != model.TaskStatusProcessing {
		t.Errorf("got = %#v", got)
	}
}

func TestBuildTaskListFilter_DateRange(t *testing.T) {
	filter, err := buildTaskListFilter(TaskListOptions{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending,
		StartDate: "2026-01-01", EndDate: "2026-01-31",
		Keywords: []string{" launch ", "", "spring"},
	})
	if err != nil {
		t.Fatalf("buildTaskListFilter() error = %v", err)
	}
	if filter.Type != model.TaskTypeAISlice || filter.Status != model.TaskStatusPending {
		t.Errorf("type/status = %s/%s", filter.Type, filter.Status)
	}
	if filter.StartAt == nil || filter.EndAt == nil {
		t.Fatal("StartAt/EndAt should be set")
	}
	if filter.StartAt.Format("2006-01-02") != "2026-01-01" {
		t.Errorf("StartAt = %v", filter.StartAt)
	}
	// end_date includes the whole day: next day 00:00 exclusive
	if filter.EndAt.Format("2006-01-02") != "2026-02-01" {
		t.Errorf("EndAt = %v, want 2026-02-01", filter.EndAt)
	}
	if len(filter.Keywords) != 2 || filter.Keywords[0] != "launch" || filter.Keywords[1] != "spring" {
		t.Errorf("Keywords = %v", filter.Keywords)
	}
}

func TestTaskService_List_InvalidDate(t *testing.T) {
	svc := NewTaskService(&mockTaskRepo{}, &mockLiveRepoForTask{}, stubVideoProjectRepo{}, &mockPromptRepo{}, nil, nil, nil)
	_, _, err := svc.List(context.Background(), 1, 10, TaskListOptions{StartDate: "2026/01/01"})
	if err == nil || !strings.Contains(err.Error(), "start_date") {
		t.Fatalf("error = %v, want start_date format error", err)
	}
}
