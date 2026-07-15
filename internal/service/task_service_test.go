package service

import (
	"context"
	"errors"
	"testing"

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
func (stubVideoProjectRepo) List(ctx context.Context, filter repository.VideoProjectListFilter, offset, limit int) ([]model.VideoProject, int64, error) {
	return nil, 0, nil
}

func TestTaskService_CreateAISlice_EnqueuesWorker(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusCompleted,
	}}
	tasks := &mockTaskRepo{}
	worker := &mockAISliceWorkerEnqueue{}
	svc := NewTaskService(tasks, live, stubVideoProjectRepo{}, &mockPromptRepo{}, worker)

	got, err := svc.CreateAISlice(context.Background(), 7, CreateAISliceInput{
		LiveID: 1, Name: "n", TargetDurationMs: 30000,
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
}

func TestTaskService_CreateAISlice_ASRNotReady(t *testing.T) {
	live := &mockLiveRepoForTask{material: &model.LiveMaterial{
		ID: 1, ASRStatus: model.ASRStatusProcessing,
	}}
	svc := NewTaskService(&mockTaskRepo{}, live, stubVideoProjectRepo{}, &mockPromptRepo{}, nil)
	_, err := svc.CreateAISlice(context.Background(), 1, CreateAISliceInput{LiveID: 1})
	if !errors.Is(err, ErrTaskASRNotReady) {
		t.Fatalf("error = %v, want ErrTaskASRNotReady", err)
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

	svc := NewTaskService(repo, &mockLiveRepoForTask{}, stubVideoProjectRepo{}, &mockPromptRepo{}, nil)
	got, err := svc.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Progress != 66 || got.Status != model.TaskStatusProcessing {
		t.Errorf("got = %#v", got)
	}
}
