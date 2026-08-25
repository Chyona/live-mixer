package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	// aiSliceDraftDefaultConcurrency 单实例内并行抢占/执行一键成片的 Worker 数（配置缺失时回落）。
	aiSliceDraftDefaultConcurrency = 3
	// aiSliceDraftPollInterval 无待处理任务时的 DB 轮询间隔。
	aiSliceDraftPollInterval = 3 * time.Second
	// aiSliceDraftStaleTimeout processing 超时未更新进度则自动改回 pending。
	aiSliceDraftStaleTimeout = 90 * time.Minute
)

// AISliceDraftWorker 一键成片编排器：先 AI 切片（写 clips1），再生成剪映草稿并导出视频。
type AISliceDraftWorker interface {
	// Enqueue 唤醒调度循环尝试领取任务（非阻塞）。
	Enqueue()
	// Process 执行已抢占的一键成片任务：AI 切片 → 草稿 → 视频。
	Process(ctx context.Context, task *model.Task) error
	// Start 启动后台 Worker 循环。
	Start(ctx context.Context)
}

type aiSliceDraftWorker struct {
	taskRepo         repository.TaskRepository
	videoProjectRepo repository.VideoProjectRepository
	aiSlice          AISliceWorker
	draft            DraftWorker
	logger           *zap.Logger
	concurrency      int
	pollInterval     time.Duration
	staleTimeout     time.Duration

	wake      chan struct{}
	startOnce sync.Once
}

// NewAISliceDraftWorker 创建一键成片编排 Worker，复用 AI 切片与草稿阶段实现。
// concurrency 为单实例并行 Worker 数；<=0 时使用内置默认值（3）。
// staleTimeout 为 processing 孤儿回收阈值；<=0 时使用内置默认值（90 分钟）。
func NewAISliceDraftWorker(
	taskRepo repository.TaskRepository,
	videoProjectRepo repository.VideoProjectRepository,
	aiSlice AISliceWorker,
	draftWorker DraftWorker,
	logger *zap.Logger,
	concurrency int,
	staleTimeout time.Duration,
) AISliceDraftWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if concurrency <= 0 {
		concurrency = aiSliceDraftDefaultConcurrency
	}
	if staleTimeout <= 0 {
		staleTimeout = aiSliceDraftStaleTimeout
	}
	return &aiSliceDraftWorker{
		taskRepo:         taskRepo,
		videoProjectRepo: videoProjectRepo,
		aiSlice:          aiSlice,
		draft:            draftWorker,
		logger:           logger,
		concurrency:      concurrency,
		pollInterval:     aiSliceDraftPollInterval,
		staleTimeout:     staleTimeout,
		wake:             make(chan struct{}, 1),
	}
}

// Enqueue 非阻塞唤醒调度器。
func (w *aiSliceDraftWorker) Enqueue() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Start 启动并发领取循环与定时 poll，并在启动时立即回收孤儿 processing 任务。
func (w *aiSliceDraftWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		n := w.concurrency
		if n <= 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			go w.loop(ctx, i)
		}
		go w.pollLoop(ctx)
		// 启动即回收：服务重启后 processing 孤儿无需等待第一个 poll 周期。
		w.requeueStale(ctx)
		w.Enqueue()
		w.logger.Info("一键成片 Worker 已启动",
			zap.Int("concurrency", n),
			zap.Duration("stale_timeout", w.staleTimeout),
		)
	})
}

// pollLoop 定时回收超时 processing，并唤醒领取 pending。
func (w *aiSliceDraftWorker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.requeueStale(ctx)
			w.Enqueue()
		}
	}
}

// requeueStale 将 updated_at 超时未刷新的一键成片 processing 任务改回 pending。
func (w *aiSliceDraftWorker) requeueStale(ctx context.Context) {
	n, err := w.taskRepo.RequeueStaleProcessingByType(ctx, model.TaskTypeAISliceDraft, w.staleTimeout)
	if err != nil {
		w.logger.Warn("回收超时一键成片任务失败", zap.Error(err))
		return
	}
	if n > 0 {
		w.logger.Warn("已将超时一键成片任务重新排队", zap.Int64("count", n))
	}
}

func (w *aiSliceDraftWorker) loop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.drain(ctx, workerID)
		}
	}
}

func (w *aiSliceDraftWorker) drain(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		task, err := w.taskRepo.ClaimPendingByType(ctx, model.TaskTypeAISliceDraft)
		if err != nil {
			w.logger.Error("抢占一键成片任务失败",
				zap.Int("worker_id", workerID),
				zap.Error(err),
			)
			return
		}
		if task == nil {
			return
		}
		w.logger.Info("已抢占一键成片任务",
			zap.Int("worker_id", workerID),
			zap.String("task_id", task.ID),
			zap.Int64("version", task.Version),
		)
		if err := w.Process(ctx, task); err != nil {
			w.logger.Error("一键成片任务执行失败",
				zap.String("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 串联执行：AI 切片（进度 0–50）→ 校验 clips1 → 草稿（进度 50–100）。
func (w *aiSliceDraftWorker) Process(ctx context.Context, task *model.Task) error {
	if task == nil {
		return fmt.Errorf("task 不能为空")
	}
	if w.aiSlice == nil {
		return w.fail(ctx, task.ID, 0, fmt.Errorf("AI 切片 Worker 未配置"))
	}
	if w.draft == nil {
		return w.fail(ctx, task.ID, 0, fmt.Errorf("草稿 Worker 未配置"))
	}

	// 阶段一：AI 切片写 clips1，不标记任务完成。
	if err := w.aiSlice.ProcessWithOptions(ctx, task, PhaseOptions{
		MarkComplete: false,
		ProgressBase: 0,
		ProgressSpan: 50,
	}); err != nil {
		// 子阶段已 MarkFailed。
		return err
	}

	projectID := model.UintValue(task.VideoProjectID)
	if projectID == 0 {
		if ext, err := parseTaskExt(task.Ext); err == nil {
			projectID = ext.VideoProjectID
		}
	}
	if projectID == 0 {
		return w.fail(ctx, task.ID, 50, fmt.Errorf("任务缺少 video_project_id"))
	}

	project, err := w.videoProjectRepo.GetByID(ctx, projectID)
	if err != nil {
		return w.fail(ctx, task.ID, 50, fmt.Errorf("查询剪辑项目失败: %w", err))
	}
	// 一键成片必须使用 AI 切片产出的 clips1，不能回退到分析窗口 clips0。
	if len(project.Clips1) == 0 {
		return w.fail(ctx, task.ID, 50, fmt.Errorf("AI 切片结果为空，无法生成草稿"))
	}

	// 阶段二：生成剪映草稿（成功后继续 gen_video）并标记任务完成。
	if err := w.draft.ProcessWithOptions(ctx, task, PhaseOptions{
		MarkComplete: true,
		ProgressBase: 50,
		ProgressSpan: 50,
	}); err != nil {
		return err
	}

	w.logger.Info("一键成片任务完成",
		zap.String("task_id", task.ID),
		zap.Uint("video_project_id", projectID),
		zap.Int("clips1", len(project.Clips1)),
	)
	return nil
}

func (w *aiSliceDraftWorker) fail(ctx context.Context, taskID string, progress int16, err error) error {
	w.logger.Error("一键成片任务失败",
		zap.String("task_id", taskID),
		zap.Int16("progress", progress),
		zap.Error(err),
	)
	if failErr := w.taskRepo.MarkFailed(ctx, taskID, progress, err.Error()); failErr != nil {
		return fmt.Errorf("一键成片失败且写入失败状态异常: task=%w, db=%v", err, failErr)
	}
	return err
}
