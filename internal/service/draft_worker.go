package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"live-mixer/internal/draft"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	draftDefaultConcurrency = 3
	draftPollInterval       = 3 * time.Second
	draftStaleTimeout       = 60 * time.Minute
)

// DraftWorker 剪映草稿任务适配器：DB 抢占/进度/完成，组装委托给 draft.Generator。
type DraftWorker interface {
	Enqueue()
	Process(ctx context.Context, task *model.Task) error
	ProcessWithOptions(ctx context.Context, task *model.Task, opts PhaseOptions) error
	Start(ctx context.Context)
}

type draftWorker struct {
	taskRepo         repository.TaskRepository
	liveMaterialRepo repository.LiveMaterialRepository
	videoProjectRepo repository.VideoProjectRepository
	generator        draft.Generator
	web              webroot.Config
	logger           *zap.Logger
	concurrency      int
	pollInterval     time.Duration
	staleTimeout     time.Duration

	wake      chan struct{}
	startOnce sync.Once
}

// DraftWorkerDeps 构造草稿任务 Worker 所需依赖。
type DraftWorkerDeps struct {
	TaskRepo         repository.TaskRepository
	LiveMaterialRepo repository.LiveMaterialRepository
	VideoProjectRepo repository.VideoProjectRepository
	Generator        draft.Generator
	Web              webroot.Config
	Logger           *zap.Logger
	// Concurrency 单实例并行 Worker 数；<=0 时使用内置默认值（3）。
	Concurrency int
	// StaleTimeout processing 孤儿回收阈值；<=0 时使用内置默认值（60 分钟）。
	StaleTimeout time.Duration
}

// NewDraftWorker 创建剪映草稿任务后台 Worker（任务生命周期 + 调用 draft.Generator）。
func NewDraftWorker(deps DraftWorkerDeps) DraftWorker {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	concurrency := deps.Concurrency
	if concurrency <= 0 {
		concurrency = draftDefaultConcurrency
	}
	staleTimeout := deps.StaleTimeout
	if staleTimeout <= 0 {
		staleTimeout = draftStaleTimeout
	}
	return &draftWorker{
		taskRepo:         deps.TaskRepo,
		liveMaterialRepo: deps.LiveMaterialRepo,
		videoProjectRepo: deps.VideoProjectRepo,
		generator:        deps.Generator,
		web:              deps.Web,
		logger:           logger,
		concurrency:      concurrency,
		pollInterval:     draftPollInterval,
		staleTimeout:     staleTimeout,
		wake:             make(chan struct{}, 1),
	}
}

// Enqueue 非阻塞唤醒调度器。
func (w *draftWorker) Enqueue() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Start 启动并发领取循环与定时 poll。
func (w *draftWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		n := w.concurrency
		if n <= 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			go w.loop(ctx, i)
		}
		go w.pollLoop(ctx)
		w.requeueStale(ctx)
		w.Enqueue()
		w.logger.Info("剪映草稿 Worker 已启动",
			zap.Int("concurrency", n),
			zap.Duration("stale_timeout", w.staleTimeout),
		)
	})
}

func (w *draftWorker) pollLoop(ctx context.Context) {
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

func (w *draftWorker) requeueStale(ctx context.Context) {
	n, err := w.taskRepo.RequeueStaleProcessingByType(ctx, model.TaskTypeDraft, w.staleTimeout)
	if err != nil {
		w.logger.Warn("回收超时草稿任务失败", zap.Error(err))
		return
	}
	if n > 0 {
		w.logger.Warn("已将超时草稿任务重新排队", zap.Int64("count", n))
	}
}

func (w *draftWorker) loop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.drain(ctx, workerID)
		}
	}
}

func (w *draftWorker) drain(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		task, err := w.taskRepo.ClaimPendingByType(ctx, model.TaskTypeDraft)
		if err != nil {
			w.logger.Error("抢占剪映草稿任务失败",
				zap.Int("worker_id", workerID),
				zap.Error(err),
			)
			return
		}
		if task == nil {
			return
		}
		w.logger.Info("已抢占剪映草稿任务",
			zap.Int("worker_id", workerID),
			zap.String("task_id", task.ID),
			zap.Int64("version", task.Version),
		)
		if err := w.Process(ctx, task); err != nil {
			w.logger.Error("剪映草稿任务执行失败",
				zap.String("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 执行单条草稿任务完整流程。
func (w *draftWorker) Process(ctx context.Context, task *model.Task) error {
	return w.ProcessWithOptions(ctx, task, standalonePhaseOptions())
}

// ProcessWithOptions 从任务加载上下文，调用 draft.Generator，并回写任务状态。
func (w *draftWorker) ProcessWithOptions(ctx context.Context, task *model.Task, opts PhaseOptions) error {
	if task == nil {
		return fmt.Errorf("task 不能为空")
	}
	if w.generator == nil {
		return w.fail(ctx, task.ID, 0, fmt.Errorf("草稿 Generator 未配置"))
	}

	var lastProgress int16
	setProgress := func(local int16) int16 {
		p := mapPhaseProgress(opts, local)
		lastProgress = p
		_ = w.taskRepo.UpdateProgress(ctx, task.ID, p)
		return p
	}

	progress := setProgress(5)

	ext, err := parseTaskExt(task.Ext)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("解析任务 ext 失败: %w", err))
	}
	projectID := model.UintValue(task.VideoProjectID)
	if projectID == 0 {
		projectID = ext.VideoProjectID
	}
	if projectID == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("任务缺少 video_project_id"))
	}

	project, err := w.videoProjectRepo.GetByID(ctx, projectID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询剪辑项目失败: %w", err))
	}

	liveID := ext.LiveID
	if liveID == 0 {
		liveID = project.LiveID
	}
	material, err := w.liveMaterialRepo.GetByID(ctx, liveID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询直播素材失败: %w", err))
	}
	// 优先使用创建任务时快照的 live_url，素材侧为空时仍可继续生成草稿。
	if material.LiveURL == "" {
		material.LiveURL = task.LiveURL
	}
	if material.LiveURL == "" {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("直播素材 live_url 为空"))
	}

	// 画布尺寸优先使用创建时写入 task 的快照；缺失时再按项目/默认值解析。
	width, height := draft.ResolveCanvasSize(task.Width, task.Height, project)

	result, err := w.generator.Build(ctx, draft.Request{
		JobID:      task.ID,
		Material:   material,
		Project:    project,
		CanvasW:    width,
		CanvasH:    height,
		StagingDir: w.web.StagingDir(task.ID),
		RecordDir:  w.web.CapCutMateRecordDir(task.ID),
		Progress:   func(local int16) { setProgress(local) },
	})
	if err != nil {
		return w.fail(ctx, task.ID, lastProgress, err)
	}

	progress = setProgress(90)
	if err := w.taskRepo.UpdateDraftURL(ctx, task.ID, result.DraftURL); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("回写 task.draft_url 失败: %w", err))
	}
	if result.ClipsTarURL != "" {
		if err := w.taskRepo.UpdateClipsTarURL(ctx, task.ID, result.ClipsTarURL); err != nil {
			return w.fail(ctx, task.ID, progress, fmt.Errorf("回写 task.clips_tar_url 失败: %w", err))
		}
	}

	ext.LiveID = liveID
	ext.VideoProjectID = project.ID
	extRaw, err := marshalTaskExt(ext)
	if err != nil {
		extRaw = fmt.Sprintf(`{"live_id":%d,"video_project_id":%d}`, liveID, project.ID)
	}

	if opts.MarkComplete {
		if err := w.taskRepo.MarkCompleted(ctx, task.ID, mapPhaseProgress(opts, 100), extRaw); err != nil {
			return fmt.Errorf("标记任务完成失败: %w", err)
		}
	} else {
		_ = w.taskRepo.UpdateExt(ctx, task.ID, extRaw)
		_ = setProgress(100)
	}
	w.logger.Info("剪映草稿阶段完成",
		zap.String("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.String("draft_url", result.DraftURL),
		zap.String("clips_tar_url", result.ClipsTarURL),
		zap.Bool("mark_complete", opts.MarkComplete),
	)
	return nil
}

func (w *draftWorker) fail(ctx context.Context, taskID string, progress int16, err error) error {
	w.logger.Error("剪映草稿任务失败",
		zap.String("task_id", taskID),
		zap.Int16("progress", progress),
		zap.Error(err),
	)
	if failErr := w.taskRepo.MarkFailed(ctx, taskID, progress, err.Error()); failErr != nil {
		return fmt.Errorf("剪映草稿失败且写入失败状态异常: task=%w, db=%v", err, failErr)
	}
	return err
}
