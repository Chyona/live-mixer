package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

func TestMapPhaseProgress(t *testing.T) {
	opts := PhaseOptions{ProgressBase: 50, ProgressSpan: 50}
	if got := mapPhaseProgress(opts, 0); got != 50 {
		t.Errorf("0 => %d, want 50", got)
	}
	if got := mapPhaseProgress(opts, 100); got != 100 {
		t.Errorf("100 => %d, want 100", got)
	}
	if got := mapPhaseProgress(opts, 50); got != 75 {
		t.Errorf("50 => %d, want 75", got)
	}
}

func TestAISliceDraftWorker_Process_Success(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	webRoot := t.TempDir()

	liveASR := `{"result":{"utterances":[
		{"additions":{},"start_time":0,"end_time":1000,"text":"今天上新很好看",
			"words":[{"start_time":0,"end_time":1000,"text":"今天上新很好看"}]}
	]}}`
	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/live.mp4",
		LiveASR: liveASR, ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}
	project := &model.VideoProject{
		Name: "一键", LiveID: material.ID, CreatedBy: 1, PromptID: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 2000}},
		Clips1: []model.ClipWithText{},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ext, _ := marshalTaskExt(TaskExt{
		LiveID: material.ID, VideoProjectID: project.ID,
	})
	task := &model.Task{
		Type: model.TaskTypeAISliceDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "系统提示", VideoProjectID: model.NewUintPtr(project.ID),
		Width: 1080, Height: 1920, LiveURL: material.LiveURL, Ext: ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISliceDraft)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	aiSlice := NewAISliceWorker(taskRepo, liveRepo, projectRepo, &mockLLMChat{content: `[0]`}, zap.NewNop(), 0, 0, webroot.Config{})
	capcut := &mockCapCutAPI{}
	draftWorker := newTestDraftWorker(taskRepo, liveRepo, projectRepo, capcut, webRoot)
	worker := NewAISliceDraftWorker(taskRepo, projectRepo, aiSlice, draftWorker, zap.NewNop(), 0, 0)

	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	got, err := taskRepo.GetByID(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("status/progress = %s/%d", got.Status, got.Progress)
	}
	if got.UsrPrompt == "" {
		t.Error("usr_prompt should be persisted by AI slice phase")
	}
	if got.DraftURL == "" {
		t.Error("task.draft_url should be written")
	}

	updated, err := projectRepo.GetByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get project: %v", err)
	}
	if len(updated.Clips1) != 1 || updated.Clips1[0].Text != "今天上新很好看" {
		t.Errorf("clips1 = %#v", updated.Clips1)
	}
	if capcut.createCalls != 1 || capcut.addCalls != 1 {
		t.Errorf("capcut calls create=%d add=%d", capcut.createCalls, capcut.addCalls)
	}
}

func TestAISliceDraftWorker_Process_SliceFail(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/a.mp4",
		LiveASR:   `{"result":{"utterances":[{"additions":{},"start_time":0,"end_time":100,"text":"hi","words":[]}]}}`,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	_ = liveRepo.Create(ctx, material)
	project := &model.VideoProject{
		Name: "p", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 200}},
		Clips1: []model.ClipWithText{},
	}
	_ = projectRepo.Create(ctx, project)
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeAISliceDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "sys", VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISliceDraft)

	aiSlice := NewAISliceWorker(taskRepo, liveRepo, projectRepo, &mockLLMChat{err: errors.New("llm down")}, zap.NewNop(), 0, 0, webroot.Config{})
	draftWorker := newTestDraftWorker(taskRepo, liveRepo, projectRepo, &mockCapCutAPI{}, t.TempDir())
	worker := NewAISliceDraftWorker(taskRepo, projectRepo, aiSlice, draftWorker, zap.NewNop(), 0, 0)
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected error")
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
	if got.DraftURL != "" {
		t.Error("draft should not run after slice failure")
	}
}

func TestAISliceDraftWorker_Process_EmptyClips1(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/a.mp4",
		LiveASR:   `{"result":{"utterances":[{"additions":{},"start_time":0,"end_time":100,"text":"hi","words":[]}]}}`,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	_ = liveRepo.Create(ctx, material)
	project := &model.VideoProject{
		Name: "p", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 200}},
		Clips1: []model.ClipWithText{},
	}
	_ = projectRepo.Create(ctx, project)
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeAISliceDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "sys", VideoProjectID: model.NewUintPtr(project.ID),
		Width: 1080, Height: 1920, LiveURL: material.LiveURL, Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISliceDraft)

	aiSlice := NewAISliceWorker(taskRepo, liveRepo, projectRepo, &mockLLMChat{content: `[]`}, zap.NewNop(), 0, 0, webroot.Config{})
	capcut := &mockCapCutAPI{}
	draftWorker := newTestDraftWorker(taskRepo, liveRepo, projectRepo, capcut, t.TempDir())
	worker := NewAISliceDraftWorker(taskRepo, projectRepo, aiSlice, draftWorker, zap.NewNop(), 0, 0)
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected empty clips1 error")
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
	if capcut.createCalls != 0 {
		t.Errorf("draft should not run, createCalls=%d", capcut.createCalls)
	}
}

func TestAISliceDraftWorker_DefaultConcurrencyIsThree(t *testing.T) {
	w := NewAISliceDraftWorker(nil, nil, nil, nil, zap.NewNop(), 0, 0).(*aiSliceDraftWorker)
	if w.concurrency != 3 {
		t.Fatalf("concurrency = %d, want 3", w.concurrency)
	}
	if aiSliceDraftDefaultConcurrency != 3 {
		t.Fatalf("aiSliceDraftDefaultConcurrency = %d, want 3", aiSliceDraftDefaultConcurrency)
	}
}

func TestAISliceDraftWorker_UsesConfiguredConcurrency(t *testing.T) {
	w := NewAISliceDraftWorker(nil, nil, nil, nil, zap.NewNop(), 5, 0).(*aiSliceDraftWorker)
	if w.concurrency != 5 {
		t.Fatalf("concurrency = %d, want 5", w.concurrency)
	}
}

func TestAISliceDraftWorker_DefaultAndConfiguredStaleTimeout(t *testing.T) {
	defaultW := NewAISliceDraftWorker(nil, nil, nil, nil, zap.NewNop(), 0, 0).(*aiSliceDraftWorker)
	if defaultW.staleTimeout != aiSliceDraftStaleTimeout {
		t.Fatalf("default staleTimeout = %v, want %v", defaultW.staleTimeout, aiSliceDraftStaleTimeout)
	}
	custom := NewAISliceDraftWorker(nil, nil, nil, nil, zap.NewNop(), 0, 80*time.Minute).(*aiSliceDraftWorker)
	if custom.staleTimeout != 80*time.Minute {
		t.Fatalf("custom staleTimeout = %v, want 80m", custom.staleTimeout)
	}
}

func TestAISliceDraftWorker_RequeueStaleProcessing(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	ctx := context.Background()

	stale := &model.Task{
		Type: model.TaskTypeAISliceDraft, Status: model.TaskStatusProcessing,
		Progress: 55, CreatedBy: 1,
	}
	if err := taskRepo.Create(ctx, stale); err != nil {
		t.Fatalf("Create: %v", err)
	}
	staleAt := time.Now().Add(-3 * time.Hour)
	if err := db.Model(&model.Task{}).Where("id = ?", stale.ID).Update("updated_at", staleAt).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	worker := NewAISliceDraftWorker(taskRepo, nil, nil, nil, zap.NewNop(), 1, time.Hour).(*aiSliceDraftWorker)
	worker.requeueStale(ctx)

	got, err := taskRepo.GetByID(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusPending {
		t.Fatalf("Status = %q, want pending", got.Status)
	}
}
