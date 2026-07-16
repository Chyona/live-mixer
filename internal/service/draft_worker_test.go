package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type mockCapCutAPI struct {
	createResp *capcutmate.CreateDraftResponse
	createErr  error
	addResp    *capcutmate.AddVideosResponse
	addErr     error
	createCalls int
	addCalls    int
	lastAdd     capcutmate.AddVideosRequest
}

func (m *mockCapCutAPI) CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error) {
	m.createCalls++
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

func TestResolveDraftClipRanges_PreferClips1(t *testing.T) {
	project := &model.VideoProject{
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 100}},
		Clips1: []model.ClipWithText{{Text: "hi", StartTime: 10, EndTime: 50, Words: []model.ClipWord{}}},
	}
	clips, err := resolveDraftClipRanges(project)
	if err != nil {
		t.Fatalf("resolveDraftClipRanges() error = %v", err)
	}
	if len(clips) != 1 || clips[0].StartTime != 10 || clips[0].EndTime != 50 {
		t.Errorf("clips = %#v", clips)
	}
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

	ext, _ := marshalTaskExt(TaskExt{
		LiveID: material.ID, VideoProjectID: project.ID,
		CanvasWidth: 1080, CanvasHeight: 1920,
	})
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	capcut := &mockCapCutAPI{}
	cutter := &mockVideoCutter{}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		CapCut: capcut, Cutter: cutter, Downloader: &mockDraftDownloader{},
		Web: webroot.Config{RootDir: webRoot, RootURL: "http://192.168.3.219:81"},
		Logger: zap.NewNop(),
	})

	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if capcut.createCalls != 1 || capcut.addCalls != 1 {
		t.Errorf("capcut calls create=%d add=%d", capcut.createCalls, capcut.addCalls)
	}
	if len(cutter.calls) != 2 {
		t.Errorf("cut calls = %d, want 2", len(cutter.calls))
	}
	if !strings.Contains(capcut.lastAdd.VideoInfos, "clip_000.mp4") {
		t.Errorf("video_infos = %s", capcut.lastAdd.VideoInfos)
	}

	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("task = %s/%d", got.Status, got.Progress)
	}
	// 草稿地址应写入 task.draft_url，而非 video_project。
	if got.DraftURL != "http://example.com/draft" {
		t.Errorf("task.draft_url = %s", got.DraftURL)
	}

	// 切片应落在 staging/{task_id}
	staging := filepath.Join(webRoot, "staging", strconv.FormatUint(uint64(claimed.ID), 10))
	if _, err := os.Stat(filepath.Join(staging, "source.mp4")); err != nil {
		t.Errorf("source.mp4 missing: %v", err)
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
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeDraft, Status: model.TaskStatusPending, CreatedBy: 1,
		VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)

	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		CapCut: &mockCapCutAPI{createErr: errors.New("capcut down")},
		Cutter: &mockVideoCutter{}, Downloader: &mockDraftDownloader{},
		Web: webroot.Config{RootDir: webRoot, RootURL: "http://example.com"},
		Logger: zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected error")
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
}
