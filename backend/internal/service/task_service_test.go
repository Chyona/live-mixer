package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"live-mixer/internal/draft"
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
	task.ID = "00000000-0000-0000-0000-000000000042"
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
func (m *mockLiveRepoForTask) GetByName(ctx context.Context, name string) (*model.LiveMaterial, error) {
	return nil, gorm.ErrRecordNotFound
}
func (m *mockLiveRepoForTask) GetByLiveURL(ctx context.Context, liveURL string) (*model.LiveMaterial, error) {
	return nil, gorm.ErrRecordNotFound
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
func (m *mockLiveRepoForTask) UpdateASRProgress(ctx context.Context, id uint, asrVersion int64, progress int16) error {
	return nil
}
func (m *mockLiveRepoForTask) UpdateASRCompleted(ctx context.Context, id uint, asrVersion int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error {
	return nil
}
func (m *mockLiveRepoForTask) UpdateASRFailed(ctx context.Context, id uint, asrVersion int64, progress int16, errorMsg string) error {
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
	if m.project == nil || m.project.ID != id {
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
		ID: 1, Name: "春季发布会", LiveURL: "https://live.example/a.mp4", ASRStatus: model.ASRStatusCompleted,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 5, Name: "slice-project-A", LiveID: 1, PromptID: 1,
		Width: 1080, Height: 1920,
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
	if got.ID != "00000000-0000-0000-0000-000000000042" || got.Status != model.TaskStatusPending || got.Progress != 0 {
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
	// 创建时应按 video_project / live_material 自动写入冗余快照字段。
	if tasks.created == nil || tasks.created.LiveURL != "https://live.example/a.mp4" {
		t.Errorf("LiveURL = %q", tasks.created.LiveURL)
	}
	if tasks.created == nil || tasks.created.LiveName != "春季发布会" {
		t.Errorf("LiveName = %q, want 春季发布会", tasks.created.LiveName)
	}
	if tasks.created == nil || tasks.created.Width != 1080 || tasks.created.Height != 1920 {
		t.Errorf("Width/Height = %d/%d, want 1080/1920", tasks.created.Width, tasks.created.Height)
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
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{ID: 9, Name: "源视频A", LiveURL: "https://x/a.mp4"}}
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
	if tasks.created == nil || tasks.created.LiveURL != "https://x/a.mp4" {
		t.Errorf("LiveURL = %q", tasks.created.LiveURL)
	}
	if tasks.created == nil || tasks.created.LiveName != "源视频A" {
		t.Errorf("LiveName = %q, want 源视频A", tasks.created.LiveName)
	}
	// 未传画布且项目未设置时，写入默认竖屏尺寸。
	if tasks.created == nil || tasks.created.Width != draft.DefaultCanvasWidth || tasks.created.Height != draft.DefaultCanvasHeight {
		t.Errorf("Width/Height = %d/%d", tasks.created.Width, tasks.created.Height)
	}
	if tasks.created == nil || !strings.Contains(tasks.created.Ext, `"video_project_id":3`) {
		t.Errorf("ext = %v", tasks.created)
	}
}

// TestTaskService_CreateDraft_UsesProjectCanvasSize 验证未传画布尺寸时回退到 video_project.width/height 并写入 task。
func TestTaskService_CreateDraft_UsesProjectCanvasSize(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{ID: 9, LiveURL: "https://x/a.mp4"}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 3, Name: "draft-project-B", LiveID: 9, Width: 720, Height: 1280,
		Clips1: []model.ClipWithText{{Text: "a", StartTime: 0, EndTime: 1000}},
	}}
	tasks := &mockTaskRepo{}
	svc := NewTaskService(tasks, live, projects, &mockPromptRepo{}, nil, nil, nil)

	_, err := svc.CreateDraft(context.Background(), 7, CreateDraftInput{VideoProjectID: 3})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if tasks.created == nil || tasks.created.Width != 720 || tasks.created.Height != 1280 {
		t.Errorf("Width/Height = %d/%d, want 720/1280 from project", tasks.created.Width, tasks.created.Height)
	}
	if tasks.created == nil || tasks.created.LiveURL != "https://x/a.mp4" {
		t.Errorf("LiveURL = %q", tasks.created.LiveURL)
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
		ID: 1, Name: "一键源视频", LiveURL: "https://live.example/one.mp4", ASRStatus: model.ASRStatusCompleted,
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
	// 请求画布覆盖应写入 task.width/height；live_url/live_name 来自素材快照。
	if tasks.created == nil || tasks.created.Width != 720 || tasks.created.Height != 1280 {
		t.Errorf("Width/Height = %d/%d, want 720/1280", tasks.created.Width, tasks.created.Height)
	}
	if tasks.created == nil || tasks.created.LiveURL != "https://live.example/one.mp4" {
		t.Errorf("LiveURL = %q", tasks.created.LiveURL)
	}
	if tasks.created == nil || tasks.created.LiveName != "一键源视频" {
		t.Errorf("LiveName = %q, want 一键源视频", tasks.created.LiveName)
	}
}

// TestTaskService_CreateAISliceDraft_UsesProjectCanvasSize 验证未传画布时写入项目宽高到 task。
func TestTaskService_CreateAISliceDraft_UsesProjectCanvasSize(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, LiveURL: "https://live.example/one.mp4", ASRStatus: model.ASRStatusCompleted,
	}}
	projects := &mockVideoProjectRepoForDraft{project: &model.VideoProject{
		ID: 8, Name: "one-click-project", LiveID: 1, PromptID: 1,
		Width: 1920, Height: 1080,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 2000}},
		Clips1: []model.ClipWithText{},
	}}
	tasks := &mockTaskRepo{}
	prompts := &mockPromptRepo{prompt: &model.LLMSystemPrompt{ID: 1, Content: "sys-prompt"}}
	svc := NewTaskService(tasks, live, projects, prompts, nil, nil, &mockAISliceDraftWorkerEnqueue{})

	_, err := svc.CreateAISliceDraft(context.Background(), 3, CreateAISliceDraftInput{VideoProjectID: 8})
	if err != nil {
		t.Fatalf("CreateAISliceDraft() error = %v", err)
	}
	if tasks.created == nil || tasks.created.Width != 1920 || tasks.created.Height != 1080 {
		t.Errorf("Width/Height = %d/%d, want 1920/1080 from project", tasks.created.Width, tasks.created.Height)
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
		Keywords: " launch , spring | draft ",
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
	if len(filter.Keywords) != 2 || len(filter.Keywords[0]) != 2 || filter.Keywords[0][0] != "launch" || filter.Keywords[1][0] != "draft" {
		t.Errorf("Keywords = %#v", filter.Keywords)
	}
}

func TestTaskService_List_InvalidDate(t *testing.T) {
	svc := NewTaskService(&mockTaskRepo{}, &mockLiveRepoForTask{}, stubVideoProjectRepo{}, &mockPromptRepo{}, nil, nil, nil)
	_, _, err := svc.List(context.Background(), 1, 10, TaskListOptions{StartDate: "2026/01/01"})
	if err == nil || !strings.Contains(err.Error(), "start_date") {
		t.Fatalf("error = %v, want start_date format error", err)
	}
}

func TestTaskService_ListRunningByVideoProject(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&model.Task{})
	repo := repository.NewTaskRepository(db)
	ctx := context.Background()

	projectID := uint(7)
	otherID := uint(8)
	tasks := []*model.Task{
		{Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1, VideoProjectID: &projectID},
		{Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing, CreatedBy: 1, VideoProjectID: &projectID},
		{Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1, VideoProjectID: &projectID},
		{Type: model.TaskTypeDraft, Status: model.TaskStatusCompleted, CreatedBy: 1, VideoProjectID: &projectID},
		{Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1, VideoProjectID: &otherID},
	}
	for _, task := range tasks {
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	projectRepo := &mockVideoProjectRepoForDraft{project: &model.VideoProject{ID: projectID}}
	svc := NewTaskService(repo, &mockLiveRepoForTask{}, projectRepo, &mockPromptRepo{}, nil, nil, nil)

	list, total, err := svc.ListRunningByVideoProject(ctx, projectID, "", false)
	if err != nil {
		t.Fatalf("ListRunningByVideoProject() error = %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("default total/len = %d/%d, want 3/3", total, len(list))
	}

	list, total, err = svc.ListRunningByVideoProject(ctx, projectID, "", true)
	if err != nil {
		t.Fatalf("activeOnly error = %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Status != model.TaskStatusProcessing {
		t.Fatalf("activeOnly total/len/status = %d/%d/%v", total, len(list), list)
	}

	list, total, err = svc.ListRunningByVideoProject(ctx, projectID, model.TaskTypeDraft, false)
	if err != nil {
		t.Fatalf("type filter error = %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("type filter total/len = %d/%d, want 2/2", total, len(list))
	}

	_, _, err = svc.ListRunningByVideoProject(ctx, 99, "", false)
	if !errors.Is(err, ErrVideoProjectNotFound) {
		t.Fatalf("error = %v, want ErrVideoProjectNotFound", err)
	}

	_, _, err = svc.ListRunningByVideoProject(ctx, projectID, "bad_type", false)
	if !errors.Is(err, ErrTaskInvalidType) {
		t.Fatalf("error = %v, want ErrTaskInvalidType", err)
	}
}

// TestResolveDraftCanvasSize 验证画布尺寸优先级：请求 > 项目 > 默认值。
func TestResolveDraftCanvasSize(t *testing.T) {
	project := &model.VideoProject{Width: 720, Height: 1280}

	w, h := draft.ResolveCanvasSize(1080, 1920, project)
	if w != 1080 || h != 1920 {
		t.Fatalf("request override = %dx%d, want 1080x1920", w, h)
	}

	w, h = draft.ResolveCanvasSize(0, 0, project)
	if w != 720 || h != 1280 {
		t.Fatalf("project fallback = %dx%d, want 720x1280", w, h)
	}

	w, h = draft.ResolveCanvasSize(0, 0, &model.VideoProject{})
	if w != draft.DefaultCanvasWidth || h != draft.DefaultCanvasHeight {
		t.Fatalf("default = %dx%d, want %dx%d", w, h, draft.DefaultCanvasWidth, draft.DefaultCanvasHeight)
	}
}
