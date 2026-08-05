package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"live-mixer/internal/draft"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	draftDefaultConcurrency = 3
	draftPollInterval       = 3 * time.Second
	draftStaleTimeout       = 60 * time.Minute

	// 草稿组装占用本地进度 0–85；视频生成占用 85–100。
	draftPhaseLocalProgress = int16(85)
	videoPhaseLocalStart    = int16(85)
	videoPhaseLocalSpan     = int16(14) // 85→99；最终 MarkCompleted 到 100
)

// VideoExporter 草稿成功后导出成片视频的能力（通常由 capcut-mate gen_video 实现）。
type VideoExporter interface {
	GenerateVideoAndWait(ctx context.Context, draftURL, recordDir string, onProgress capcutmate.VideoProgressCallback) (string, error)
}

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
	videoExporter    VideoExporter
	enableGenVideo   bool
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
	// VideoExporter 草稿成功后生成成片；为 nil 时跳过视频生成（仍写 draft_url 并完成任务）。
	VideoExporter VideoExporter
	// EnableGenVideo 是否调用 gen_video；nil 时默认 true。为 false 时跳过视频生成并直接完成任务。
	EnableGenVideo *bool
	Web            webroot.Config
	Logger         *zap.Logger
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
	enableGenVideo := true
	if deps.EnableGenVideo != nil {
		enableGenVideo = *deps.EnableGenVideo
	}
	return &draftWorker{
		taskRepo:         deps.TaskRepo,
		liveMaterialRepo: deps.LiveMaterialRepo,
		videoProjectRepo: deps.VideoProjectRepo,
		generator:        deps.Generator,
		videoExporter:    deps.VideoExporter,
		enableGenVideo:   enableGenVideo,
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
// 草稿成功后（MarkComplete=true）继续调用 gen_video；视频失败仍保留 draft_url 并标记完成（部分成功）。
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
		// 将 Generator 本地进度压缩到草稿阶段上限，为视频生成预留空间。
		Progress: func(local int16) {
			if local > draftPhaseLocalProgress {
				local = draftPhaseLocalProgress
			}
			setProgress(local)
		},
	})
	if err != nil {
		return w.fail(ctx, task.ID, lastProgress, err)
	}

	progress = setProgress(draftPhaseLocalProgress)
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

	var videoWarn string
	var videoURL string
	if opts.MarkComplete {
		videoURL, videoWarn = w.exportVideo(ctx, task.ID, result.DraftURL, setProgress)
	}

	if opts.MarkComplete {
		if err := w.taskRepo.MarkCompleted(ctx, task.ID, mapPhaseProgress(opts, 100), extRaw); err != nil {
			return fmt.Errorf("标记任务完成失败: %w", err)
		}
		// 视频失败视为部分成功：状态 completed + draft_url 已写入，error_message 记录视频原因。
		if videoWarn != "" {
			_ = w.taskRepo.UpdateErrorMessage(ctx, task.ID, videoWarn)
		}
	} else {
		_ = w.taskRepo.UpdateExt(ctx, task.ID, extRaw)
		_ = setProgress(100)
	}
	w.logger.Info("剪映草稿阶段完成",
		zap.String("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.String("draft_url", result.DraftURL),
		zap.String("video_url", videoURL),
		zap.String("clips_tar_url", result.ClipsTarURL),
		zap.Bool("mark_complete", opts.MarkComplete),
		zap.String("video_warn", videoWarn),
	)
	return nil
}

// exportVideo 在草稿成功后生成成片；失败时返回警告文案（不导致任务失败）。
func (w *draftWorker) exportVideo(
	ctx context.Context,
	taskID, draftURL string,
	setProgress func(int16) int16,
) (videoURL string, warn string) {
	if !w.enableGenVideo {
		w.logger.Info("已关闭 gen_video，跳过视频生成", zap.String("task_id", taskID))
		return "", ""
	}
	if w.videoExporter == nil {
		w.logger.Info("未配置视频导出器，跳过 gen_video", zap.String("task_id", taskID))
		return "", ""
	}

	recordDir := w.web.CapCutMateRecordDir(taskID)
	setProgress(videoPhaseLocalStart)
	videoURL, err := w.videoExporter.GenerateVideoAndWait(ctx, draftURL, recordDir, func(remoteProgress int, status string) {
		local := videoPhaseLocalStart + int16(int(videoPhaseLocalSpan)*clampProgress(remoteProgress)/100)
		setProgress(local)
		w.logger.Info("视频生成进度",
			zap.String("task_id", taskID),
			zap.String("status", status),
			zap.Int("remote_progress", remoteProgress),
			zap.Int16("local_progress", local),
		)
	})
	if err != nil {
		w.logger.Warn("视频生成失败，任务按部分成功完成",
			zap.String("task_id", taskID),
			zap.String("draft_url", draftURL),
			zap.Error(err),
		)
		return "", fmt.Sprintf("草稿已生成，视频生成失败: %v", err)
	}
	if err := w.taskRepo.UpdateVideoURL(ctx, taskID, videoURL); err != nil {
		w.logger.Warn("回写 task.video_url 失败，任务按部分成功完成",
			zap.String("task_id", taskID),
			zap.String("video_url", videoURL),
			zap.Error(err),
		)
		return "", fmt.Sprintf("草稿已生成，回写 video_url 失败: %v", err)
	}
	setProgress(videoPhaseLocalStart + videoPhaseLocalSpan)
	return videoURL, ""
}

func clampProgress(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
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
