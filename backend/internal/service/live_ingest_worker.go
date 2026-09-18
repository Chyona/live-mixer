package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	liveIngestDefaultConcurrency = 6
	liveIngestPollInterval       = 3 * time.Second
	liveIngestHeartbeat          = 20 * time.Second
	liveIngestProbeInterval      = 12 * time.Second
)

// liveRecordPipeline 录像链路日志统一 pipeline 字段，便于 grep「live_record」。
const liveRecordPipeline = "live_record"

func liveRecordFields(stage string, materialID uint, extra ...zap.Field) []zap.Field {
	fields := []zap.Field{
		zap.String("pipeline", liveRecordPipeline),
		zap.String("stage", stage),
		zap.Uint("material_id", materialID),
	}
	return append(fields, extra...)
}

// LiveIngestWorker 跟播门面：内部拆录像 / 异步 master / 窗口 ASR / Finalize 流水线。
type LiveIngestWorker interface {
	Enqueue()
	Start(ctx context.Context)
	// Cancel 取消指定素材的进行中跟播（杀 ffmpeg / 打断探测），释放并发槽。
	// 若该素材当前未在本进程执行，返回 false。
	Cancel(materialID uint) bool
}

type liveIngestWorker struct {
	repo          repository.LiveIngestRepository
	asrService    ASRService
	audioPreparer LiveMaterialASRAudioPreparer
	llmClient     LLMChatClient
	storage       *storage.Client
	ffmpeg        *media.FFmpegConverter
	prober        media.MediaTimelineProber
	httpClient    *http.Client
	web           webroot.Config
	logger        *zap.Logger
	concurrency   int

	recorderWake chan struct{}
	asrWake      chan struct{}
	finalizeWake chan struct{}
	startOnce    sync.Once

	cancelsMu       sync.Mutex
	recorderCancels map[uint]*ingestCancelEntry
	asrCancels      map[uint]*ingestCancelEntry
	finalizeCancels map[uint]*ingestCancelEntry

	// 进程内按素材串行的异步 master 队列（上传窗 + append/全量拼 master）。
	masterMu     sync.Mutex
	masterQueues map[uint]*materialMasterQueue
}

type storageLiveURLAllocator struct {
	client *storage.Client
}

func (a storageLiveURLAllocator) PublicLiveURL(recordUUID string) string {
	if a.client == nil {
		return staticLiveURLAllocator{}.PublicLiveURL(recordUUID)
	}
	if u := a.client.PublicURL(liveingest.FinalObjectKey(recordUUID)); u != "" {
		return u
	}
	return staticLiveURLAllocator{}.PublicLiveURL(recordUUID)
}

// NewStorageLiveURLAllocator 按对象存储预分配不可变 live_url。
func NewStorageLiveURLAllocator(client *storage.Client) LiveRecordURLAllocator {
	return storageLiveURLAllocator{client: client}
}

// NewLiveIngestWorker 创建跟播 Worker。
func NewLiveIngestWorker(
	repo repository.LiveIngestRepository,
	asrService ASRService,
	audioPreparer LiveMaterialASRAudioPreparer,
	llmClient LLMChatClient,
	storageClient *storage.Client,
	web webroot.Config,
	logger *zap.Logger,
	concurrency int,
) LiveIngestWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if concurrency <= 0 {
		concurrency = liveIngestDefaultConcurrency
	}
	return &liveIngestWorker{
		repo:            repo,
		asrService:      asrService,
		audioPreparer:   audioPreparer,
		llmClient:       llmClient,
		storage:         storageClient,
		ffmpeg:          media.NewFFmpegConverter(""),
		prober:          media.NewFFprobeProber(""),
		httpClient:      &http.Client{Timeout: 20 * time.Second},
		web:             web,
		logger:          logger,
		concurrency:     concurrency,
		recorderWake:    newWakeChan(concurrency),
		asrWake:         newWakeChan(concurrency),
		finalizeWake:    newWakeChan(concurrency),
		recorderCancels: make(map[uint]*ingestCancelEntry),
		asrCancels:      make(map[uint]*ingestCancelEntry),
		finalizeCancels: make(map[uint]*ingestCancelEntry),
		masterQueues:    make(map[uint]*materialMasterQueue),
	}
}

func (w *liveIngestWorker) Enqueue() {
	enqueueWake(w.recorderWake, w.concurrency)
	enqueueWake(w.asrWake, w.concurrency)
	enqueueWake(w.finalizeWake, w.concurrency)
}

func (w *liveIngestWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		for i := 0; i < w.concurrency; i++ {
			go w.recorderLoop(ctx, i)
			go w.asrLoop(ctx, i)
			go w.finalizeLoop(ctx, i)
		}
		go w.pollLoop(ctx)
		w.Enqueue()
		w.logger.Info("直播跟播四流水线已启动",
			zap.Int("concurrency", w.concurrency),
			zap.Strings("pipelines", []string{"recorder", "async_master", "window_asr", "finalize"}),
		)
	})
}

func (w *liveIngestWorker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(liveIngestPollInterval)
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

func (w *liveIngestWorker) recorderLoop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.recorderWake:
			w.drainRecorder(ctx, workerID)
		}
	}
}

func (w *liveIngestWorker) asrLoop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.asrWake:
			w.drainASR(ctx, workerID)
		}
	}
}

func (w *liveIngestWorker) finalizeLoop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.finalizeWake:
			w.drainFinalize(ctx, workerID)
		}
	}
}

func (w *liveIngestWorker) drainRecorder(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		material, err := w.repo.ClaimRecorderWork(ctx)
		if err != nil {
			w.logger.Error("抢占录像任务失败", zap.Int("worker_id", workerID), zap.Error(err))
			return
		}
		if material == nil {
			return
		}
		w.logger.Info("已抢占录像任务",
			liveRecordFields("claim", material.ID,
				zap.Int("worker_id", workerID),
				zap.String("live_status", material.LiveStatus),
				zap.String("source_mode", material.SourceMode),
				zap.Int64("ingest_epoch", material.IngestEpoch),
				zap.Int64("next_window_seg", material.NextWindowSeg),
				zap.Int64("duration_ms", material.Duration),
				zap.String("m3u8_url", material.M3U8URL),
			)...,
		)
		if err := w.Process(ctx, material); err != nil {
			if errors.Is(err, context.Canceled) {
				w.logger.Info("录像任务已取消",
					liveRecordFields("cancel", material.ID)...,
				)
			} else {
				w.logger.Warn("录像任务异常结束",
					liveRecordFields("fail", material.ID,
						zap.String("live_status", material.LiveStatus),
						zap.Error(err),
					)...,
				)
			}
		} else {
			w.logger.Info("录像任务正常结束",
				liveRecordFields("done", material.ID,
					zap.String("live_status", material.LiveStatus),
					zap.Int64("next_window_seg", material.NextWindowSeg),
					zap.Int64("duration_ms", material.Duration),
				)...,
			)
		}
	}
}

func (w *liveIngestWorker) drainASR(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		material, err := w.repo.ClaimWindowASRWork(ctx)
		if err != nil {
			w.logger.Error("抢占窗口 ASR 失败", zap.Int("worker_id", workerID), zap.Error(err))
			return
		}
		if material == nil {
			return
		}
		w.logger.Info("已抢占窗口 ASR",
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_epoch", material.ASREpoch),
			zap.Int64("asr_cursor_ms", material.ASRCursorMS),
			zap.Int64("duration_ms", material.Duration),
			zap.Bool("asr_due", material.ASRDue),
		)
		if err := w.processWindowASR(ctx, material); err != nil {
			if errors.Is(err, context.Canceled) {
				w.logger.Info("窗口 ASR 已取消", zap.Uint("material_id", material.ID))
			} else {
				w.logger.Warn("窗口 ASR 结束", zap.Uint("material_id", material.ID), zap.Error(err))
			}
		}
	}
}

func (w *liveIngestWorker) drainFinalize(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		material, err := w.repo.ClaimFinalizeWork(ctx)
		if err != nil {
			w.logger.Error("抢占 Finalize 失败", zap.Int("worker_id", workerID), zap.Error(err))
			return
		}
		if material == nil {
			return
		}
		w.logger.Info("已抢占 Finalize",
			zap.Uint("material_id", material.ID),
			zap.String("live_status", material.LiveStatus),
			zap.Int64("ingest_epoch", material.IngestEpoch),
		)
		if err := w.processFinalize(ctx, material); err != nil {
			if errors.Is(err, context.Canceled) {
				w.logger.Info("Finalize 已取消", zap.Uint("material_id", material.ID))
			} else {
				w.logger.Warn("Finalize 结束", zap.Uint("material_id", material.ID), zap.Error(err))
			}
		}
	}
}

// Process 执行录像流水线（探测 + ffmpeg）；关播后交给 Finalize。
func (w *liveIngestWorker) Process(ctx context.Context, material *model.LiveMaterial) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	unbind := w.bindRecorderCancel(material.ID, cancel)
	defer unbind()

	stopHB := w.startHeartbeat(ctx, material.ID, material.IngestEpoch)
	defer stopHB()

	w.logger.Info("开始处理录像任务",
		liveRecordFields("process_start", material.ID,
			zap.String("live_status", material.LiveStatus),
			zap.String("source_mode", material.SourceMode),
			zap.String("asr_status", material.ASRStatus),
			zap.Int16("asr_progress", material.ASRProgress),
			zap.Int64("asr_cursor_ms", material.ASRCursorMS),
			zap.Int64("next_seg", material.NextSeg),
			zap.Int64("next_window_seg", material.NextWindowSeg),
			zap.Int64("duration_ms", material.Duration),
			zap.Int64("ingest_epoch", material.IngestEpoch),
			zap.String("m3u8_url", material.M3U8URL),
		)...,
	)

	switch material.LiveStatus {
	case model.LiveStatusWaiting, model.LiveStatusConnecting:
		w.logger.Info("跟播进入等待媒体阶段",
			liveRecordFields("wait_media", material.ID)...,
		)
		if err := w.waitForMedia(ctx, material); err != nil {
			return err
		}
		w.logger.Info("已检测到直播媒体，进入录像",
			liveRecordFields("enter_record", material.ID)...,
		)
		return w.recordOnly(ctx, material)
	case model.LiveStatusLive:
		w.logger.Info("跟播已是 live，直接进入录像",
			liveRecordFields("enter_record_resume", material.ID,
				zap.Int64("next_window_seg", material.NextWindowSeg),
			)...,
		)
		return w.recordOnly(ctx, material)
	default:
		w.logger.Info("录像流水线跳过非录像状态",
			liveRecordFields("skip_non_record", material.ID,
				zap.String("live_status", material.LiveStatus),
			)...,
		)
		return nil
	}
}

func (w *liveIngestWorker) processWindowASR(ctx context.Context, material *model.LiveMaterial) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	unbind := w.bindASRCancel(material.ID, cancel)
	defer unbind()

	stopHB := w.startASRHeartbeat(ctx, material.ID, material.ASREpoch)
	defer stopHB()
	defer func() {
		_ = w.repo.ReleaseASRLease(context.Background(), material.ID, material.ASREpoch)
	}()

	_ = w.repo.MarkASRProcessing(ctx, material.ID, material.ASREpoch)

	if latest, gerr := w.repo.GetByID(ctx, material.ID); gerr == nil && latest != nil {
		material.LiveStatus = latest.LiveStatus
		material.MediaWindows = latest.MediaWindows
		material.Duration = latest.Duration
		material.ASRCursorMS = latest.ASRCursorMS
		material.LiveASR = latest.LiveASR
		material.ASRDue = latest.ASRDue
	}
	if w.shouldDeferLiveASR(material) {
		w.logger.Info("直播中推迟全量 ASR（master 落后或队列积压）",
			zap.Uint("material_id", material.ID),
			zap.Int64("duration_ms", material.Duration),
			zap.Int64("sealed_ms", material.ParsedMediaWindows().TotalReadyMS()),
			zap.Int("master_pending", w.masterQueuePending(material.ID)),
		)
		// 清 due 避免空转抢占；后续 master 节流点或关播再置 asr_due。
		_ = w.repo.ClearASRDue(ctx, material.ID, material.ASREpoch)
		return nil
	}

	// 单次租约内对当前 master 全量 ASR；master 若再变长则同租约内继续全量直到追上。
	w.catchUpWindowASR(ctx, material)
	if latest, gerr := w.repo.GetByID(ctx, material.ID); gerr == nil && latest != nil {
		if !latest.ParsedMediaWindows().ASRPending(latest.ASRCursorMS) {
			_ = w.repo.ClearASRDue(ctx, material.ID, material.ASREpoch)
		}
	}
	return nil
}

func (w *liveIngestWorker) processFinalize(ctx context.Context, material *model.LiveMaterial) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	unbind := w.bindFinalizeCancel(material.ID, cancel)
	defer unbind()

	stopHB := w.startHeartbeat(ctx, material.ID, material.IngestEpoch)
	defer stopHB()

	switch material.LiveStatus {
	case model.LiveStatusEnding:
		resumeFrom := material.IngestResumeSeg
		return w.finalizeRecording(ctx, material, resumeFrom)
	case model.LiveStatusEnded:
		if material.ASRStatus == model.ASRStatusCompleted {
			w.logger.Info("关播 ASR 已完成，跳过", zap.Uint("material_id", material.ID))
			return nil
		}
		w.logger.Info("关播后补完 ASR 收尾",
			zap.Uint("material_id", material.ID),
			zap.String("asr_status", material.ASRStatus),
			zap.Int16("asr_progress", material.ASRProgress),
		)
		w.catchUpWindowASRUnderLease(ctx, material)
		return w.finishASRPostprocess(ctx, material, material.Duration)
	default:
		return nil
	}
}

func (w *liveIngestWorker) waitForMedia(ctx context.Context, material *model.LiveMaterial) error {
	deadline := time.Now().Add(model.LiveConnectGrace)
	if material.LiveStatus == model.LiveStatusWaiting && material.WaitDeadlineAt != nil {
		deadline = *material.WaitDeadlineAt
	}
	if material.LiveStatus == model.LiveStatusConnecting && material.ConnectDeadlineAt != nil {
		deadline = *material.ConnectDeadlineAt
	}
	if material.LiveStatus == model.LiveStatusWaiting && material.ScheduledAt != nil {
		startAt := material.ScheduledAt.Add(-model.LiveEarlyProbe)
		if wait := time.Until(startAt); wait > 0 {
			w.logger.Info("未到提前探测时间，等待中",
				liveRecordFields("wait_early_probe", material.ID,
					zap.Time("scheduled_at", *material.ScheduledAt),
					zap.Time("probe_start_at", startAt),
					zap.Duration("sleep", wait),
				)...,
			)
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}

	if err := w.repo.MarkConnecting(ctx, material.ID, material.IngestEpoch); err != nil {
		w.logger.Warn("标记 connecting 失败，仍继续探测",
			liveRecordFields("mark_connecting", material.ID, zap.Error(err))...,
		)
	} else {
		w.logger.Info("已标记 connecting，开始轮询直播播放列表",
			liveRecordFields("mark_connecting", material.ID,
				zap.Time("deadline", deadline),
				zap.String("m3u8_url", material.M3U8URL),
			)...,
		)
	}
	material.LiveStatus = model.LiveStatusConnecting

	ticker := time.NewTicker(liveIngestProbeInterval)
	defer ticker.Stop()
	attempt := 0
	for {
		if time.Now().After(deadline) {
			msg := "添加后 2 小时内未检测到直播"
			if material.ScheduledAt != nil {
				msg = "开播后 2 小时内未检测到直播"
			}
			w.markIngestFailedLogged(ctx, material, "wait_media_timeout", msg)
			return fmt.Errorf("%s", msg)
		}
		attempt++
		probe, err := media.ProbeHLSPlaylist(w.httpClient, material.M3U8URL)
		if probe.Encrypted {
			msg := media.SanitizeHLSError(err)
			if msg == "" {
				msg = "不支持加密 HLS"
			}
			w.markIngestFailedLogged(ctx, material, "wait_media_encrypted", msg)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s", msg)
		}
		if probe.HasMedia {
			w.logger.Info("HTTP 探测到直播播放列表含媒体分片",
				liveRecordFields("probe_hls_ok", material.ID,
					zap.Int("attempt", attempt),
					zap.String("m3u8_url", material.M3U8URL),
				)...,
			)
			return nil
		}
		if attempt == 1 || attempt%5 == 0 {
			w.logger.Info("尚未检测到直播媒体分片，继续等待",
				liveRecordFields("probe_hls_empty", material.ID,
					zap.Int("attempt", attempt),
					zap.Time("deadline", deadline),
					zap.Duration("remain", time.Until(deadline)),
					zap.Error(err),
				)...,
			)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// markIngestFailedLogged 写失败状态并打 Warn，避免静默 MarkIngestFailed。
func (w *liveIngestWorker) markIngestFailedLogged(ctx context.Context, material *model.LiveMaterial, stage, msg string) {
	if material == nil {
		return
	}
	if err := w.repo.MarkIngestFailed(ctx, material.ID, material.IngestEpoch, msg); err != nil {
		w.logger.Error("标记跟播失败写库失败",
			liveRecordFields(stage, material.ID,
				zap.String("fail_msg", msg),
				zap.Error(err),
			)...,
		)
		return
	}
	w.logger.Warn("已标记跟播失败",
		liveRecordFields(stage, material.ID, zap.String("fail_msg", msg))...,
	)
}

func (w *liveIngestWorker) recordOnly(ctx context.Context, material *model.LiveMaterial) error {
	width, height := material.Width, material.Height
	if w.prober != nil {
		probeStart := time.Now()
		w.logger.Info("开始 ffprobe 探测直播流（若长时间无后续日志，多半卡在此处）",
			liveRecordFields("ffprobe_start", material.ID,
				zap.String("m3u8_url", material.M3U8URL),
			)...,
		)
		tl, err := w.prober.ProbeMediaTimeline(ctx, material.M3U8URL)
		if err != nil {
			w.logger.Warn("ffprobe 探测直播流失败，将沿用已有宽高继续录像",
				liveRecordFields("ffprobe_fail", material.ID,
					zap.Duration("elapsed", time.Since(probeStart)),
					zap.Error(err),
				)...,
			)
		} else {
			width, height = tl.Width, tl.Height
			w.logger.Info("ffprobe 探测直播流完成",
				liveRecordFields("ffprobe_ok", material.ID,
					zap.Duration("elapsed", time.Since(probeStart)),
					zap.Int("width", width),
					zap.Int("height", height),
				)...,
			)
		}
	} else {
		w.logger.Info("未配置 prober，跳过直播流探测",
			liveRecordFields("ffprobe_skip", material.ID)...,
		)
	}

	winIdx := nextWindowIndex(material)
	resumeFrom := int64(winIdx)
	if err := w.repo.MarkLiveStarted(ctx, material.ID, material.IngestEpoch, width, height, resumeFrom); err != nil {
		w.logger.Warn("标记 live 失败（页面可能仍显示等待中，但本进程将继续录像）",
			liveRecordFields("mark_live_fail", material.ID, zap.Error(err))...,
		)
	} else {
		w.logger.Info("已标记 live，进入按窗录像循环",
			liveRecordFields("mark_live_ok", material.ID,
				zap.Int64("ingest_epoch", material.IngestEpoch),
				zap.Int("resume_window", winIdx),
				zap.Int("width", width),
				zap.Int("height", height),
			)...,
		)
	}
	material.LiveStatus = model.LiveStatusLive
	material.Width, material.Height = width, height
	material.IngestResumeSeg = resumeFrom
	material.NextSeg = resumeFrom
	material.NextWindowSeg = resumeFrom

	workDir := w.segmentDir(material)
	// 全新录像：清空同 epoch 工作目录，避免脏残留。
	if winIdx == 0 {
		if err := resetLiveIngestWorkDir(workDir); err != nil {
			w.logger.Warn("清空跟播工作目录失败，继续尝试录像",
				liveRecordFields("workdir_reset_fail", material.ID,
					zap.String("work_dir", workDir),
					zap.Error(err),
				)...,
			)
		} else {
			w.logger.Info("已清空跟播工作目录（全新录像）",
				liveRecordFields("workdir_reset_ok", material.ID,
					zap.String("work_dir", workDir),
				)...,
			)
		}
	}
	winDir := filepath.Join(workDir, liveingest.WindowsDirName())
	if err := os.MkdirAll(winDir, 0o755); err != nil {
		w.logger.Error("创建媒体窗目录失败",
			liveRecordFields("workdir_mkdir_fail", material.ID,
				zap.String("win_dir", winDir),
				zap.Error(err),
			)...,
		)
		return err
	}
	// 续录：把已 seal 但尚未进入 master 的本地窗重新投递异步拼接。
	if winIdx > 0 {
		w.logger.Info("续录：检查并补投递未拼 master 的媒体窗",
			liveRecordFields("requeue_master", material.ID,
				zap.Int("resume_window", winIdx),
				zap.Int64("duration_ms", material.Duration),
				zap.Int("ready_windows", material.ParsedMediaWindows().ReadyCount()),
			)...,
		)
		w.requeuePendingMasterBuilds(material)
	}

	windowMS := material.EffectiveMediaWindowMS()
	windowSec := int(windowMS / 1000)
	if windowSec <= 0 {
		windowSec = int(model.LiveMediaWindowDuration / time.Second)
	}
	// 满窗判定：达到目标时长的 90% 视为完整窗，继续下一窗；否则当作断流尾巴。
	fullWindowMS := windowMS * 9 / 10
	if fullWindowMS < minRecordedWindowMS {
		fullWindowMS = minRecordedWindowMS
	}

	gotAnyWindow := material.ParsedMediaWindows().ReadyCount() > 0
	emptyAttempts := 0
	const maxEmptyAttempts = 8
	const maxAccessFailStreak = 2

	sawRealtimeWindow := gotAnyWindow // 续录已有窗时启用回放关播，避免重启后再次胀库
	lastFingerprint := ""
	accessFailStreak := 0
	if gotAnyWindow {
		if prev := nextWindowIndex(material) - 1; prev >= 0 {
			prevPath := filepath.Join(winDir, liveingest.WindowMP4FileName(prev))
			if fp, err := windowFileFingerprint(prevPath); err == nil {
				lastFingerprint = fp
			}
		}
	}

	w.logger.Info("按窗录像循环开始",
		liveRecordFields("window_loop_start", material.ID,
			zap.Int("start_window", winIdx),
			zap.Int("window_sec", windowSec),
			zap.Int64("full_window_ms", fullWindowMS),
			zap.Bool("has_prior_windows", gotAnyWindow),
		)...,
	)

	for {
		if err := ctx.Err(); err != nil {
			w.logger.Info("按窗录像循环被取消",
				liveRecordFields("window_loop_cancel", material.ID,
					zap.Int("window_index", nextWindowIndex(material)),
					zap.Error(err),
				)...,
			)
			return err
		}
		winIdx = nextWindowIndex(material)
		mp4Path := filepath.Join(winDir, liveingest.WindowMP4FileName(winIdx))
		_ = os.Remove(mp4Path)

		// 窗前探测：地址不可访问 → 已有窗则关播，否则沿用开播重试。
		preProbe, preErr := media.ProbeHLSPlaylist(w.httpClient, material.M3U8URL)
		hasEndList := preErr == nil && preProbe.HasEndList
		if preErr != nil {
			accessFailStreak++
			w.logger.Warn("窗前 HLS 探测失败",
				liveRecordFields("pre_probe_fail", material.ID,
					zap.Int("window_index", winIdx),
					zap.Int("access_fail_streak", accessFailStreak),
					zap.Error(preErr),
				)...,
			)
			if gotAnyWindow && accessFailStreak >= maxAccessFailStreak {
				w.logger.Info("直播源不可访问，结束录像",
					liveRecordFields("end_source_unreachable", material.ID,
						zap.Int("access_fail_streak", accessFailStreak),
						zap.Error(preErr),
					)...,
				)
				break
			}
			if !gotAnyWindow {
				emptyAttempts++
				deadline := time.Now().Add(model.LiveConnectGrace)
				if material.ConnectDeadlineAt != nil {
					deadline = *material.ConnectDeadlineAt
				}
				if material.WaitDeadlineAt != nil {
					deadline = *material.WaitDeadlineAt
				}
				if emptyAttempts >= maxEmptyAttempts || !time.Now().Before(deadline) {
					msg := "未能录制到直播媒体窗"
					if preErr != nil {
						msg = fmt.Sprintf("%s: %v", msg, preErr)
					}
					w.markIngestFailedLogged(ctx, material, "no_window_pre_probe", msg)
					return fmt.Errorf("%s", msg)
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(liveIngestProbeInterval):
			}
			continue
		}
		if accessFailStreak > 0 {
			w.logger.Info("窗前 HLS 探测恢复",
				liveRecordFields("pre_probe_recovered", material.ID,
					zap.Int("window_index", winIdx),
					zap.Int("prev_fail_streak", accessFailStreak),
					zap.Bool("playlist_endlist", hasEndList),
				)...,
			)
		}
		accessFailStreak = 0

		w.logger.Info("开始 ffmpeg 按窗录像",
			liveRecordFields("ffmpeg_window_start", material.ID,
				zap.Int("window_index", winIdx),
				zap.Int("duration_sec", windowSec),
				zap.Bool("playlist_endlist", hasEndList),
				zap.String("output", mp4Path),
				zap.String("m3u8_url", material.M3U8URL),
			)...,
		)
		recStart := time.Now()
		onProgress := func() {
			// 不传已封窗总长：录像协程的 Duration 常为 0，MasterReadyMS 会返回尚未拼进主片的窗长。
			if err := w.repo.UpdateRecordingProgress(ctx, material.ID, material.IngestEpoch, int64(winIdx), 0, ""); err != nil {
				w.logger.Warn("更新录像进度失败",
					liveRecordFields("progress_update_fail", material.ID,
						zap.Int("window_index", winIdx),
						zap.Error(err),
					)...,
				)
			}
		}
		recErr := w.ffmpeg.RecordHLSWindowMP4(ctx, material.M3U8URL, mp4Path, windowSec, onProgress)
		elapsed := time.Since(recStart)
		if ctx.Err() != nil {
			w.logger.Info("ffmpeg 按窗录像被取消",
				liveRecordFields("ffmpeg_window_cancel", material.ID,
					zap.Int("window_index", winIdx),
					zap.Duration("elapsed", elapsed),
				)...,
			)
			return ctx.Err()
		}

		durMS := w.probeLocalWindowDurationMS(ctx, mp4Path)
		wallRatio := 0.0
		if windowSec > 0 {
			wallRatio = elapsed.Seconds() / float64(windowSec)
		}
		w.logger.Info("ffmpeg 按窗录像结束",
			liveRecordFields("ffmpeg_window_end", material.ID,
				zap.Int("window_index", winIdx),
				zap.Duration("elapsed", elapsed),
				zap.Float64("wall_ratio", wallRatio),
				zap.Int64("probed_dur_ms", durMS),
				zap.Error(recErr),
			)...,
		)
		if durMS <= 0 {
			w.logger.Warn("本地媒体窗时长探测失败或为空",
				liveRecordFields("window_probe_empty", material.ID,
					zap.Int("window_index", winIdx),
					zap.String("path", mp4Path),
					zap.Error(recErr),
				)...,
			)
		}

		if durMS < minRecordedWindowMS {
			emptyAttempts++
			accessFailStreak++
			_ = os.Remove(mp4Path)
			if gotAnyWindow {
				w.logger.Info("直播源不可访问或无有效输出，结束录像",
					liveRecordFields("end_empty_after_windows", material.ID,
						zap.Int("empty_attempts", emptyAttempts),
						zap.Int("access_fail_streak", accessFailStreak),
						zap.Error(recErr),
					)...,
				)
				break
			}
			deadline := time.Now().Add(model.LiveConnectGrace)
			if material.ConnectDeadlineAt != nil {
				deadline = *material.ConnectDeadlineAt
			}
			if material.WaitDeadlineAt != nil {
				deadline = *material.WaitDeadlineAt
			}
			if emptyAttempts >= maxEmptyAttempts || !time.Now().Before(deadline) {
				msg := "未能录制到直播媒体窗"
				if recErr != nil {
					msg = fmt.Sprintf("%s: %v", msg, recErr)
				}
				w.markIngestFailedLogged(ctx, material, "no_window_empty", msg)
				return fmt.Errorf("%s", msg)
			}
			w.logger.Info("尚未写出有效媒体窗，稍后重试",
				liveRecordFields("window_empty_retry", material.ID,
					zap.Int("empty_attempts", emptyAttempts),
				)...,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(liveIngestProbeInterval):
			}
			continue
		}
		accessFailStreak = 0

		if isRealtimeWindowElapsed(elapsed, windowSec) {
			sawRealtimeWindow = true
		}

		// 窗后补探测 ENDLIST（窗前可能尚无；回放切换常发生在本窗 remux 期间）。
		if postProbe, postErr := media.ProbeHLSPlaylist(w.httpClient, material.M3U8URL); postErr == nil && postProbe.HasEndList {
			if !hasEndList {
				w.logger.Info("窗后探测到 ENDLIST",
					liveRecordFields("post_probe_endlist", material.ID,
						zap.Int("window_index", winIdx),
					)...,
				)
			}
			hasEndList = true
		} else if postErr != nil {
			w.logger.Warn("窗后 HLS 探测失败（不影响本窗提交）",
				liveRecordFields("post_probe_fail", material.ID,
					zap.Int("window_index", winIdx),
					zap.Error(postErr),
				)...,
			)
		}

		fp, fpErr := windowFileFingerprint(mp4Path)
		if fpErr != nil {
			w.logger.Warn("计算媒体窗指纹失败，跳过指纹去重",
				liveRecordFields("fingerprint_fail", material.ID,
					zap.Int("window_index", winIdx),
					zap.Error(fpErr),
				)...,
			)
		}

		replayHit, replayReason := replayEndSignal(sawRealtimeWindow, hasEndList, elapsed, durMS, windowSec, fullWindowMS)
		dupFingerprint := fpErr == nil && lastFingerprint != "" && fp == lastFingerprint
		if dupFingerprint && sawRealtimeWindow {
			replayHit = true
			if replayReason == "" {
				replayReason = "dup_fingerprint"
			} else {
				replayReason = replayReason + "+dup_fingerprint"
			}
		}

		if replayHit {
			if dupFingerprint {
				_ = os.Remove(mp4Path)
				w.logger.Info("直播源已转为回放，丢弃重复窗并结束录像",
					liveRecordFields("end_replay_dup", material.ID,
						zap.Int("window_index", winIdx),
						zap.String("reason", replayReason),
						zap.Bool("playlist_endlist", hasEndList),
						zap.Duration("elapsed", elapsed),
						zap.Int64("probed_dur_ms", durMS),
						zap.String("fingerprint", fp),
					)...,
				)
				break
			}
			// 回放信号但内容与上窗不同：快路径登记本窗后关播（master 异步）。
			partial := durMS < fullWindowMS || recErr != nil
			if err := w.sealRecordedWindow(ctx, material, winIdx, mp4Path, partial); err != nil {
				w.logger.Warn("提交回放尾窗失败，仍结束录像",
					liveRecordFields("seal_replay_tail_fail", material.ID,
						zap.Int("window_index", winIdx),
						zap.String("reason", replayReason),
						zap.Error(err),
					)...,
				)
				break
			}
			gotAnyWindow = true
			if fpErr == nil {
				lastFingerprint = fp
			}
			w.logger.Info("直播源已转为回放，已提交本窗并结束录像",
				liveRecordFields("end_replay_sealed", material.ID,
					zap.Int("window_index", winIdx),
					zap.String("reason", replayReason),
					zap.Bool("playlist_endlist", hasEndList),
					zap.Duration("elapsed", elapsed),
					zap.Int64("probed_dur_ms", durMS),
				)...,
			)
			break
		}

		partial := durMS < fullWindowMS || recErr != nil
		if err := w.sealRecordedWindow(ctx, material, winIdx, mp4Path, partial); err != nil {
			w.logger.Warn("登记媒体窗失败",
				liveRecordFields("seal_fail", material.ID,
					zap.Int("window_index", winIdx),
					zap.Bool("partial", partial),
					zap.Error(err),
				)...,
			)
			// 满窗登记失败可稍后重试同一窗；partial 且已有窗则关播。
			if partial {
				if gotAnyWindow {
					break
				}
				return err
			}
			w.logger.Info("满窗登记失败，稍后重试同一窗",
				liveRecordFields("seal_retry", material.ID,
					zap.Int("window_index", winIdx),
				)...,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(liveIngestProbeInterval):
			}
			continue
		}
		gotAnyWindow = true
		emptyAttempts = 0
		if fpErr == nil {
			lastFingerprint = fp
		}
		w.logger.Info("媒体窗登记成功，继续下一窗或收尾",
			liveRecordFields("seal_ok", material.ID,
				zap.Int("window_index", winIdx),
				zap.Bool("partial", partial),
				zap.Int64("dur_ms", durMS),
				zap.Int64("next_window_seg", material.NextWindowSeg),
				zap.Float64("wall_ratio", wallRatio),
			)...,
		)

		if partial {
			w.logger.Info("媒体窗为 partial（断流或不足目标时长），结束录像",
				liveRecordFields("end_partial", material.ID,
					zap.Int("window_index", winIdx),
					zap.Int64("dur_ms", durMS),
					zap.Int64("full_window_ms", fullWindowMS),
				)...,
			)
			break
		}
	}

	if !gotAnyWindow {
		msg := "未能录制到直播媒体窗"
		w.markIngestFailedLogged(ctx, material, "no_window_final", msg)
		return fmt.Errorf("%s", msg)
	}

	if err := w.repo.MarkEnding(ctx, material.ID, material.IngestEpoch); err != nil {
		w.logger.Error("标记 ending 写库失败（Finalize 可能无法接手）",
			liveRecordFields("mark_ending_fail", material.ID, zap.Error(err))...,
		)
	} else {
		w.logger.Info("录像结束，已标记 ending 并交 Finalize 流水线",
			liveRecordFields("mark_ending_ok", material.ID,
				zap.Int64("next_window", material.NextWindowSeg),
				zap.Int64("duration_ms", material.Duration),
				zap.Int("ready_windows", material.ParsedMediaWindows().ReadyCount()),
			)...,
		)
	}
	material.LiveStatus = model.LiveStatusEnding
	enqueueWake(w.finalizeWake, 1)
	enqueueWake(w.asrWake, 1)
	return nil
}

func (w *liveIngestWorker) finalizeRecording(ctx context.Context, material *model.LiveMaterial, resumeFrom int64) error {
	_ = resumeFrom
	w.logger.Info("Finalize 开始收尾录像",
		liveRecordFields("finalize_start", material.ID,
			zap.Int64("duration_ms", material.Duration),
			zap.Int("ready_windows", material.ParsedMediaWindows().ReadyCount()),
		)...,
	)
	latest, err := w.repo.GetByID(ctx, material.ID)
	if err != nil {
		w.logger.Warn("Finalize 刷新素材失败，沿用内存快照",
			liveRecordFields("finalize_refresh_fail", material.ID, zap.Error(err))...,
		)
	} else if latest != nil {
		material = latest
	}

	// 等异步 master 队列排空，再校验/补拼主片。
	w.logger.Info("Finalize 等待异步 master 队列排空",
		liveRecordFields("finalize_wait_master", material.ID,
			zap.Int("master_pending", w.masterQueuePending(material.ID)),
		)...,
	)
	if err := w.waitMasterQueueDrain(ctx, material.ID); err != nil {
		w.logger.Error("Finalize 等待 master 队列失败",
			liveRecordFields("finalize_wait_master_fail", material.ID, zap.Error(err))...,
		)
		return fmt.Errorf("等待异步 master 完成失败: %w", err)
	}
	w.logger.Info("Finalize master 队列已排空",
		liveRecordFields("finalize_master_drained", material.ID)...,
	)
	if latest2, gerr := w.repo.GetByID(ctx, material.ID); gerr == nil && latest2 != nil {
		material = latest2
	}

	windows := material.ParsedMediaWindows().ReadyWindows()
	if len(windows) == 0 {
		msg := "关播时没有可用媒体窗"
		w.markIngestFailedLogged(ctx, material, "finalize_no_windows", msg)
		return fmt.Errorf("%s", msg)
	}

	workDir := w.segmentDir(material)
	_ = os.MkdirAll(workDir, 0o755)

	masterPath := filepath.Join(workDir, liveingest.MasterMP4FileName())
	needRebuild := false
	rebuildReason := ""
	if st, err := os.Stat(masterPath); err != nil || st.Size() == 0 {
		needRebuild = true
		rebuildReason = "master_missing_or_empty"
	} else if material.ParsedMediaWindows().TotalReadyMS() > material.Duration+masterAppendDurationSlackMS {
		needRebuild = true
		rebuildReason = "sealed_ahead_of_duration"
	}
	if needRebuild {
		w.logger.Info("Finalize 需要补拼 master",
			liveRecordFields("finalize_rebuild", material.ID,
				zap.String("reason", rebuildReason),
				zap.Int("window_count", len(windows)),
				zap.Int64("sealed_ms", material.ParsedMediaWindows().TotalReadyMS()),
				zap.Int64("duration_ms", material.Duration),
			)...,
		)
		if err := w.rebuildMasterFromWindows(ctx, material, true); err != nil {
			if errors.Is(err, errWindowUnrecoverable) {
				w.markIngestFailedLogged(ctx, material, "finalize_window_missing", err.Error())
			}
			w.logger.Error("Finalize 补拼 master 失败",
				liveRecordFields("finalize_rebuild_fail", material.ID, zap.Error(err))...,
			)
			return fmt.Errorf("关播拼接主 MP4 失败: %w", err)
		}
		if latest3, gerr := w.repo.GetByID(ctx, material.ID); gerr == nil && latest3 != nil {
			material.MediaWindows = latest3.MediaWindows
			material.NextWindowSeg = latest3.NextWindowSeg
			material.NextSeg = latest3.NextSeg
			material.Duration = latest3.Duration
			material.LiveURL = latest3.LiveURL
			material.LiveASR = latest3.LiveASR
			material.ASRCursorMS = latest3.ASRCursorMS
		}
	} else {
		w.logger.Info("Finalize 本地 master 已对齐，跳过补拼",
			liveRecordFields("finalize_skip_rebuild", material.ID,
				zap.Int("window_count", len(windows)),
				zap.Int64("duration_ms", material.Duration),
			)...,
		)
	}

	dur := material.Duration
	if w.prober != nil {
		if tl, err := w.prober.ProbeMediaTimeline(ctx, masterPath); err == nil {
			probedSec := tl.VideoDurationSec
			if probedSec <= 0 {
				probedSec = tl.FormatDurationSec
			}
			if probedSec > 0 {
				dur = int64(probedSec * 1000)
			}
			if tl.Width > 0 {
				material.Width = tl.Width
			}
			if tl.Height > 0 {
				material.Height = tl.Height
			}
		} else {
			w.logger.Warn("Finalize 探测 master 时长失败，沿用库内 duration",
				liveRecordFields("finalize_probe_fail", material.ID, zap.Error(err))...,
			)
		}
	}
	if dur > 0 && dur != material.Duration {
		w.logger.Info("Finalize 校正 master 时长",
			liveRecordFields("finalize_duration_fix", material.ID,
				zap.Int64("old_ms", material.Duration),
				zap.Int64("new_ms", dur),
			)...,
		)
		material.Duration = dur
		if err := w.repo.CommitMasterMP4(ctx, material.ID, material.IngestEpoch, material.MediaWindows, material.NextWindowSeg, dur, material.LiveURL, true); err != nil {
			w.logger.Warn("Finalize 写回校正时长失败",
				liveRecordFields("finalize_duration_commit_fail", material.ID, zap.Error(err))...,
			)
		}
		material.NextSeg = material.NextWindowSeg
	}

	if err := w.repo.MarkEnded(ctx, material.ID, material.IngestEpoch, dur); err != nil {
		w.logger.Error("标记 ended 失败",
			liveRecordFields("mark_ended_fail", material.ID, zap.Error(err))...,
		)
		return err
	}
	w.logger.Info("已标记 ended，开始关播 ASR 收尾",
		liveRecordFields("mark_ended_ok", material.ID,
			zap.Int64("duration_ms", dur),
			zap.Int("window_count", len(windows)),
		)...,
	)
	material.Duration = dur
	w.catchUpWindowASRUnderLease(ctx, material)
	if err := w.finishASRPostprocess(ctx, material, dur); err != nil {
		w.logger.Warn("关播 ASR 后处理失败，将保留 ended+processing 供重试",
			liveRecordFields("finalize_asr_post_fail", material.ID, zap.Error(err))...,
		)
		_ = os.RemoveAll(workDir)
		return err
	}
	w.logger.Info("Finalize 完成，清理本地工作目录",
		liveRecordFields("finalize_done", material.ID,
			zap.String("work_dir", workDir),
			zap.Int64("duration_ms", dur),
		)...,
	)
	_ = os.RemoveAll(workDir)
	return nil
}

// windowTimelineMS 优先用 MediaTimelineIndex / sidecar 分片时长之和，否则用 concat 探针。
func windowTimelineMS(workDir string, files []string, concatProbeMS, nominalSegMS int64) int64 {
	if nominalSegMS <= 0 {
		nominalSegMS = 6000
	}
	durs := loadSegDurations(workDir)
	var sum int64
	known := 0
	for _, f := range files {
		idx, ok := parseLocalSegIndex(f)
		if !ok || idx < 0 {
			sum += nominalSegMS
			continue
		}
		if ms := durs[int64(idx)]; ms > 0 {
			sum += ms
			known++
		} else {
			sum += nominalSegMS
		}
	}
	if sum > 0 && known*2 >= len(files) {
		return sum
	}
	if concatProbeMS > 0 {
		return concatProbeMS
	}
	if sum > 0 {
		return sum
	}
	return concatProbeMS
}

// catchUpWindowASR 在租约内对当前 master 做全量 ASR；若跑输（master 又变长）则再跑，直到 cursor 追上 ready。
func (w *liveIngestWorker) catchUpWindowASR(ctx context.Context, material *model.LiveMaterial) {
	const maxRounds = 32 // 约等于媒体窗数量上限；每轮一次全量 Transcribe
	for i := 0; i < maxRounds; i++ {
		if err := ctx.Err(); err != nil {
			w.logger.Warn("全量 ASR 追平因上下文取消退出",
				zap.Uint("material_id", material.ID),
				zap.Int("round", i),
				zap.Int64("asr_cursor_ms", material.ASRCursorMS),
				zap.Int64("duration_ms", material.Duration),
				zap.Error(err),
			)
			return
		}
		beforeCursor := material.ASRCursorMS
		beforeReady := material.MasterReadyMS()
		if err := w.runFullMasterASR(ctx, material); err != nil {
			w.logger.Warn("全量 ASR 追平中断",
				zap.Uint("material_id", material.ID),
				zap.Int("round", i),
				zap.Error(err),
			)
			return
		}
		if latest, gerr := w.repo.GetByID(ctx, material.ID); gerr == nil && latest != nil {
			material.ASRCursorMS = latest.ASRCursorMS
			material.LiveASR = latest.LiveASR
			material.Duration = latest.Duration
			material.MediaWindows = latest.MediaWindows
			material.ASRDue = latest.ASRDue
			material.ASRParagraphs = latest.ASRParagraphs
			material.LiveURL = latest.LiveURL
		}
		if !material.ParsedMediaWindows().ASRPending(material.ASRCursorMS) && !material.ASRDue {
			return
		}
		// 无进展且未标记再跑：结束，避免空转
		if material.ASRCursorMS <= beforeCursor && material.MasterReadyMS() <= beforeReady && !material.ASRDue {
			return
		}
	}
	w.logger.Warn("全量 ASR 追平达到轮次上限",
		zap.Uint("material_id", material.ID),
		zap.Int64("asr_cursor_ms", material.ASRCursorMS),
		zap.Int64("duration_ms", material.Duration),
	)
}

// catchUpWindowASRUnderLease 在 Finalize 内抢 ASR 租约后追平，避免与 WindowASR Worker 并发转写。
func (w *liveIngestWorker) catchUpWindowASRUnderLease(ctx context.Context, material *model.LiveMaterial) {
	var claimed *model.LiveMaterial
	var err error
	claimed, err = w.repo.ClaimWindowASRWork(ctx)
	if err != nil {
		w.logger.Warn("Finalize 抢占 ASR 租约失败，跳过追平", zap.Uint("material_id", material.ID), zap.Error(err))
		return
	}
	if claimed == nil || claimed.ID != material.ID {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			if ctx.Err() != nil {
				return
			}
			latest, gerr := w.repo.GetByID(ctx, material.ID)
			if gerr != nil || latest == nil {
				return
			}
			material.ASRCursorMS = latest.ASRCursorMS
			material.LiveASR = latest.LiveASR
			material.Duration = latest.Duration
			material.ASREpoch = latest.ASREpoch
			material.MediaWindows = latest.MediaWindows
			material.ASRDue = latest.ASRDue
			if !latest.ParsedMediaWindows().ASRPending(latest.ASRCursorMS) {
				return
			}
			if !latest.ASRDue {
				_ = w.repo.CommitMasterMP4(ctx, latest.ID, latest.IngestEpoch, latest.MediaWindows, latest.NextWindowSeg, latest.Duration, latest.LiveURL, true)
			}
			claimed, err = w.repo.ClaimWindowASRWork(ctx)
			if err != nil {
				claimed = nil
			} else if claimed != nil && claimed.ID != material.ID {
				_ = w.repo.ReleaseASRLease(ctx, claimed.ID, claimed.ASREpoch)
				claimed = nil
			}
			if claimed != nil && claimed.ID == material.ID {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		if claimed == nil || claimed.ID != material.ID {
			w.logger.Warn("Finalize 等待 ASR 租约超时，继续后处理",
				zap.Uint("material_id", material.ID),
				zap.Int64("asr_cursor_ms", material.ASRCursorMS),
				zap.Int64("duration_ms", material.Duration),
			)
			return
		}
	}
	material.ASREpoch = claimed.ASREpoch
	stopHB := w.startASRHeartbeat(ctx, material.ID, material.ASREpoch)
	defer stopHB()
	defer func() {
		_ = w.repo.ReleaseASRLease(context.Background(), material.ID, material.ASREpoch)
	}()
	_ = w.repo.MarkASRProcessing(ctx, material.ID, material.ASREpoch)
	w.catchUpWindowASR(ctx, material)
	if latest, gerr := w.repo.GetByID(ctx, material.ID); gerr == nil && latest != nil {
		if !latest.ParsedMediaWindows().ASRPending(latest.ASRCursorMS) {
			_ = w.repo.ClearASRDue(ctx, material.ID, material.ASREpoch)
		}
	} else {
		_ = w.repo.ClearASRDue(ctx, material.ID, material.ASREpoch)
	}
}

// maxWindowASRSegments 单次送去转写的最大分片数（短 chunk + 1 片重叠余量）。
func maxWindowASRSegments() int {
	segSec := model.LiveSegmentDurationSec
	if segSec <= 0 {
		segSec = 6
	}
	chunkSec := int(model.MaxASRTranscribeDuration / time.Second)
	if chunkSec <= 0 {
		chunkSec = 2 * 60
	}
	n := chunkSec / segSec
	if n < 1 {
		n = 1
	}
	return n + 1
}

// measureSegmentPrefixDurationMS 累加 startSeg 之前本地分片的真实时长（仅探测前缀，带单片超时）。
func (w *liveIngestWorker) measureSegmentPrefixDurationMS(ctx context.Context, material *model.LiveMaterial, startSeg int64) int64 {
	if startSeg <= 0 {
		return 0
	}
	prober := w.prober
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}
	dir := w.segmentDir(material)
	byIdx := make(map[int]string)
	for _, f := range globLocalSegments(dir) {
		idx, ok := parseLocalSegIndex(f)
		if !ok || idx < 0 || int64(idx) >= startSeg {
			continue
		}
		byIdx[idx] = f
	}
	var sum int64
	for i := int64(0); i < startSeg; i++ {
		if p, ok := byIdx[int(i)]; ok {
			sum += probeSegmentDurationMS(ctx, prober, p, nominal)
		} else {
			sum += nominal
		}
	}
	return sum
}

// measureLocalRecordingDurationMS 累加本地已落盘分片的真实时长，并写入 outDur（按分片下标）。
// 用于续录校正 duration，以及关播写 m3u8 的 EXTINF。
func (w *liveIngestWorker) measureLocalRecordingDurationMS(ctx context.Context, material *model.LiveMaterial, outDur map[int64]int64) int64 {
	prober := w.prober
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}
	var sum int64
	for _, f := range globLocalSegments(w.segmentDir(material)) {
		idx, ok := parseLocalSegIndex(f)
		if !ok || idx < 0 {
			continue
		}
		ms := probeSegmentDurationMS(ctx, prober, f, nominal)
		if ms <= 0 {
			ms = nominal
		}
		if outDur != nil {
			outDur[int64(idx)] = ms
		}
		sum += ms
	}
	return sum
}

func (w *liveIngestWorker) finishASRPostprocess(ctx context.Context, material *model.LiveMaterial, duration int64) error {
	latest, err := w.repo.GetByID(ctx, material.ID)
	if err != nil {
		return err
	}
	if duration <= 0 {
		duration = latest.Duration
	}
	epoch := material.IngestEpoch
	if epoch <= 0 {
		epoch = latest.IngestEpoch
	}

	post, postErr := runASRPostprocess(ctx, w.llmClient, latest.LiveASR, duration, nil, w.logger)
	if postErr != nil {
		w.logger.Warn("关播 ASR summaries LLM 失败，回退为空 summaries；段落仍用 MinGap 算法并标记完成",
			zap.Uint("material_id", latest.ID),
			zap.Error(postErr),
		)
		post = localASRPostprocessFallback(latest.LiveASR, duration)
	}
	if err := w.repo.FinalizeASR(ctx, latest.ID, epoch, latest.LiveASR, duration, latest.Width, latest.Height, post.Summaries, post.Paragraphs); err != nil {
		return err
	}
	w.logger.Info("关播 ASR 已标记完成",
		zap.Uint("material_id", latest.ID),
		zap.Int64("duration_ms", duration),
		zap.Int("summaries", len(post.Summaries)),
		zap.Int("paragraphs", len(post.Paragraphs)),
		zap.Bool("llm_fallback", postErr != nil),
	)
	return nil
}

// localASRPostprocessFallback summaries LLM 不可用时：summaries 为空，段落用 MinGap 算法（尽力产出，不做严格校验），保证关播仍能到 ASR 完成态。
func localASRPostprocessFallback(liveASR string, durationMs int64) asrPostprocessResult {
	out := asrPostprocessResult{}
	utterances := asr.FormatUtterancesForAPI(liveASR)
	if len(utterances) == 0 {
		return out
	}
	paras, _ := BuildASRParagraphsByMinGap(utterances, asrParagraphMaxRunes, nil)
	paras, _ = enforceASRParagraphMaxRunes(paras)
	finalizeASRParagraphTimeline(paras, durationMs)
	out.Paragraphs = paras
	return out
}

func (w *liveIngestWorker) segmentObjectKey(material *model.LiveMaterial, resumeFrom, index int64) string {
	epoch := liveingest.SegmentStorageEpoch(material.IngestEpoch, resumeFrom, index)
	return liveingest.SegmentObjectKey(material.RecordUUID, epoch, index)
}

func (w *liveIngestWorker) uploadSegment(ctx context.Context, material *model.LiveMaterial, index int64, localPath string, resumeFrom int64) (string, error) {
	if w.storage == nil {
		return "", fmt.Errorf("对象存储未配置")
	}
	return w.storage.UploadFile(ctx, localPath, w.segmentObjectKey(material, resumeFrom, index))
}

func (w *liveIngestWorker) publishPlaylist(ctx context.Context, material *model.LiveMaterial, segURLs map[int64]string, segDurMS map[int64]int64, ended bool, resumeFrom int64) (string, error) {
	if w.storage == nil {
		return "", fmt.Errorf("对象存储未配置")
	}
	segURLs = w.completeSegmentURLs(ctx, material, segURLs, resumeFrom)
	keys := make([]int64, 0, len(segURLs))
	for k := range segURLs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	nominalSec := float64(model.LiveSegmentDurationSec)
	if nominalSec <= 0 {
		nominalSec = 6
	}
	items := make([]liveingest.PlaylistItem, 0, len(keys))
	for _, k := range keys {
		durSec := nominalSec
		if segDurMS != nil {
			if ms, ok := segDurMS[k]; ok && ms > 0 {
				durSec = float64(ms) / 1000.0
			}
		}
		items = append(items, liveingest.PlaylistItem{
			DurationSec: durSec,
			URL:         segURLs[k],
		})
	}
	body := liveingest.BuildEventPlaylist(items, model.LiveSegmentDurationSec, ended)
	tmp := filepath.Join(w.segmentDir(material), "live.m3u8")
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	uploaded, err := w.storage.UploadFile(ctx, tmp, liveingest.PlaylistObjectKey(material.RecordUUID))
	if err != nil {
		return "", err
	}
	return liveingest.PreferStablePlaylistURL(material.RecordPlaylistURL, uploaded), nil
}

func (w *liveIngestWorker) completeSegmentURLs(ctx context.Context, material *model.LiveMaterial, uploaded map[int64]string, resumeFrom int64) map[int64]string {
	out := make(map[int64]string, len(uploaded)+int(material.NextSeg))
	for k, v := range uploaded {
		if v != "" {
			out[k] = v
		}
	}
	max := material.NextSeg - 1
	for k := range out {
		if k > max {
			max = k
		}
	}
	if w.storage == nil || max < 0 {
		return out
	}
	for i := int64(0); i <= max; i++ {
		if out[i] != "" {
			continue
		}
		url, err := w.storage.AccessURL(ctx, w.segmentObjectKey(material, resumeFrom, i))
		if err != nil || url == "" {
			continue
		}
		out[i] = url
	}
	return out
}

func (w *liveIngestWorker) segmentDir(material *model.LiveMaterial) string {
	root := w.web.RootDir
	if root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "staging", "live_ingest", fmt.Sprintf("%d", material.ID), fmt.Sprintf("e%d", material.IngestEpoch))
}

// resetLiveIngestWorkDir 删除跟播工作目录，避免删库/重建同 ID 后扫到旧窗/master。
func resetLiveIngestWorkDir(workDir string) error {
	if strings.TrimSpace(workDir) == "" {
		return nil
	}
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	return os.MkdirAll(workDir, 0o755)
}

func (w *liveIngestWorker) listUploadedSegmentFiles(ctx context.Context, material *model.LiveMaterial, resumeFrom int64) []string {
	return w.collectLocalSegmentFiles(ctx, material, resumeFrom)
}

func (w *liveIngestWorker) collectLocalSegmentFiles(ctx context.Context, material *model.LiveMaterial, resumeFrom int64) []string {
	dir := w.segmentDir(material)
	_ = os.MkdirAll(dir, 0o755)
	if material.NextSeg <= 0 {
		return globLocalSegments(dir)
	}
	out := make([]string, 0, material.NextSeg)
	missing := 0
	for i := int64(0); i < material.NextSeg; i++ {
		local := filepath.Join(dir, liveingest.SegmentFileName(int(i)))
		if _, err := os.Stat(local); err == nil {
			out = append(out, local)
			continue
		}
		if w.storage == nil {
			missing++
			continue
		}
		url, err := w.storage.AccessURL(ctx, w.segmentObjectKey(material, resumeFrom, i))
		if err != nil || url == "" {
			missing++
			continue
		}
		if err := downloadHTTPFile(ctx, w.httpClient, url, local); err != nil {
			missing++
			continue
		}
		out = append(out, local)
	}
	if missing > 0 {
		w.logger.Warn("回拉分片有缺失",
			zap.Uint("material_id", material.ID),
			zap.Int("missing", missing),
			zap.Int("restored", len(out)),
			zap.Int64("next_seg", material.NextSeg),
		)
	}
	if len(out) == 0 {
		return globLocalSegments(dir)
	}
	return out
}

func downloadHTTPFile(ctx context.Context, client *http.Client, url, dest string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("下载分片 HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.ReadFrom(resp.Body)
	return err
}

func (w *liveIngestWorker) collectSegmentURLs(ctx context.Context, material *model.LiveMaterial, files []string, resumeFrom int64) map[int64]string {
	out := map[int64]string{}
	if w.storage == nil {
		return out
	}
	for _, f := range files {
		idx, ok := parseLocalSegIndex(f)
		if !ok {
			continue
		}
		url, err := w.storage.AccessURL(ctx, w.segmentObjectKey(material, resumeFrom, int64(idx)))
		if err != nil || url == "" {
			continue
		}
		out[int64(idx)] = url
	}
	return out
}

func globLocalSegments(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "seg_*.ts"))
	sort.Strings(matches)
	return matches
}

// latestLocalSegIndex 返回目录中最大 seg 序号；无分片时为 -1。
// 用于对比 ffmpeg 已写盘进度与 onSeg 上传进度（upload_lag_segs）。
func latestLocalSegIndex(dir string) int64 {
	matches, err := filepath.Glob(filepath.Join(dir, "seg_*.ts"))
	if err != nil || len(matches) == 0 {
		return -1
	}
	max := int64(-1)
	for _, p := range matches {
		idx, ok := parseLocalSegIndex(p)
		if !ok {
			continue
		}
		if int64(idx) > max {
			max = int64(idx)
		}
	}
	return max
}

func globLocalSegmentsFrom(dir string, startIndex int) []string {
	all := globLocalSegments(dir)
	out := make([]string, 0, len(all))
	for _, f := range all {
		idx, ok := parseLocalSegIndex(f)
		if ok && idx >= startIndex {
			out = append(out, f)
		}
	}
	return out
}

func parseLocalSegIndex(path string) (int, bool) {
	base := filepath.Base(path)
	var n int
	if _, err := fmt.Sscanf(base, "seg_%d.ts", &n); err != nil {
		return 0, false
	}
	return n, true
}
