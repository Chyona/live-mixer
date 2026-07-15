package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	// draftDefaultConcurrency 单实例内并行抢占/执行草稿任务的 Worker 数。
	draftDefaultConcurrency = 1
	// draftPollInterval 无待处理任务时的 DB 轮询间隔（多实例兜底唤醒）。
	draftPollInterval = 3 * time.Second
	// draftDefaultCanvasWidth / draftDefaultCanvasHeight 剪映草稿默认画布尺寸。
	draftDefaultCanvasWidth  = 1080
	draftDefaultCanvasHeight = 1920
)

// DraftWorker 剪映草稿任务后台调度器：DB 原子抢占 + ffmpeg 切片 + capcut-mate 组装草稿。
type DraftWorker interface {
	// Enqueue 唤醒调度循环尝试领取任务（非阻塞）。
	Enqueue()
	// Process 执行已抢占（processing）的草稿任务完整流程。
	Process(ctx context.Context, task *model.Task) error
	// Start 启动后台 Worker 循环。
	Start(ctx context.Context)
}

// CapCutMateAPI 草稿生成所需的 capcut-mate 接口，便于单测替换。
type CapCutMateAPI interface {
	CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error)
	AddVideos(ctx context.Context, req capcutmate.AddVideosRequest, recordDir string) (*capcutmate.AddVideosResponse, error)
}

// VideoSegmentCutter 视频精确裁剪抽象，便于单测替换。
type VideoSegmentCutter interface {
	CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error
}

type draftWorker struct {
	taskRepo         repository.TaskRepository
	liveMaterialRepo repository.LiveMaterialRepository
	videoProjectRepo repository.VideoProjectRepository
	capcut           CapCutMateAPI
	cutter           VideoSegmentCutter
	downloader       FileDownloader
	web              webroot.Config
	logger           *zap.Logger
	concurrency      int
	pollInterval     time.Duration

	wake      chan struct{}
	startOnce sync.Once
}

// DraftWorkerDeps 构造草稿 Worker 所需依赖，避免过长参数列表。
type DraftWorkerDeps struct {
	TaskRepo         repository.TaskRepository
	LiveMaterialRepo repository.LiveMaterialRepository
	VideoProjectRepo repository.VideoProjectRepository
	CapCut           CapCutMateAPI
	Cutter           VideoSegmentCutter
	Downloader       FileDownloader
	Web              webroot.Config
	Logger           *zap.Logger
}

// NewDraftWorker 创建剪映草稿后台 Worker。
func NewDraftWorker(deps DraftWorkerDeps) DraftWorker {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	cutter := deps.Cutter
	if cutter == nil {
		cutter = media.NewFFmpegConverter("")
	}
	downloader := deps.Downloader
	if downloader == nil {
		downloader = newLoggingResumableDownloader(logger)
	}
	return &draftWorker{
		taskRepo:         deps.TaskRepo,
		liveMaterialRepo: deps.LiveMaterialRepo,
		videoProjectRepo: deps.VideoProjectRepo,
		capcut:           deps.CapCut,
		cutter:           cutter,
		downloader:       downloader,
		web:              deps.Web,
		logger:           logger,
		concurrency:      draftDefaultConcurrency,
		pollInterval:     draftPollInterval,
		wake:             make(chan struct{}, 1),
	}
}

// Enqueue 非阻塞唤醒调度器，Create 任务后调用以尽快领取。
func (w *draftWorker) Enqueue() {
	select {
	case w.wake <- struct{}{}:
	default:
		// 已有唤醒信号在队列中，无需重复投递。
	}
}

// Start 启动若干并发领取循环 + 定时 poll，保证重启后 pending 任务仍会被处理。
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
		w.logger.Info("剪映草稿 Worker 已启动", zap.Int("concurrency", n))
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
			w.Enqueue()
		}
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

// drain 循环抢占并处理，直到当前没有更多 pending draft 任务。
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
			zap.Uint("task_id", task.ID),
		)
		if err := w.Process(ctx, task); err != nil {
			w.logger.Error("剪映草稿任务执行失败",
				zap.Uint("task_id", task.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 执行单条草稿任务：
// 1) 读 video_project.clips1 与 live_material.live_url；
// 2) 下载直播视频到 staging/{task_id}；
// 3) ffmpeg 精确裁剪各片段；
// 4) 调用 capcut-mate create_draft + add_videos；
// 5) 回写 video_project.draft_url 与任务状态/进度。
func (w *draftWorker) Process(ctx context.Context, task *model.Task) error {
	if task == nil {
		return fmt.Errorf("task 不能为空")
	}
	progress := int16(5)
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	ext, err := parseTaskExt(task.Ext)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("解析任务 ext 失败: %w", err))
	}
	if ext.VideoProjectID == 0 {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("任务缺少 video_project_id"))
	}

	project, err := w.videoProjectRepo.GetByID(ctx, ext.VideoProjectID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询剪辑项目失败: %w", err))
	}

	clips, err := resolveDraftClipRanges(project)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}
	if err := validateClipRanges(clips); err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}

	liveID := ext.LiveID
	if liveID == 0 {
		liveID = project.LiveID
	}
	material, err := w.liveMaterialRepo.GetByID(ctx, liveID)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("查询直播素材失败: %w", err))
	}
	if material.LiveURL == "" {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("直播素材 live_url 为空"))
	}

	stagingDir := w.web.StagingDir(task.ID)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("创建任务暂存目录失败: %w", err))
	}
	recordDir := w.web.CapCutMateRecordDir(task.ID)
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("创建 capcut-mate 录制目录失败: %w", err))
	}

	w.logger.Info("开始下载直播视频",
		zap.Uint("task_id", task.ID),
		zap.String("live_url", material.LiveURL),
		zap.String("staging_dir", stagingDir),
	)
	progress = 15
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	sourcePath := filepath.Join(stagingDir, "source.mp4")
	if _, err := w.downloader.Download(material.LiveURL, sourcePath); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("下载直播视频失败: %w", err))
	}

	progress = 25
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	clipPaths, err := w.cutClips(ctx, task.ID, sourcePath, stagingDir, clips)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}

	progress = 50
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	width := ext.CanvasWidth
	height := ext.CanvasHeight
	if width <= 0 {
		width = draftDefaultCanvasWidth
	}
	if height <= 0 {
		height = draftDefaultCanvasHeight
	}

	if w.capcut == nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("capcut-mate 客户端未配置"))
	}

	w.logger.Info("调用 capcut-mate 创建草稿",
		zap.Uint("task_id", task.ID),
		zap.Int("width", width),
		zap.Int("height", height),
	)
	createResp, err := w.capcut.CreateDraft(ctx, width, height, recordDir)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("创建剪映草稿失败: %w", err))
	}

	progress = 70
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	videoInfos, err := w.buildVideoInfos(clipPaths, clips)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}
	videoInfosJSON, err := capcutmate.BuildVideoInfosJSON(videoInfos)
	if err != nil {
		return w.fail(ctx, task.ID, progress, err)
	}

	w.logger.Info("调用 capcut-mate 批量添加视频",
		zap.Uint("task_id", task.ID),
		zap.Int("clips", len(videoInfos)),
		zap.String("draft_url", createResp.DraftURL),
	)
	addResp, err := w.capcut.AddVideos(ctx, capcutmate.AddVideosRequest{
		Alpha:          1,
		DraftURL:       createResp.DraftURL,
		ScaleX:         1,
		ScaleY:         1,
		SceneTimelines: []any{},
		VideoInfos:     videoInfosJSON,
	}, recordDir)
	if err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("向草稿添加视频失败: %w", err))
	}

	draftURL := addResp.DraftURL
	if draftURL == "" {
		draftURL = createResp.DraftURL
	}

	progress = 90
	_ = w.taskRepo.UpdateProgress(ctx, task.ID, progress)

	project.DraftURL = draftURL
	if err := w.videoProjectRepo.Update(ctx, project); err != nil {
		return w.fail(ctx, task.ID, progress, fmt.Errorf("回写 video_project.draft_url 失败: %w", err))
	}

	ext.LiveID = liveID
	ext.VideoProjectID = project.ID
	ext.CanvasWidth = width
	ext.CanvasHeight = height
	extRaw, err := marshalTaskExt(ext)
	if err != nil {
		// 草稿已生成，仍尽量标记完成并回写精简 ext。
		extRaw = fmt.Sprintf(`{"live_id":%d,"video_project_id":%d,"canvas_width":%d,"canvas_height":%d}`,
			liveID, project.ID, width, height)
	}

	if err := w.taskRepo.MarkCompleted(ctx, task.ID, 100, extRaw); err != nil {
		return fmt.Errorf("标记任务完成失败: %w", err)
	}
	w.logger.Info("剪映草稿任务完成",
		zap.Uint("task_id", task.ID),
		zap.Uint("video_project_id", project.ID),
		zap.String("draft_url", draftURL),
		zap.Int("clips", len(clips)),
	)
	return nil
}

// cutClips 按 clips 时间段（毫秒）裁剪出本地切片文件。
func (w *draftWorker) cutClips(ctx context.Context, taskID uint, sourcePath, stagingDir string, clips []model.ClipRange) ([]string, error) {
	paths := make([]string, 0, len(clips))
	for i, clip := range clips {
		outPath := filepath.Join(stagingDir, fmt.Sprintf("clip_%03d.mp4", i))
		startSec := float64(clip.StartTime) / 1000.0
		endSec := float64(clip.EndTime) / 1000.0
		w.logger.Info("开始 ffmpeg 裁剪切片",
			zap.Uint("task_id", taskID),
			zap.Int("index", i),
			zap.Int64("start_ms", clip.StartTime),
			zap.Int64("end_ms", clip.EndTime),
			zap.String("output", outPath),
		)
		if err := w.cutter.CutVideoSegment(ctx, sourcePath, outPath, startSec, endSec); err != nil {
			return nil, fmt.Errorf("裁剪第 %d 段失败: %w", i, err)
		}
		paths = append(paths, outPath)

		// 裁剪阶段进度：25 → 50，按片段数线性推进。
		if len(clips) > 0 {
			p := int16(25 + int(25*float64(i+1)/float64(len(clips))))
			_ = w.taskRepo.UpdateProgress(ctx, taskID, p)
		}
	}
	return paths, nil
}

// buildVideoInfos 将本地切片转为时间轴 video_info（URL + 微秒起止）。
func (w *draftWorker) buildVideoInfos(clipPaths []string, clips []model.ClipRange) ([]capcutmate.VideoInfo, error) {
	infos := make([]capcutmate.VideoInfo, 0, len(clipPaths))
	var cursorUS int64
	for i, localPath := range clipPaths {
		videoURL, err := w.web.LocalPathToURL(localPath)
		if err != nil {
			return nil, fmt.Errorf("切片路径转 URL 失败: %w", err)
		}
		durMS := clips[i].EndTime - clips[i].StartTime
		if durMS <= 0 {
			return nil, fmt.Errorf("第 %d 段时长无效", i)
		}
		durUS := durMS * 1000 // 毫秒 → 微秒
		infos = append(infos, capcutmate.VideoInfo{
			VideoURL: videoURL,
			Start:    cursorUS,
			End:      cursorUS + durUS,
			Volume:   1,
		})
		cursorUS += durUS
	}
	return infos, nil
}

func (w *draftWorker) fail(ctx context.Context, taskID uint, progress int16, err error) error {
	w.logger.Error("剪映草稿任务失败",
		zap.Uint("task_id", taskID),
		zap.Int16("progress", progress),
		zap.Error(err),
	)
	if failErr := w.taskRepo.MarkFailed(ctx, taskID, progress, err.Error()); failErr != nil {
		return fmt.Errorf("剪映草稿失败且写入失败状态异常: task=%w, db=%v", err, failErr)
	}
	return err
}

// resolveDraftClipRanges 优先从 video_project.clips1 提取时间段；为空则回退 clips0。
func resolveDraftClipRanges(project *model.VideoProject) ([]model.ClipRange, error) {
	if project == nil {
		return nil, fmt.Errorf("video_project 不能为空")
	}
	if clips, err := parseClips1Ranges(project.Clips1); err == nil && len(clips) > 0 {
		return clips, nil
	}
	var clips0 []model.ClipRange
	if err := json.Unmarshal([]byte(emptyJSONArrayIfBlank(project.Clips0)), &clips0); err != nil {
		return nil, fmt.Errorf("video_project.clips0 格式无效: %w", err)
	}
	if len(clips0) == 0 {
		return nil, fmt.Errorf("video_project.clips1/clips0 均为空，无法生成草稿")
	}
	return clips0, nil
}

// parseClips1Ranges 解析 clips1 JSON，提取各片段的 start_time/end_time（毫秒）。
func parseClips1Ranges(raw string) ([]model.ClipRange, error) {
	var clips []clipWithWords
	if err := json.Unmarshal([]byte(emptyJSONArrayIfBlank(raw)), &clips); err != nil {
		return nil, err
	}
	out := make([]model.ClipRange, 0, len(clips))
	for _, c := range clips {
		out = append(out, model.ClipRange{StartTime: c.StartTime, EndTime: c.EndTime})
	}
	return out, nil
}

func emptyJSONArrayIfBlank(raw string) string {
	if raw == "" {
		return "[]"
	}
	return raw
}
