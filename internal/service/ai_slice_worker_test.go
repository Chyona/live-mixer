package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type mockLLMChat struct {
	content string
	err     error
	calls   int
}

func (m *mockLLMChat) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	m.calls++
	if m.err != nil {
		return "", m.err
	}
	return m.content, nil
}

func setupAISliceWorkerTestDB(t *testing.T) *gorm.DB {
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

func TestAISliceWorker_Process_Success(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()

	liveASR := `{
		"result":{
			"utterances":[{
				"additions":{"speaker":"1"},
				"start_time":0,"end_time":2000,"text":"今天上新很好看",
				"words":[
					{"start_time":0,"end_time":800,"text":"今天"},
					{"start_time":800,"end_time":2000,"text":"上新很好看"}
				]
			}]
		}
	}`
	material := &model.LiveMaterial{
		Name:        "直播1",
		LiveURL:     "https://example.com/a.mp4",
		LiveASR:     liveASR,
		ASRStatus:   model.ASRStatusCompleted,
		ASRProgress: 100,
		CreatedBy:   1,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}

	project := &model.VideoProject{
		Name: "项目A", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ext, _ := marshalTaskExt(TaskExt{
		LiveID: material.ID, VideoProjectID: project.ID, TargetDurationMs: 60000,
	})
	task := &model.Task{
		Type:      model.TaskTypeAISlice,
		Status:    model.TaskStatusPending,
		CreatedBy: 1,
		Ext:       ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	mock := &mockLLMChat{content: `{"clips":[{"start_time":0,"end_time":2000}]}`}
	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, mock, zap.NewNop())

	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if mock.calls != 1 {
		t.Errorf("llm calls = %d, want 1", mock.calls)
	}

	got, err := taskRepo.GetByID(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("task status/progress = %s/%d", got.Status, got.Progress)
	}

	var gotExt TaskExt
	if err := json.Unmarshal([]byte(got.Ext), &gotExt); err != nil {
		t.Fatalf("ext unmarshal: %v", err)
	}
	if gotExt.VideoProjectID != project.ID {
		t.Fatalf("video_project_id = %d, want %d", gotExt.VideoProjectID, project.ID)
	}
	updated, err := projectRepo.GetByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get project: %v", err)
	}
	if len(updated.Clips1) == 0 || !strings.Contains(updated.Clips1[0].Text, "今天") {
		t.Errorf("clips1 = %#v, want contain 今天", updated.Clips1)
	}
	if len(updated.Clips0) == 0 {
		t.Errorf("clips0 should be non-empty ranges, got %#v", updated.Clips0)
	}
}

func TestAISliceWorker_Process_LLMFail(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/a.mp4",
		LiveASR: `{"result":{"utterances":[{"additions":{},"start_time":0,"end_time":100,"text":"hi","words":[]}]}}`,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	_ = liveRepo.Create(ctx, material)
	project := &model.VideoProject{Name: "p", LiveID: material.ID, CreatedBy: 1, Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{}}
	_ = projectRepo.Create(ctx, project)
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1, Ext: ext}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)

	mock := &mockLLMChat{err: errors.New("timeout")}
	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, mock, zap.NewNop())
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected error")
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
}
