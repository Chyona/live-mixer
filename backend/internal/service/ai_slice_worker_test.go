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

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type mockLLMChat struct {
	content  string
	err      error
	calls    int
	lastMsgs []llm.ChatMessage
}

func (m *mockLLMChat) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	m.calls++
	m.lastMsgs = messages
	if m.err != nil {
		return "", m.err
	}
	return m.content, nil
}

func (m *mockLLMChat) ChatStructured(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.Chat(ctx, messages)
}

func (m *mockLLMChat) ChatThinking(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.Chat(ctx, messages)
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
			"utterances":[
				{
					"additions":{"speaker":"1"},
					"start_time":0,"end_time":1000,"text":"开场闲聊",
					"words":[{"start_time":0,"end_time":1000,"text":"开场闲聊"}]
				},
				{
					"additions":{"speaker":"1"},
					"start_time":5000,"end_time":7000,"text":"今天上新很好看",
					"words":[
						{"start_time":5000,"end_time":5800,"text":"今天"},
						{"start_time":5800,"end_time":7000,"text":"上新很好看"}
					]
				},
				{
					"additions":{"speaker":"1"},
					"start_time":7100,"end_time":8000,"text":"面料垂感特别好",
					"words":[{"start_time":7100,"end_time":8000,"text":"面料垂感特别好"}]
				},
				{
					"additions":{"speaker":"1"},
					"start_time":20000,"end_time":21000,"text":"窗外闲话",
					"words":[{"start_time":20000,"end_time":21000,"text":"窗外闲话"}]
				}
			]
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

	// clips0 仅覆盖中间两句（下标 0/1 对应筛选后列表）。
	project := &model.VideoProject{
		Name: "项目A", LiveID: material.ID, CreatedBy: 1, PromptID: 1,
		Clips0: []model.ClipRange{{StartTime: 4500, EndTime: 8500}},
		Clips1: []model.ClipWithText{},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type:           model.TaskTypeAISlice,
		Status:         model.TaskStatusPending,
		CreatedBy:      1,
		SysPrompt:      "你是切片助手",
		VideoProjectID: model.NewUintPtr(project.ID),
		Ext:            ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	mock := &mockLLMChat{content: `[0, 1]`}
	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, mock, zap.NewNop(), 0, 0, webroot.Config{})

	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if mock.calls != 1 {
		t.Errorf("llm calls = %d, want 1", mock.calls)
	}
	if len(mock.lastMsgs) != 2 || mock.lastMsgs[0].Role != "system" || mock.lastMsgs[1].Role != "user" {
		t.Fatalf("messages = %#v", mock.lastMsgs)
	}
	if mock.lastMsgs[0].Content != "你是切片助手" {
		t.Errorf("sys = %q", mock.lastMsgs[0].Content)
	}
	usr := mock.lastMsgs[1].Content
	if !strings.Contains(usr, "## 视频ASR") || !strings.Contains(usr, "## 输出格式") {
		t.Errorf("usr prompt missing sections: %s", usr)
	}
	if !strings.Contains(usr, "[0] (5.00 - 7.00) 今天上新很好看") {
		t.Errorf("usr missing segment0: %s", usr)
	}
	if !strings.Contains(usr, "[1] (7.10 - 8.00) 面料垂感特别好") {
		t.Errorf("usr missing segment1: %s", usr)
	}
	if strings.Contains(usr, "开场闲聊") || strings.Contains(usr, "窗外闲话") {
		t.Errorf("usr should not include out-of-range ASR: %s", usr)
	}

	got, err := taskRepo.GetByID(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusCompleted || got.Progress != 100 {
		t.Errorf("task status/progress = %s/%d", got.Status, got.Progress)
	}
	if got.UsrPrompt == "" || !strings.Contains(got.UsrPrompt, "## 视频ASR") {
		t.Errorf("usr_prompt not persisted: %q", got.UsrPrompt)
	}
	if got.SysPrompt != "你是切片助手" {
		t.Errorf("sys_prompt = %q", got.SysPrompt)
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
	// clips0 作为输入窗口，不应被 Worker 覆盖。
	if len(updated.Clips0) != 1 || updated.Clips0[0].StartTime != 4500 {
		t.Errorf("clips0 mutated: %#v", updated.Clips0)
	}
	if len(updated.Clips1) != 1 {
		t.Fatalf("clips1 len = %d, want 1 (adjacent merged), got %#v", len(updated.Clips1), updated.Clips1)
	}
	merged := updated.Clips1[0]
	if merged.Text != "今天上新很好看面料垂感特别好" || merged.StartTime != 5000 || merged.EndTime != 8000 {
		t.Errorf("clips1[0] = %#v", merged)
	}
	if len(merged.Words) != 3 {
		t.Errorf("merged words len = %d, want 3", len(merged.Words))
	}
}

func TestAISliceWorker_Process_SkipOutOfRangeIndices(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "直播", LiveURL: "https://example.com/a.mp4",
		LiveASR: `{"result":{"utterances":[
			{"additions":{},"start_time":0,"end_time":100,"text":"A","words":[]},
			{"additions":{},"start_time":200,"end_time":300,"text":"B","words":[]}
		]}}`,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	_ = liveRepo.Create(ctx, material)
	project := &model.VideoProject{
		Name: "p", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 500}},
		Clips1: []model.ClipWithText{},
	}
	_ = projectRepo.Create(ctx, project)
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "sys", VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)

	// 下标 0 有效，99 越界应跳过。
	mock := &mockLLMChat{content: `[0, 99]`}
	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, mock, zap.NewNop(), 0, 0, webroot.Config{})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	updated, _ := projectRepo.GetByID(ctx, project.ID)
	if len(updated.Clips1) != 1 || updated.Clips1[0].Text != "A" {
		t.Fatalf("clips1 = %#v", updated.Clips1)
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
	project := &model.VideoProject{
		Name: "p", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 200}},
		Clips1: []model.ClipWithText{},
	}
	_ = projectRepo.Create(ctx, project)
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "sys", VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)

	mock := &mockLLMChat{err: errors.New("timeout")}
	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, mock, zap.NewNop(), 0, 0, webroot.Config{})
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected error")
	}
	got, _ := taskRepo.GetByID(ctx, claimed.ID)
	if got.Status != model.TaskStatusFailed {
		t.Errorf("status = %s, want failed", got.Status)
	}
}

func TestAISliceWorker_Process_EmptyClips0(t *testing.T) {
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
	project := &model.VideoProject{
		Name: "p", LiveID: material.ID, CreatedBy: 1,
		Clips0: []model.ClipRange{}, Clips1: []model.ClipWithText{},
	}
	_ = projectRepo.Create(ctx, project)
	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "sys", VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	_ = taskRepo.Create(ctx, task)
	claimed, _ := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)

	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, &mockLLMChat{content: `[]`}, zap.NewNop(), 0, 0, webroot.Config{})
	if err := worker.Process(ctx, claimed); err == nil {
		t.Fatal("expected empty clips0 error")
	}
}

func TestBuildAISliceUserPrompt(t *testing.T) {
	got := buildAISliceUserPrompt([]asr.Utterance{
		{StartTime: 1000, EndTime: 2000, Text: "第一句"},
		{StartTime: 3000, EndTime: 4000, Text: "第二句"},
	})
	if !strings.HasPrefix(got, "## 视频ASR\n") {
		t.Fatalf("prefix = %q", got[:20])
	}
	if !strings.Contains(got, "[0] (1.00 - 2.00) 第一句\n") {
		t.Errorf("missing line0: %s", got)
	}
	if !strings.Contains(got, "[1] (3.00 - 4.00) 第二句\n") {
		t.Errorf("missing line1: %s", got)
	}
	if !strings.Contains(got, "## 输出格式") || !strings.Contains(got, `[2, 5, 9, 13]`) {
		t.Errorf("missing output format: %s", got)
	}
}

func TestAISliceWorker_DefaultConcurrencyIsSix(t *testing.T) {
	w := NewAISliceWorker(nil, nil, nil, nil, zap.NewNop(), 0, 0, webroot.Config{}).(*aiSliceWorker)
	if w.concurrency != 6 {
		t.Fatalf("concurrency = %d, want 6", w.concurrency)
	}
	if aiSliceDefaultConcurrency != 6 {
		t.Fatalf("aiSliceDefaultConcurrency = %d, want 6", aiSliceDefaultConcurrency)
	}
}

func TestAISliceWorker_UsesConfiguredConcurrency(t *testing.T) {
	w := NewAISliceWorker(nil, nil, nil, nil, zap.NewNop(), 3, 0, webroot.Config{}).(*aiSliceWorker)
	if w.concurrency != 3 {
		t.Fatalf("concurrency = %d, want 3", w.concurrency)
	}
}

// TestAISliceWorker_DefaultAndConfiguredStaleTimeout 验证 AI 切片孤儿回收超时默认值与配置覆盖。
func TestAISliceWorker_DefaultAndConfiguredStaleTimeout(t *testing.T) {
	defaultW := NewAISliceWorker(nil, nil, nil, nil, zap.NewNop(), 0, 0, webroot.Config{}).(*aiSliceWorker)
	if defaultW.staleTimeout != aiSliceStaleTimeout {
		t.Fatalf("default staleTimeout = %v, want %v", defaultW.staleTimeout, aiSliceStaleTimeout)
	}
	custom := NewAISliceWorker(nil, nil, nil, nil, zap.NewNop(), 0, 12*time.Minute, webroot.Config{}).(*aiSliceWorker)
	if custom.staleTimeout != 12*time.Minute {
		t.Fatalf("custom staleTimeout = %v, want 12m", custom.staleTimeout)
	}
}

// TestAISliceWorker_RequeueStaleProcessing 验证将超时 processing 改回 pending。
func TestAISliceWorker_RequeueStaleProcessing(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	ctx := context.Background()

	stale := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing,
		Progress: 30, CreatedBy: 1, SysPrompt: "sys",
	}
	if err := taskRepo.Create(ctx, stale); err != nil {
		t.Fatalf("Create: %v", err)
	}
	staleAt := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&model.Task{}).Where("id = ?", stale.ID).Update("updated_at", staleAt).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	worker := NewAISliceWorker(taskRepo, nil, nil, nil, zap.NewNop(), 1, time.Hour, webroot.Config{}).(*aiSliceWorker)
	worker.requeueStale(ctx)

	got, err := taskRepo.GetByID(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != model.TaskStatusPending {
		t.Fatalf("Status = %q, want pending", got.Status)
	}
	if got.Progress != 0 {
		t.Errorf("Progress = %d, want 0", got.Progress)
	}
}

// TestAISliceWorker_Process_PreprocessClips0 验证重叠 clips0 排序合并后用于筛选，
// staging 落盘 before/after，且不回写 video_project.clips0。
func TestAISliceWorker_Process_PreprocessClips0(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()
	webRoot := t.TempDir()

	liveASR := `{
		"result":{
			"utterances":[
				{"additions":{},"start_time":0,"end_time":1000,"text":"开场",
					"words":[{"start_time":0,"end_time":1000,"text":"开场"}]},
				{"additions":{},"start_time":2000,"end_time":3000,"text":"中段A",
					"words":[{"start_time":2000,"end_time":3000,"text":"中段A"}]},
				{"additions":{},"start_time":5000,"end_time":6000,"text":"中段B",
					"words":[{"start_time":5000,"end_time":6000,"text":"中段B"}]},
				{"additions":{},"start_time":9000,"end_time":10000,"text":"结尾",
					"words":[{"start_time":9000,"end_time":10000,"text":"结尾"}]}
			]
		}
	}`
	material := &model.LiveMaterial{
		Name: "直播预处理", LiveURL: "https://example.com/a.mp4",
		LiveASR: liveASR, ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}

	// 乱序且重叠：合并后应为 [1500,6500]，覆盖中段A/B，不含开场/结尾。
	rawClips0 := []model.ClipRange{
		{StartTime: 4000, EndTime: 6500},
		{StartTime: 1500, EndTime: 3500},
		{StartTime: 3000, EndTime: 5000},
	}
	project := &model.VideoProject{
		Name: "预处理项目", LiveID: material.ID, CreatedBy: 1, PromptID: 1,
		Clips0: rawClips0,
		Clips1: []model.ClipWithText{},
	}
	if err := projectRepo.Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	ext, _ := marshalTaskExt(TaskExt{LiveID: material.ID, VideoProjectID: project.ID})
	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusPending, CreatedBy: 1,
		SysPrompt: "sys", VideoProjectID: model.NewUintPtr(project.ID), Ext: ext,
	}
	if err := taskRepo.Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	claimed, err := taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISlice)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", err, claimed)
	}

	mock := &mockLLMChat{content: `[0, 1]`}
	worker := NewAISliceWorker(taskRepo, liveRepo, projectRepo, mock, zap.NewNop(), 0, 0, webroot.Config{RootDir: webRoot})
	if err := worker.Process(ctx, claimed); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	usr := mock.lastMsgs[1].Content
	if !strings.Contains(usr, "中段A") || !strings.Contains(usr, "中段B") {
		t.Errorf("usr missing merged-range ASR: %s", usr)
	}
	if strings.Contains(usr, "开场") || strings.Contains(usr, "结尾") {
		t.Errorf("usr should not include out-of-range ASR: %s", usr)
	}

	stagingDir := filepath.Join(webRoot, "staging", claimed.ID, "ai_slice")
	beforeRaw, err := os.ReadFile(filepath.Join(stagingDir, "clips0_before.json"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	afterRaw, err := os.ReadFile(filepath.Join(stagingDir, "clips0_after.json"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	var beforeRec, afterRec aiSliceClips0DebugRecord
	if err := json.Unmarshal(beforeRaw, &beforeRec); err != nil {
		t.Fatalf("before unmarshal: %v", err)
	}
	if err := json.Unmarshal(afterRaw, &afterRec); err != nil {
		t.Fatalf("after unmarshal: %v", err)
	}
	if beforeRec.Count != 3 || afterRec.Count != 1 {
		t.Fatalf("before=%d after=%d", beforeRec.Count, afterRec.Count)
	}
	if afterRec.Clips[0].StartTime != 1500 || afterRec.Clips[0].EndTime != 6500 {
		t.Fatalf("after clips = %#v", afterRec.Clips)
	}

	updated, err := projectRepo.GetByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("Get project: %v", err)
	}
	if len(updated.Clips0) != 3 {
		t.Fatalf("clips0 mutated len=%d", len(updated.Clips0))
	}
	if updated.Clips0[0].StartTime != 4000 || updated.Clips0[1].StartTime != 1500 || updated.Clips0[2].StartTime != 3000 {
		t.Errorf("clips0 order/values mutated: %#v", updated.Clips0)
	}
}
