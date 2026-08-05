package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"live-mixer/internal/draft"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type mockCapCutAPI struct {
	createResp    *capcutmate.CreateDraftResponse
	createErr     error
	addResp       *capcutmate.AddVideosResponse
	addErr        error
	captionsResp  *capcutmate.AddCaptionsResponse
	captionsErr   error
	createCalls   int
	addCalls      int
	captionsCalls int
	lastWidth     int
	lastHeight    int
	lastAdd       capcutmate.AddVideosRequest
	lastCaptions  capcutmate.AddCaptionsRequest
}

func (m *mockCapCutAPI) CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error) {
	m.createCalls++
	m.lastWidth = width
	m.lastHeight = height
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.createResp != nil {
		return m.createResp, nil
	}
	return &capcutmate.CreateDraftResponse{Code: 0, DraftURL: "http://example.com/draft"}, nil
}

func (m *mockCapCutAPI) AddVideos(ctx context.Context, req capcutmate.AddVideosRequest, recordDir string) (*capcutmate.AddVideosResponse, error) {
	m.addCalls++
	m.lastAdd = req
	if m.addErr != nil {
		return nil, m.addErr
	}
	if m.addResp != nil {
		return m.addResp, nil
	}
	return &capcutmate.AddVideosResponse{Code: 0, DraftURL: req.DraftURL}, nil
}

func (m *mockCapCutAPI) AddCaptions(ctx context.Context, req capcutmate.AddCaptionsRequest, recordDir string) (*capcutmate.AddCaptionsResponse, error) {
	m.captionsCalls++
	m.lastCaptions = req
	if m.captionsErr != nil {
		return nil, m.captionsErr
	}
	if m.captionsResp != nil {
		return m.captionsResp, nil
	}
	return &capcutmate.AddCaptionsResponse{Code: 0, DraftURL: req.DraftURL}, nil
}

type mockVideoCutter struct {
	err   error
	calls []string
}

func (m *mockVideoCutter) CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	m.calls = append(m.calls, outputPath)
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(outputPath, []byte("fake-mp4"), 0o644)
}

type mockDraftDownloader struct {
	err error
}

func (m *mockDraftDownloader) Download(url, dest string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, []byte("source"), 0o644)
}

func setupDraftWorkerTestDB(t *testing.T) *gorm.DB {
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

func mustMarshalDraftExt(t *testing.T, liveID, projectID uint) string {
	t.Helper()
	raw, err := json.Marshal(TaskExt{
		LiveID: liveID, VideoProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// mockDraftObjectUploader 草稿 Worker 测试用对象存储上传 mock。
type mockDraftObjectUploader struct{}

func (mockDraftObjectUploader) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	return "https://oss.example/" + objectKey, nil
}

func newTestDraftGenerator(capcut draft.CapCutMateAPI) draft.Generator {
	return draft.NewGenerator(draft.GeneratorDeps{
		CapCut: capcut, Cutter: &mockVideoCutter{}, Downloader: &mockDraftDownloader{},
		Uploader: mockDraftObjectUploader{}, Logger: zap.NewNop(),
	})
}

func newTestDraftWorker(
	taskRepo repository.TaskRepository,
	liveRepo repository.LiveMaterialRepository,
	projectRepo repository.VideoProjectRepository,
	capcut draft.CapCutMateAPI,
	webRoot string,
) DraftWorker {
	return NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator: newTestDraftGenerator(capcut),
		Web:       webroot.Config{RootDir: webRoot},
		Logger:    zap.NewNop(),
	})
}

func TestDraftWorker_Process_Success(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	webRoot := t.TempDir()

	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/live.mp4", CreatedBy: 1,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}
	project := &model.VideoProject{
		Name: "项目", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 1000}},
		Clips1: []model.ClipWithText{
			{Text: "你好", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}},
			{Text: "世界", StartTime: 2000, EndTime: 3500, Words: []model.ClipWord{}},
		},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ext := mustMarshalDraftExt(t, material.ID, project.ID)
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: model.NewUintPtr(project.ID),
		// 创建时快照的画布尺寸与直播链接，Worker 直接读取。
		Width: 1080, Height: 1920, LiveURL: material.LiveURL,
		Ext: ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	capcut := &mockCapCutAPI{}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator: draft.NewGenerator(draft.GeneratorDeps{
			CapCut: capcut, Cutter: &mockVideoCutter{}, Downloader: &mockDraftDownloader{},
			Uploader: mockDraftObjectUploader{}, Logger: zap.NewNop(),
		}),
		Web:    webroot.Config{RootDir: webRoot},
		Logger: zap.NewNop(),
	})

	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if capcut.createCalls != 1 || capcut.addCalls != 1 {
		t.Errorf("capcut calls create=%d add=%d", capcut.createCalls, capcut.addCalls)
	}
	if !strings.Contains(capcut.lastAdd.VideoInfos, "https://oss.example/temp/draft/") {
		t.Errorf("video_infos = %s, want object storage URL", capcut.lastAdd.VideoInfos)
	}
	if !strings.Contains(capcut.lastAdd.VideoInfos, "clip_000.mp4") {
		t.Errorf("video_infos = %s", capcut.lastAdd.VideoInfos)
	}

	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("task = %s/%d", got.Status, got.Progress)
	}
	if got.DraftURL != "http://example.com/draft" {
		t.Errorf("task.draft_url = %s", got.DraftURL)
	}
	wantTar := "https://oss.example/temp/draft/" + claimed.ID + "/" + claimed.ID + ".tar"
	if got.ClipsTarURL != wantTar {
		t.Errorf("task.clips_tar_url = %q, want %q", got.ClipsTarURL, wantTar)
	}
	stagingTar := filepath.Join(webRoot, "staging", claimed.ID, claimed.ID+".tar")
	if _, err := os.Stat(stagingTar); err != nil {
		t.Errorf("local tar missing: %v", err)
	}
	if capcut.lastWidth != 1080 || capcut.lastHeight != 1920 {
		t.Errorf("CreateDraft size = %dx%d", capcut.lastWidth, capcut.lastHeight)
	}
	staging := filepath.Join(webRoot, "staging", claimed.ID)
	if _, err := os.Stat(filepath.Join(staging, "source.mp4")); err != nil {
		t.Errorf("source.mp4 missing: %v", err)
	}
}

func TestDraftWorker_Process_UsesProjectCanvasSize(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	webRoot := t.TempDir()

	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/live.mp4", CreatedBy: 1,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100,
	}
	_ = liveRepo.Create(ctx, material)
	project := &model.VideoProject{
		Name: "横屏", LiveID: material.ID, CreatedBy: 1,
		Width: 1920, Height: 1080,
		Clips1: []model.ClipWithText{{Text: "横", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}}},
		Clips0: []model.ClipRange{},
	}
	_ = projectRepo.Create(ctx, project)
	ext := mustMarshalDraftExt(t, material.ID, project.ID)
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		// Width/Height 为 0 时 Worker 回退到 video_project 画布尺寸。
		VideoProjectID: model.NewUintPtr(project.ID), LiveURL: material.LiveURL, Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)

	capcut := &mockCapCutAPI{}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator: newTestDraftGenerator(capcut),
		Web:       webroot.Config{RootDir: webRoot},
		Logger:    zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if capcut.lastWidth != 1920 || capcut.lastHeight != 1080 {
		t.Fatalf("CreateDraft size = %dx%d, want 1920x1080", capcut.lastWidth, capcut.lastHeight)
	}
}

func TestDraftWorker_Process_UsesTaskLiveURLFallback(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	webRoot := t.TempDir()

	// 素材 live_url 为空时，应回退使用创建任务时写入的 task.live_url。
	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "", CreatedBy: 1,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}
	project := &model.VideoProject{
		Name: "项目", LiveID: material.ID, CreatedBy: 1,
		Clips1: []model.ClipWithText{{Text: "hi", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}}},
		Clips0: []model.ClipRange{},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ext := mustMarshalDraftExt(t, material.ID, project.ID)
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: model.NewUintPtr(project.ID),
		Width: 1080, Height: 1920, LiveURL: "https://snapshot.example/live.mp4", Ext: ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	capcut := &mockCapCutAPI{}
	worker := newTestDraftWorker(taskRepo, liveRepo, projectRepo, capcut, webRoot)
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted {
		t.Errorf("status = %s, want completed", got.Status)
	}
}

func TestDraftWorker_Process_CapCutFail(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	webRoot := t.TempDir()

	material := &model.LiveMaterial{Name: "直播", LiveURL: "https://example.com/a.mp4", CreatedBy: 1, ASRStatus: model.ASRStatusCompleted}
	_ = liveRepo.Create(ctx, material)
	project := &model.VideoProject{
		LiveID: material.ID, CreatedBy: 1,
		Clips1: []model.ClipWithText{{Text: "a", StartTime: 0, EndTime: 500, Words: []model.ClipWord{}}},
		Clips0: []model.ClipRange{},
	}
	_ = projectRepo.Create(ctx, project)
	ext := mustMarshalDraftExt(t, material.ID, project.ID)
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: model.NewUintPtr(project.ID), LiveURL: material.LiveURL, Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)

	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator: newTestDraftGenerator(&mockCapCutAPI{createErr: errors.New("capcut down")}),
		Web:       webroot.Config{RootDir: webRoot},
		Logger:    zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected error")
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
}

func TestDraftWorker_DefaultConcurrencyIsThree(t *testing.T) {
	w := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop()}).(*draftWorker)
	if w.concurrency != 3 {
		t.Fatalf("concurrency = %d, want 3", w.concurrency)
	}
}

func TestDraftWorker_UsesConfiguredConcurrency(t *testing.T) {
	w := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop(), Concurrency: 5}).(*draftWorker)
	if w.concurrency != 5 {
		t.Fatalf("concurrency = %d, want 5", w.concurrency)
	}
}

func TestDraftWorker_DefaultAndConfiguredStaleTimeout(t *testing.T) {
	defaultW := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop()}).(*draftWorker)
	if defaultW.staleTimeout != draftStaleTimeout {
		t.Fatalf("default staleTimeout = %v", defaultW.staleTimeout)
	}
	custom := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop(), StaleTimeout: 45 * time.Minute}).(*draftWorker)
	if custom.staleTimeout != 45*time.Minute {
		t.Fatalf("custom staleTimeout = %v", custom.staleTimeout)
	}
}

func TestDraftWorker_RequeueStaleProcessing(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	ctx := context.Background()

	stale := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusProcessing,
		Progress: 40, CreatedBy: 1,
	}
	if err := taskRepo.Create(ctx, stale); err != nil {
		t.Fatalf("Create: %v", err)
	}
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&model.Task{}).Where("id = ?", stale.ID).Update("updated_at", staleAt).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	w := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, Logger: zap.NewNop(), Concurrency: 1, StaleTimeout: time.Hour,
	}).(*draftWorker)
	w.requeueStale(ctx)

	got, err := taskRepo.GetByID(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusPending {
		t.Fatalf("Status = %q, want pending", got.Status)
	}
}

type mockVideoExporter struct {
	videoURL string
	err      error
	calls    int
}

func (m *mockVideoExporter) GenerateVideoAndWait(ctx context.Context, draftURL, recordDir string, onProgress capcutmate.VideoProgressCallback) (string, error) {
	m.calls++
	if onProgress != nil {
		onProgress(50, capcutmate.GenVideoStatusProcessing)
		onProgress(100, capcutmate.GenVideoStatusCompleted)
	}
	if m.err != nil {
		return "", m.err
	}
	return m.videoURL, nil
}

func seedDraftTask(t *testing.T, ctx context.Context, taskRepo repository.TaskRepository, liveRepo repository.LiveMaterialRepository, projectRepo repository.VideoProjectRepository) *model.Task {
	t.Helper()
	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/live.mp4", CreatedBy: 1,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}
	project := &model.VideoProject{
		Name: "项目", LiveID: material.ID, CreatedBy: 1,
		Clips1: []model.ClipWithText{{Text: "你好", StartTime: 0, EndTime: 1000}},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: model.NewUintPtr(project.ID),
		Width: 1080, Height: 1920, LiveURL: material.LiveURL,
		Ext: mustMarshalDraftExt(t, material.ID, project.ID),
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	return claimed
}

func TestDraftWorker_Process_VideoExportSuccess(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	claimed := seedDraftTask(t, ctx, taskRepo, liveRepo, projectRepo)

	exporter := &mockVideoExporter{videoURL: "https://example.com/out.mp4"}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator:     newTestDraftGenerator(&mockCapCutAPI{}),
		VideoExporter: exporter,
		Web:           webroot.Config{RootDir: t.TempDir()},
		Logger:        zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if exporter.calls != 1 {
		t.Errorf("exporter.calls = %d", exporter.calls)
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("task = %s/%d", got.Status, got.Progress)
	}
	if got.DraftURL != "http://example.com/draft" {
		t.Errorf("draft_url = %s", got.DraftURL)
	}
	if got.VideoURL != "https://example.com/out.mp4" {
		t.Errorf("video_url = %s", got.VideoURL)
	}
	if got.ErrorMessage != "" {
		t.Errorf("error_message = %q, want empty", got.ErrorMessage)
	}
}

func TestDraftWorker_Process_VideoExportFailKeepsDraft(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	claimed := seedDraftTask(t, ctx, taskRepo, liveRepo, projectRepo)

	exporter := &mockVideoExporter{err: errors.New("gen_video down")}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator:     newTestDraftGenerator(&mockCapCutAPI{}),
		VideoExporter: exporter,
		Web:           webroot.Config{RootDir: t.TempDir()},
		Logger:        zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v, want nil (partial success)", err)
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted {
		t.Errorf("Status = %q, want completed", got.Status)
	}
	if got.DraftURL != "http://example.com/draft" {
		t.Errorf("draft_url = %s", got.DraftURL)
	}
	if got.VideoURL != "" {
		t.Errorf("video_url = %q, want empty", got.VideoURL)
	}
	if !strings.Contains(got.ErrorMessage, "视频生成失败") {
		t.Errorf("error_message = %q", got.ErrorMessage)
	}
}

func TestDraftWorker_Process_SkipGenVideoWhenDisabled(t *testing.T) {
	db := setupDraftWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	claimed := seedDraftTask(t, ctx, taskRepo, liveRepo, projectRepo)

	off := false
	exporter := &mockVideoExporter{videoURL: "https://example.com/out.mp4"}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		Generator:      newTestDraftGenerator(&mockCapCutAPI{}),
		VideoExporter:  exporter,
		EnableGenVideo: &off,
		Web:            webroot.Config{RootDir: t.TempDir()},
		Logger:         zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if exporter.calls != 0 {
		t.Errorf("exporter.calls = %d, want 0", exporter.calls)
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("task = %s/%d", got.Status, got.Progress)
	}
	if got.DraftURL != "http://example.com/draft" {
		t.Errorf("draft_url = %s", got.DraftURL)
	}
	if got.VideoURL != "" {
		t.Errorf("video_url = %q, want empty", got.VideoURL)
	}
	if got.ErrorMessage != "" {
		t.Errorf("error_message = %q, want empty", got.ErrorMessage)
	}
}
