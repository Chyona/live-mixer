package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// heartbeatCountingRepo 统计心跳调用次数；用于断言「停止后不再写库」，比对比时间戳更确定。
type heartbeatCountingRepo struct {
	repository.TaskRepository
	heartbeats atomic.Int32
}

func (r *heartbeatCountingRepo) HeartbeatProgress(ctx context.Context, id string, version int64, progress int16) (bool, error) {
	r.heartbeats.Add(1)
	return r.TaskRepository.HeartbeatProgress(ctx, id, version, progress)
}

func (r *heartbeatCountingRepo) heartbeatCalls() int32 {
	return r.heartbeats.Load()
}

// waitHeartbeatCalls 等到心跳至少被调用 want 次，超时即失败。
func waitHeartbeatCalls(t *testing.T, repo *heartbeatCountingRepo, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if repo.heartbeatCalls() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("心跳调用数 = %d, want >= %d", repo.heartbeatCalls(), want)
}

// assertHeartbeatQuiet 断言再过若干个心跳周期也一次都不写库（先留出观察窗口，避开在途的那一次）。
func assertHeartbeatQuiet(t *testing.T, repo *heartbeatCountingRepo, interval time.Duration) {
	t.Helper()
	time.Sleep(3 * interval)
	settled := repo.heartbeatCalls()
	time.Sleep(3 * interval)
	if got := repo.heartbeatCalls(); got != settled {
		t.Fatalf("心跳应已停止，但调用数仍在增长: %d -> %d", settled, got)
	}
}

func backdateTask(t *testing.T, db *gorm.DB, id string, at time.Time) {
	t.Helper()
	if err := db.Model(&model.Task{}).Where("id = ?", id).Update("updated_at", at).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}
}

// TestTaskHeartbeatIntervalBeatsStaleTimeouts 心跳间隔必须远小于各类型的孤儿回收阈值，
// 否则「心跳还在跑」和「被判成孤儿」会同时成立。
func TestTaskHeartbeatIntervalBeatsStaleTimeouts(t *testing.T) {
	if taskHeartbeatInterval <= 0 {
		t.Fatalf("taskHeartbeatInterval = %v, want > 0", taskHeartbeatInterval)
	}
	for _, stale := range []time.Duration{
		aiSliceStaleTimeout,         // 20 分钟：AI 切片
		aiSliceDraftStaleTimeout,    // 90 分钟：一键成片
		liveMaterialASRStaleTimeout, // 60 分钟：直播素材 ASR
	} {
		if taskHeartbeatInterval > stale/4 {
			t.Errorf("心跳间隔 %v 相对回收阈值 %v 过大（应 <= 阈值的 1/4）", taskHeartbeatInterval, stale)
		}
	}
}

// TestStartTaskHeartbeat_RefreshesUpdatedAtAndStops 验证心跳刷新 updated_at 且 stop() 之后不再写库。
func TestStartTaskHeartbeat_RefreshesUpdatedAtAndStops(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	base := repository.NewTaskRepository(db)
	repo := &heartbeatCountingRepo{TaskRepository: base}
	ctx := context.Background()

	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing,
		Progress: 40, CreatedBy: 1,
	}
	if err := base.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 模拟「进入大模型调用后一直没写库」：回拨 30 分钟（远超 AI 切片 20 分钟的回收阈值）。
	staleAt := time.Now().Add(-30 * time.Minute)
	backdateTask(t, db, task.ID, staleAt)

	stop := startTaskHeartbeat(ctx, repo, zap.NewNop(), task.ID, task.Version, 40, 20*time.Millisecond)
	waitHeartbeatCalls(t, repo, 1, 2*time.Second)

	got, err := base.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.UpdatedAt.After(staleAt) {
		t.Errorf("UpdatedAt = %v 未刷新（回拨值 %v）：心跳没起作用", got.UpdatedAt, staleAt)
	}
	if got.Status != model.TaskStatusProcessing {
		t.Errorf("Status = %q, want processing", got.Status)
	}
	if got.Progress != 40 {
		t.Errorf("Progress = %d, want 40（心跳只刷新时间，不改进度）", got.Progress)
	}

	// stop 幂等；停止后必须彻底不写库（大模型返回后由后续步骤自己写）。
	stop()
	stop()
	assertHeartbeatQuiet(t, repo, 20*time.Millisecond)
}

// TestStartTaskHeartbeat_StopsWhenLeaseLost 租约失效（任务被回收重排、version 已递增）后必须停手，
// 否则旧执行的进度会盖到新执行的记录上。
func TestStartTaskHeartbeat_StopsWhenLeaseLost(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	base := repository.NewTaskRepository(db)
	repo := &heartbeatCountingRepo{TaskRepository: base}
	ctx := context.Background()

	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing,
		Progress: 40, CreatedBy: 1,
	}
	if err := base.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	stop := startTaskHeartbeat(ctx, repo, zap.NewNop(), task.ID, task.Version, 40, 20*time.Millisecond)
	defer stop()
	waitHeartbeatCalls(t, repo, 1, 2*time.Second)

	// 另一处把这条 processing 任务判成孤儿并重排（version 递增，进度归零）。
	backdateTask(t, db, task.ID, time.Now().Add(-time.Hour))
	if _, err := base.RequeueStaleProcessingByType(ctx, model.TaskTypeAISlice, time.Minute); err != nil {
		t.Fatalf("RequeueStaleProcessingByType: %v", err)
	}
	requeued, err := base.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if requeued.Version == task.Version {
		t.Fatalf("Version = %d，重排后应递增（测试前提不成立）", requeued.Version)
	}

	assertHeartbeatQuiet(t, repo, 20*time.Millisecond)

	final, err := base.GetByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if final.Progress != 0 {
		t.Errorf("Progress = %d, want 0：旧租约的心跳把进度写了回去", final.Progress)
	}
	if final.Status != model.TaskStatusPending {
		t.Errorf("Status = %q, want pending", final.Status)
	}
}

// TestStartTaskHeartbeat_StopsOnContextCancel 任务级 ctx 取消后心跳必须自行退出（兜底，不依赖调用方 stop）。
func TestStartTaskHeartbeat_StopsOnContextCancel(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	base := repository.NewTaskRepository(db)
	repo := &heartbeatCountingRepo{TaskRepository: base}
	ctx, cancel := context.WithCancel(context.Background())

	task := &model.Task{
		Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing,
		Progress: 40, CreatedBy: 1,
	}
	if err := base.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	stop := startTaskHeartbeat(ctx, repo, zap.NewNop(), task.ID, task.Version, 40, 20*time.Millisecond)
	defer stop()
	waitHeartbeatCalls(t, repo, 1, 2*time.Second)

	cancel()
	// dbWriteCtx 会把写库 context 换成 Background，所以这里确实是在验证「goroutine 按 ctx 退出」，
	// 而不是写库被取消顺带挡住。
	assertHeartbeatQuiet(t, repo, 20*time.Millisecond)
}

// TestStartTaskHeartbeat_NoopOnInvalidInput 参数不合法时返回空实现，不 panic、不起 goroutine。
func TestStartTaskHeartbeat_NoopOnInvalidInput(t *testing.T) {
	db := setupAISliceWorkerTestDB(t)
	base := repository.NewTaskRepository(db)
	repo := &heartbeatCountingRepo{TaskRepository: base}
	ctx := context.Background()

	task := &model.Task{Type: model.TaskTypeAISlice, Status: model.TaskStatusProcessing, CreatedBy: 1}
	if err := base.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, stop := range []func(){
		startTaskHeartbeat(ctx, nil, zap.NewNop(), task.ID, task.Version, 40, 20*time.Millisecond),
		startTaskHeartbeat(ctx, repo, zap.NewNop(), "", task.Version, 40, 20*time.Millisecond),
		startTaskHeartbeat(ctx, repo, zap.NewNop(), task.ID, task.Version, 40, 0),
	} {
		stop()
		stop()
	}
	time.Sleep(100 * time.Millisecond)
	if got := repo.heartbeatCalls(); got != 0 {
		t.Fatalf("心跳调用数 = %d, want 0", got)
	}
}

// setupClaimedAISliceTask 构造「一条已被抢占的 AI 切片任务」及其素材/项目，供心跳与大模型调用相关测试复用。
func setupClaimedAISliceTask(t *testing.T) (*gorm.DB, *model.Task) {
	t.Helper()
	db := setupAISliceWorkerTestDB(t)
	taskRepo := repository.NewTaskRepository(db)
	liveRepo := repository.NewLiveMaterialRepository(db)
	projectRepo := repository.NewVideoProjectRepository(db)
	ctx := context.Background()

	material := &model.LiveMaterial{
		Name: "心跳直播", LiveURL: "https://example.com/a.mp4",
		LiveASR: `{"result":{"utterances":[
			{"additions":{},"start_time":0,"end_time":1000,"text":"第一句","words":[]},
			{"additions":{},"start_time":2000,"end_time":3000,"text":"第二句","words":[]}
		]}}`,
		ASRStatus: model.ASRStatusCompleted, ASRProgress: 100, CreatedBy: 1,
	}
	if err := liveRepo.Create(ctx, material); err != nil {
		t.Fatalf("create material: %v", err)
	}
	project := &model.VideoProject{
		Name: "心跳项目", LiveID: material.ID, CreatedBy: 1, PromptID: 1,
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 5000}},
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
	return db, claimed
}
