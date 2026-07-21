package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type mockCapCutAPI struct {
	createResp  *capcutmate.CreateDraftResponse
	createErr   error
	addResp     *capcutmate.AddVideosResponse
	addErr      error
	createCalls int
	addCalls    int
	lastWidth   int
	lastHeight  int
	lastAdd     capcutmate.AddVideosRequest
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

func TestMergeAdjacentClipRanges(t *testing.T) {
	const gap = draftClipMergeGapMS

	tests := []struct {
		name string
		in   []model.ClipRange
		want []model.ClipRange
	}{
		{name: "empty", in: nil, want: nil},
		{
			name: "single",
			in:   []model.ClipRange{{StartTime: 0, EndTime: 1000}},
			want: []model.ClipRange{{StartTime: 0, EndTime: 1000}},
		},
		{
			name: "gap_exactly_500_merges",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1500, EndTime: 2000},
			},
			want: []model.ClipRange{{StartTime: 0, EndTime: 2000}},
		},
		{
			name: "gap_501_keeps_two",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1501, EndTime: 2000},
			},
			want: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1501, EndTime: 2000},
			},
		},
		{
			name: "overlap_merges",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 900, EndTime: 1500},
			},
			want: []model.ClipRange{{StartTime: 0, EndTime: 1500}},
		},
		{
			name: "list_order_preserved_no_sort",
			// 列表中不相邻：即使时间上可合并也不合；顺序保持 [晚, 早]。
			in: []model.ClipRange{
				{StartTime: 1200, EndTime: 2000},
				{StartTime: 0, EndTime: 1000},
			},
			want: []model.ClipRange{
				{StartTime: 1200, EndTime: 2000},
				{StartTime: 0, EndTime: 1000},
			},
		},
		{
			name: "chain_three_into_one",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1200, EndTime: 2000},
				{StartTime: 2300, EndTime: 3000},
			},
			want: []model.ClipRange{{StartTime: 0, EndTime: 3000}},
		},
		{
			name: "partial_chain",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1200, EndTime: 2000},
				{StartTime: 3000, EndTime: 4000},
			},
			want: []model.ClipRange{
				{StartTime: 0, EndTime: 2000},
				{StartTime: 3000, EndTime: 4000},
			},
		},
		{
			name: "list_adjacent_merges_even_if_time_jumps_back",
			// 列表相邻且 gap≤500：按列表合，不按时间轴重排。
			in: []model.ClipRange{
				{StartTime: 2000, EndTime: 3000},
				{StartTime: 3200, EndTime: 4000},
				{StartTime: 0, EndTime: 500},
			},
			want: []model.ClipRange{
				{StartTime: 2000, EndTime: 4000},
				{StartTime: 0, EndTime: 500},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeAdjacentClipRanges(tt.in, gap)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d; got=%#v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestDraftWorker_Process_MergesAdjacentClips 验证间隔 ≤500ms 的 clips1 合并后只裁剪一次。
func TestDraftWorker_Process_MergesAdjacentClips(t *testing.T) {
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
		Clips0: []model.ClipRange{},
		Clips1: []model.ClipWithText{
			{Text: "a", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}},
			{Text: "b", StartTime: 1300, EndTime: 2000, Words: []model.ClipWord{}},
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

	cutter := &mockVideoCutter{}
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		CapCut: &mockCapCutAPI{}, Cutter: cutter, Downloader: &mockDraftDownloader{},
		Web: webroot.Config{RootDir: webRoot, RootURL: "http://example.com"},
		Logger: zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(cutter.calls) != 1 {
		t.Fatalf("cut calls = %d, want 1 after merge", len(cutter.calls))
	}

	// 库中 clips1 仍为细粒度两段，未写回合并结果。
	updated, err := projectRepo.GetByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(updated.Clips1) != 2 {
		t.Errorf("clips1 len = %d, want 2 (unchanged)", len(updated.Clips1))
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
	if capcut.lastWidth != 1080 || capcut.lastHeight != 1920 {
		t.Errorf("CreateDraft size = %dx%d, want 1080x1920", capcut.lastWidth, capcut.lastHeight)
	}

	// 切片应落在 staging/{task_id}
	staging := filepath.Join(webRoot, "staging", claimed.ID)
	if _, err := os.Stat(filepath.Join(staging, "source.mp4")); err != nil {
		t.Errorf("source.mp4 missing: %v", err)
	}
}

// TestDraftWorker_Process_UsesProjectCanvasSize 验证 ext 未带画布时，create_draft 使用 video_project.width/height。
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
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}
	project := &model.VideoProject{
		Name: "横屏项目", LiveID: material.ID, CreatedBy: 1,
		Width: 1920, Height: 1080,
		Clips0: []model.ClipRange{},
		Clips1: []model.ClipWithText{
			{Text: "横", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}},
		},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// ext 故意不写 canvas_*，模拟旧任务或仅带项目引用的场景。
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
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
	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo: taskRepo, LiveMaterialRepo: liveRepo, VideoProjectRepo: projectRepo,
		CapCut: capcut, Cutter: &mockVideoCutter{}, Downloader: &mockDraftDownloader{},
		Web: webroot.Config{RootDir: webRoot, RootURL: "http://example.com"},
		Logger: zap.NewNop(),
	})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if capcut.lastWidth != 1920 || capcut.lastHeight != 1080 {
		t.Fatalf("CreateDraft size = %dx%d, want project 1920x1080", capcut.lastWidth, capcut.lastHeight)
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

func TestDraftWorker_DefaultConcurrencyIsThree(t *testing.T) {
	w := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop()}).(*draftWorker)
	if w.concurrency != 3 {
		t.Fatalf("concurrency = %d, want 3", w.concurrency)
	}
	if draftDefaultConcurrency != 3 {
		t.Fatalf("draftDefaultConcurrency = %d, want 3", draftDefaultConcurrency)
	}
}

func TestDraftWorker_UsesConfiguredConcurrency(t *testing.T) {
	w := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop(), Concurrency: 5}).(*draftWorker)
	if w.concurrency != 5 {
		t.Fatalf("concurrency = %d, want 5", w.concurrency)
	}
}

// TestDraftWorker_DefaultAndConfiguredStaleTimeout 验证草稿孤儿回收超时默认值与配置覆盖。
func TestDraftWorker_DefaultAndConfiguredStaleTimeout(t *testing.T) {
	defaultW := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop()}).(*draftWorker)
	if defaultW.staleTimeout != draftStaleTimeout {
		t.Fatalf("default staleTimeout = %v, want %v", defaultW.staleTimeout, draftStaleTimeout)
	}
	custom := NewDraftWorker(DraftWorkerDeps{Logger: zap.NewNop(), StaleTimeout: 45 * time.Minute}).(*draftWorker)
	if custom.staleTimeout != 45*time.Minute {
		t.Fatalf("custom staleTimeout = %v, want 45m", custom.staleTimeout)
	}
}

// TestDraftWorker_RequeueStaleProcessing 验证将超时 processing 改回 pending。
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

	worker := NewDraftWorker(DraftWorkerDeps{
		TaskRepo:     taskRepo,
		Logger:       zap.NewNop(),
		Concurrency:  1,
		StaleTimeout: time.Hour,
	}).(*draftWorker)
	worker.requeueStale(ctx)

	got, err := taskRepo.GetByID(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusPending {
		t.Fatalf("Status = %q, want pending", got.Status)
	}
}
