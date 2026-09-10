package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// LiveIngestWorker 跟播长任务：探测 / 分片录像 / 窗口 ASR / 合成 mp4。
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

	wake      chan struct{}
	startOnce sync.Once

	cancelsMu sync.Mutex
	cancels   map[uint]*ingestCancelEntry
}

type ingestCancelEntry struct {
	cancel context.CancelFunc
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
		repo:          repo,
		asrService:    asrService,
		audioPreparer: audioPreparer,
		llmClient:     llmClient,
		storage:       storageClient,
		ffmpeg:        media.NewFFmpegConverter(""),
		prober:        media.NewFFprobeProber(""),
		httpClient:    &http.Client{Timeout: 20 * time.Second},
		web:           web,
		logger:        logger,
		concurrency:   concurrency,
		wake:          newWakeChan(concurrency),
		cancels:       make(map[uint]*ingestCancelEntry),
	}
}

func (w *liveIngestWorker) Enqueue() {
	enqueueWake(w.wake, w.concurrency)
}

// Cancel 取消本进程内正在执行的跟播任务。
func (w *liveIngestWorker) Cancel(materialID uint) bool {
	w.cancelsMu.Lock()
	entry, ok := w.cancels[materialID]
	if ok {
		delete(w.cancels, materialID)
	}
	w.cancelsMu.Unlock()
	if !ok || entry == nil {
		return false
	}
	entry.cancel()
	w.logger.Info("已请求取消跟播任务", zap.Uint("material_id", materialID))
	return true
}

func (w *liveIngestWorker) bindCancel(materialID uint, cancel context.CancelFunc) func() {
	entry := &ingestCancelEntry{cancel: cancel}
	w.cancelsMu.Lock()
	if prev, ok := w.cancels[materialID]; ok && prev != nil {
		prev.cancel()
	}
	w.cancels[materialID] = entry
	w.cancelsMu.Unlock()
	return func() {
		w.cancelsMu.Lock()
		if cur, ok := w.cancels[materialID]; ok && cur == entry {
			delete(w.cancels, materialID)
		}
		w.cancelsMu.Unlock()
	}
}

func (w *liveIngestWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		for i := 0; i < w.concurrency; i++ {
			go w.loop(ctx, i)
		}
		go w.pollLoop(ctx)
		w.Enqueue()
		w.logger.Info("直播跟播 Worker 已启动", zap.Int("concurrency", w.concurrency))
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

func (w *liveIngestWorker) loop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.drain(ctx, workerID)
		}
	}
}

func (w *liveIngestWorker) drain(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		material, err := w.repo.ClaimIngestWork(ctx)
		if err != nil {
			w.logger.Error("抢占跟播任务失败", zap.Int("worker_id", workerID), zap.Error(err))
			return
		}
		if material == nil {
			return
		}
		w.logger.Info("已抢占跟播任务",
			zap.Uint("material_id", material.ID),
			zap.String("live_status", material.LiveStatus),
			zap.Int64("ingest_epoch", material.IngestEpoch),
		)
		if err := w.Process(ctx, material); err != nil {
			if errors.Is(err, context.Canceled) {
				w.logger.Info("跟播任务已取消",
					zap.Uint("material_id", material.ID),
				)
			} else {
				w.logger.Warn("跟播任务结束",
					zap.Uint("material_id", material.ID),
					zap.Error(err),
				)
			}
		}
	}
}

// Process 执行一场跟播（无 4 小时硬超时）。
func (w *liveIngestWorker) Process(ctx context.Context, material *model.LiveMaterial) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	unbind := w.bindCancel(material.ID, cancel)
	defer unbind()

	stopHB := w.startHeartbeat(ctx, material.ID, material.IngestEpoch)
	defer stopHB()

	w.logger.Info("开始处理跟播任务",
		zap.Uint("material_id", material.ID),
		zap.String("live_status", material.LiveStatus),
		zap.String("source_mode", material.SourceMode),
		zap.String("asr_status", material.ASRStatus),
		zap.Int16("asr_progress", material.ASRProgress),
		zap.Int64("asr_cursor_ms", material.ASRCursorMS),
		zap.Int64("next_seg", material.NextSeg),
		zap.Int64("ingest_epoch", material.IngestEpoch),
		zap.String("m3u8_url", material.M3U8URL),
	)

	switch material.LiveStatus {
	case model.LiveStatusWaiting, model.LiveStatusConnecting:
		w.logger.Info("跟播进入等待媒体阶段", zap.Uint("material_id", material.ID))
		if err := w.waitForMedia(ctx, material); err != nil {
			return err
		}
		w.logger.Info("已检测到直播媒体，进入录像", zap.Uint("material_id", material.ID))
		return w.recordAndFinalize(ctx, material)
	case model.LiveStatusLive:
		w.logger.Info("跟播已是 live，直接进入录像", zap.Uint("material_id", material.ID))
		return w.recordAndFinalize(ctx, material)
	case model.LiveStatusEnding:
		w.logger.Info("跟播进入收尾合成", zap.Uint("material_id", material.ID))
		return w.finalizeRecording(ctx, material, material.NextSeg)
	case model.LiveStatusEnded:
		// 已关播但 ASR 仍 processing（常见于 LLM 后处理失败未 Finalize）时补完。
		if material.ASRStatus == model.ASRStatusCompleted {
			w.logger.Info("关播 ASR 已完成，跳过", zap.Uint("material_id", material.ID))
			return nil
		}
		w.logger.Info("关播后补完 ASR 收尾",
			zap.Uint("material_id", material.ID),
			zap.String("asr_status", material.ASRStatus),
			zap.Int16("asr_progress", material.ASRProgress),
		)
		return w.finishASRPostprocess(ctx, material, material.Duration)
	default:
		w.logger.Info("跟播状态无需处理，跳过",
			zap.Uint("material_id", material.ID),
			zap.String("live_status", material.LiveStatus),
		)
		return nil
	}
}

func (w *liveIngestWorker) startHeartbeat(ctx context.Context, id uint, epoch int64) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(liveIngestHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = w.repo.HeartbeatIngest(context.Background(), id, epoch)
			}
		}
	}()
	return func() { close(done) }
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
		if time.Now().Before(startAt) {
			timer := time.NewTimer(time.Until(startAt))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}

	_ = w.repo.MarkConnecting(ctx, material.ID, material.IngestEpoch)
	material.LiveStatus = model.LiveStatusConnecting

	ticker := time.NewTicker(liveIngestProbeInterval)
	defer ticker.Stop()
	for {
		if time.Now().After(deadline) {
			msg := "计划开播后 2 小时内未检测到直播"
			if material.SourceMode == model.SourceModeLive {
				msg = "添加后 2 小时内未检测到直播"
			}
			_ = w.repo.MarkIngestFailed(ctx, material.ID, material.IngestEpoch, msg)
			return fmt.Errorf("%s", msg)
		}
		probe, err := media.ProbeHLSPlaylist(w.httpClient, material.M3U8URL)
		if probe.Encrypted {
			msg := media.SanitizeHLSError(err)
			if msg == "" {
				msg = "不支持加密 HLS"
			}
			_ = w.repo.MarkIngestFailed(ctx, material.ID, material.IngestEpoch, msg)
			if err != nil {
				return err
			}
			return fmt.Errorf("%s", msg)
		}
		if probe.HasMedia {
			w.logger.Info("HTTP 探测到直播播放列表含媒体分片",
				zap.Uint("material_id", material.ID),
				zap.String("m3u8_url", material.M3U8URL),
			)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *liveIngestWorker) recordAndFinalize(ctx context.Context, material *model.LiveMaterial) error {
	width, height := material.Width, material.Height
	if w.prober != nil {
		probeStart := time.Now()
		w.logger.Info("开始 ffprobe 探测直播流（若长时间无后续日志，多半卡在此处）",
			zap.Uint("material_id", material.ID),
			zap.String("m3u8_url", material.M3U8URL),
		)
		tl, err := w.prober.ProbeMediaTimeline(ctx, material.M3U8URL)
		if err != nil {
			w.logger.Warn("ffprobe 探测直播流失败，将沿用已有宽高继续录像",
				zap.Uint("material_id", material.ID),
				zap.Duration("elapsed", time.Since(probeStart)),
				zap.Error(err),
			)
		} else {
			width, height = tl.Width, tl.Height
			w.logger.Info("ffprobe 探测直播流完成",
				zap.Uint("material_id", material.ID),
				zap.Duration("elapsed", time.Since(probeStart)),
				zap.Int("width", width),
				zap.Int("height", height),
			)
		}
	} else {
		w.logger.Info("未配置 prober，跳过直播流探测", zap.Uint("material_id", material.ID))
	}
	if err := w.repo.MarkLiveStarted(ctx, material.ID, material.IngestEpoch, width, height); err != nil {
		w.logger.Warn("标记 live / ASR processing 失败（页面可能仍显示等待解析）",
			zap.Uint("material_id", material.ID),
			zap.Error(err),
		)
	} else {
		w.logger.Info("已标记 live 并置 ASR 为 processing",
			zap.Uint("material_id", material.ID),
			zap.Int64("ingest_epoch", material.IngestEpoch),
		)
	}
	material.LiveStatus = model.LiveStatusLive
	material.Width, material.Height = width, height

	workDir := w.segmentDir(material)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	resumeFrom := material.NextSeg

	var asrMu sync.Mutex
	asrBusy := false
	waitASRIdle := func() {
		for {
			asrMu.Lock()
			idle := !asrBusy
			asrMu.Unlock()
			if idle {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	runWindowASRExclusive := func() {
		waitASRIdle()
		asrMu.Lock()
		asrBusy = true
		asrMu.Unlock()
		defer func() {
			asrMu.Lock()
			asrBusy = false
			asrMu.Unlock()
		}()
		w.catchUpWindowASR(ctx, material)
	}
	scheduleWindowASR := func() {
		asrMu.Lock()
		if asrBusy {
			asrMu.Unlock()
			w.logger.Info("窗口 ASR 仍在执行，跳过本轮调度",
				zap.Uint("material_id", material.ID),
				zap.Int64("next_seg", material.NextSeg),
			)
			return
		}
		asrBusy = true
		asrMu.Unlock()
		w.logger.Info("调度窗口 ASR",
			zap.Uint("material_id", material.ID),
			zap.Int64("duration_ms", material.Duration),
			zap.Int64("asr_cursor_ms", material.ASRCursorMS),
			zap.Int64("next_seg", material.NextSeg),
		)
		go func() {
			defer func() {
				asrMu.Lock()
				asrBusy = false
				asrMu.Unlock()
			}()
			// 定时窗只跑一窗（有分片上限）；积压留给后续调度或关播 catch-up。
			_ = w.runWindowASR(ctx, material)
		}()
	}

	segURLs := map[int64]string{}
	if material.NextSeg > 0 {
		segURLs = w.completeSegmentURLs(ctx, material, nil, resumeFrom)
	}
	for {
		startIndex := int(material.NextSeg)
		windowDue := time.Now().Add(model.LiveASRWindowDuration)
		var firstSegOnce sync.Once

		onSeg := func(index int, path string) {
			firstSegOnce.Do(func() {
				w.logger.Info("收到首个录像分片",
					zap.Uint("material_id", material.ID),
					zap.Int("index", index),
					zap.String("path", path),
				)
			})
			url, err := w.uploadSegment(ctx, material, int64(index), path, resumeFrom)
			if err != nil {
				w.logger.Warn("上传分片失败", zap.Uint("material_id", material.ID), zap.Int("index", index), zap.Error(err))
				return
			}
			segURLs[int64(index)] = url
			next := int64(index + 1)
			if next < material.NextSeg {
				next = material.NextSeg
			}
			dur := next * int64(model.LiveSegmentDurationSec) * 1000
			playlistURL, plErr := w.publishPlaylist(ctx, material, segURLs, false, resumeFrom)
			if plErr != nil {
				w.logger.Warn("发布播放列表失败", zap.Uint("material_id", material.ID), zap.Error(plErr))
			}
			if playlistURL != "" {
				material.RecordPlaylistURL = playlistURL
			}
			_ = w.repo.UpdateRecordingProgress(ctx, material.ID, material.IngestEpoch, next, dur, playlistURL)
			material.NextSeg = next
			material.Duration = dur
			if time.Now().After(windowDue) {
				scheduleWindowASR()
				windowDue = time.Now().Add(model.LiveASRWindowDuration)
			}
		}

		w.logger.Info("开始 ffmpeg 录像分片（阻塞至流结束或出错；若无「首个录像分片」日志则卡在拉流）",
			zap.Uint("material_id", material.ID),
			zap.Int("start_index", startIndex),
			zap.String("work_dir", workDir),
			zap.String("m3u8_url", material.M3U8URL),
			zap.Duration("first_window_asr_after", model.LiveASRWindowDuration),
		)
		recStart := time.Now()
		recErr := w.ffmpeg.RecordHLSSegments(ctx, material.M3U8URL, workDir, startIndex, model.LiveSegmentDurationSec, onSeg)
		w.logger.Info("ffmpeg 录像会话结束",
			zap.Uint("material_id", material.ID),
			zap.Duration("elapsed", time.Since(recStart)),
			zap.Int64("next_seg", material.NextSeg),
			zap.Error(recErr),
		)
		// 等进行中的窗口结束后再追平，避免与调度协程并发 Transcribe（重复计费）。
		runWindowASRExclusive()
		if recErr != nil && ctx.Err() != nil {
			return recErr
		}
		if material.NextSeg > 0 {
			break
		}
		deadline := time.Now().Add(model.LiveConnectGrace)
		if material.ConnectDeadlineAt != nil {
			deadline = *material.ConnectDeadlineAt
		}
		if material.WaitDeadlineAt != nil {
			deadline = *material.WaitDeadlineAt
		}
		if !time.Now().Before(deadline) {
			msg := "未能录制到直播分片"
			_ = w.repo.MarkIngestFailed(ctx, material.ID, material.IngestEpoch, msg)
			return fmt.Errorf("%s", msg)
		}
		w.logger.Info("尚未写出分片，稍后重试录像", zap.Uint("material_id", material.ID))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(liveIngestProbeInterval):
		}
	}
	_ = w.repo.MarkEnding(ctx, material.ID, material.IngestEpoch)
	material.LiveStatus = model.LiveStatusEnding
	return w.finalizeRecording(ctx, material, resumeFrom)
}

func (w *liveIngestWorker) finalizeRecording(ctx context.Context, material *model.LiveMaterial, resumeFrom int64) error {
	latest, err := w.repo.GetByID(ctx, material.ID)
	if err == nil && latest != nil {
		material = latest
	}
	files := w.collectLocalSegmentFiles(ctx, material, resumeFrom)
	if len(files) == 0 {
		files = globLocalSegments(w.segmentDir(material))
	}
	if len(files) == 0 {
		msg := "关播时没有可用录像分片"
		_ = w.repo.MarkIngestFailed(ctx, material.ID, material.IngestEpoch, msg)
		return fmt.Errorf("%s", msg)
	}

	workDir := w.segmentDir(material)
	_ = os.MkdirAll(workDir, 0o755)
	finalPath := filepath.Join(workDir, "final.mp4")
	if err := w.ffmpeg.ConcatMediaFiles(ctx, files, finalPath); err != nil {
		return fmt.Errorf("合成最终 mp4 失败: %w", err)
	}
	if w.storage != nil {
		if _, err := w.storage.UploadFile(ctx, finalPath, liveingest.FinalObjectKey(material.RecordUUID)); err != nil {
			return fmt.Errorf("上传最终 mp4 失败: %w", err)
		}
	}
	dur := material.Duration
	if w.prober != nil {
		if tl, err := w.prober.ProbeMediaTimeline(ctx, finalPath); err == nil && tl.FormatDurationSec > 0 {
			dur = int64(tl.FormatDurationSec * 1000)
			material.Width, material.Height = tl.Width, tl.Height
		}
	}
	segURLs := w.collectSegmentURLs(ctx, material, files, resumeFrom)
	_, _ = w.publishPlaylist(ctx, material, segURLs, true, resumeFrom)

	if err := w.repo.MarkEnded(ctx, material.ID, material.IngestEpoch, dur); err != nil {
		return err
	}
	w.catchUpWindowASR(ctx, material)
	if err := w.finishASRPostprocess(ctx, material, dur); err != nil {
		w.logger.Warn("关播 ASR 后处理失败，将保留 ended+processing 供重试",
			zap.Uint("material_id", material.ID),
			zap.Error(err),
		)
		_ = os.Remove(finalPath)
		_ = os.RemoveAll(workDir)
		return err
	}
	_ = os.Remove(finalPath)
	_ = os.RemoveAll(workDir)
	return nil
}

func (w *liveIngestWorker) runWindowASR(ctx context.Context, material *model.LiveMaterial) error {
	if w.asrService == nil || w.audioPreparer == nil {
		w.logger.Warn("窗口 ASR 跳过：ASR 服务或音频预处理未配置",
			zap.Uint("material_id", material.ID),
			zap.Bool("asr_service", w.asrService != nil),
			zap.Bool("audio_preparer", w.audioPreparer != nil),
		)
		return nil
	}
	// 从库刷新游标/ASR，写回同一指针；epoch 仍用本场跟播抢占值，避免误跟到其它 worker。
	epoch := material.IngestEpoch
	if latest, err := w.repo.GetByID(ctx, material.ID); err == nil && latest != nil {
		material.ASRCursorMS = latest.ASRCursorMS
		material.LiveASR = latest.LiveASR
		if latest.Duration > material.Duration {
			material.Duration = latest.Duration
		}
	}
	cursor := material.ASRCursorMS
	if material.Duration <= cursor {
		w.logger.Info("窗口 ASR 跳过：尚无新录像可转写",
			zap.Uint("material_id", material.ID),
			zap.Int64("duration_ms", material.Duration),
			zap.Int64("asr_cursor_ms", cursor),
		)
		return nil
	}
	prober := w.prober
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	nominalStart := liveingest.WindowStartIndex(cursor, model.LiveSegmentDurationSec)
	startSeg, offsetMS, resErr := resolveWindowStartFast(ctx, prober, w.segmentDir(material), cursor, 1)
	if resErr != nil {
		w.logger.Info("窗口 ASR 跳过：无法定位起始分片",
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_cursor_ms", cursor),
			zap.Error(resErr),
		)
		return nil
	}
	files := globLocalSegmentsFrom(w.segmentDir(material), int(startSeg))
	if len(files) == 0 {
		w.logger.Info("窗口 ASR 跳过：起始分片之后无本地文件",
			zap.Uint("material_id", material.ID),
			zap.Int64("start_seg", startSeg),
			zap.Int64("asr_cursor_ms", cursor),
			zap.String("work_dir", w.segmentDir(material)),
		)
		return nil
	}
	// 限制：每窗最多送约一个窗口时长的分片。旧逻辑会把「游标→此刻」全部积压一次送去转写；
	// 若游标未推进，会变成 10+20+30… 分钟平方累加计费。
	maxSegs := maxWindowASRSegments()
	truncated := false
	if len(files) > maxSegs {
		files = files[:maxSegs]
		truncated = true
	}
	nominalWindowMS := int64(len(files)) * int64(model.LiveSegmentDurationSec) * 1000
	asrStart := time.Now()
	w.logger.Info("开始窗口 ASR",
		zap.Uint("material_id", material.ID),
		zap.Int64("asr_cursor_ms", cursor),
		zap.Int64("start_seg", startSeg),
		zap.Int64("offset_ms", offsetMS),
		zap.Int64("nominal_start_seg", nominalStart),
		zap.Int64("nominal_offset_ms", liveingest.WindowOffsetMS(cursor, model.LiveSegmentDurationSec)),
		zap.Int("segment_files", len(files)),
		zap.Bool("truncated_to_window", truncated),
		zap.Int64("est_bill_audio_ms", nominalWindowMS),
		zap.Int64("duration_ms", material.Duration),
		zap.Int64("ingest_epoch", epoch),
	)
	tmpMP3 := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_%d.mp3", cursor))
	concatTS := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_%d.ts", cursor))
	if err := w.ffmpeg.ConcatMediaFiles(ctx, files, concatTS); err != nil {
		w.logger.Warn("窗口 ASR 拼接分片失败", zap.Uint("material_id", material.ID), zap.Error(err))
		return err
	}
	defer os.Remove(concatTS)
	// 只探测拼接结果一次，避免对窗内上百个 ts 逐个 ffprobe。
	windowMediaMS := probeSegmentDurationMS(ctx, prober, concatTS, nominalWindowMS)
	if windowMediaMS <= 0 {
		windowMediaMS = nominalWindowMS
	}
	estBillMS := windowMediaMS
	if err := w.ffmpeg.ConvertToASRMP3(ctx, concatTS, tmpMP3); err != nil {
		w.logger.Warn("窗口 ASR 转 MP3 失败", zap.Uint("material_id", material.ID), zap.Error(err))
		return err
	}
	defer os.Remove(tmpMP3)
	if w.storage == nil {
		w.logger.Warn("窗口 ASR 跳过：对象存储未配置", zap.Uint("material_id", material.ID))
		return nil
	}
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, fmt.Sprintf("%s/live-asr-%d-%d.mp3", storage.SubDirTemp, material.ID, cursor))
	if err != nil {
		w.logger.Warn("窗口 ASR 上传音频失败", zap.Uint("material_id", material.ID), zap.Error(err))
		return err
	}
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		w.logger.Warn("窗口 ASR 识别失败，跳过本窗", zap.Uint("material_id", material.ID), zap.Error(err))
		return nil
	}
	merged, _, err := asr.MergeWindowASR(material.LiveASR, raw, offsetMS, cursor)
	if err != nil {
		w.logger.Warn("窗口 ASR 合并失败", zap.Uint("material_id", material.ID), zap.Error(err))
		return err
	}
	// 游标必须沿「真实分片时间轴」推进，不能用厂商 audio_info.duration：
	// 否则下一窗再按标称 6s 反推 startSeg 时会在边界跳段/重叠。
	newCursor := offsetMS + windowMediaMS
	if newCursor < cursor {
		newCursor = cursor
	}
	asrDur := asr.ParseDurationMs(raw)
	// 覆盖进度仍以录像时长为分母；跟播中最高 99，100 留给关播 Finalize。
	progress := int16(10)
	if material.Duration > 0 {
		progress = int16(20 + 79*newCursor/material.Duration)
		if progress > 99 {
			progress = 99
		}
		if newCursor >= material.Duration && progress < 99 {
			progress = 99
		}
	}
	if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, merged, newCursor, material.Duration, progress); err != nil {
		w.logger.Warn("窗口 ASR 写库失败（厂商侧已计费，下一窗可能重复）",
			zap.Uint("material_id", material.ID),
			zap.Int64("ingest_epoch", epoch),
			zap.Int64("est_bill_audio_ms", estBillMS),
			zap.Error(err),
		)
		return err
	}
	material.ASRCursorMS = newCursor
	material.LiveASR = merged
	material.ASRProgress = progress
	w.logger.Info("窗口 ASR 完成",
		zap.Uint("material_id", material.ID),
		zap.Duration("elapsed", time.Since(asrStart)),
		zap.Int64("offset_ms", offsetMS),
		zap.Int64("asr_cursor_ms", newCursor),
		zap.Int64("window_media_ms", windowMediaMS),
		zap.Int64("asr_vendor_duration_ms", asrDur),
		zap.Int64("est_bill_audio_ms", estBillMS),
		zap.Int16("asr_progress", progress),
	)
	return nil
}

func (w *liveIngestWorker) catchUpWindowASR(ctx context.Context, material *model.LiveMaterial) {
	const maxRounds = 128
	for i := 0; i < maxRounds; i++ {
		if ctx.Err() != nil {
			return
		}
		before := material.ASRCursorMS
		if err := w.runWindowASR(ctx, material); err != nil {
			w.logger.Warn("窗口 ASR 追平中断",
				zap.Uint("material_id", material.ID),
				zap.Int("round", i),
				zap.Error(err),
			)
			return
		}
		if material.ASRCursorMS <= before {
			return
		}
		if material.Duration > 0 && material.ASRCursorMS >= material.Duration {
			return
		}
	}
	w.logger.Warn("窗口 ASR 追平达到轮次上限",
		zap.Uint("material_id", material.ID),
		zap.Int64("asr_cursor_ms", material.ASRCursorMS),
		zap.Int64("duration_ms", material.Duration),
	)
}

// maxWindowASRSegments 单次送去转写的最大分片数（约等于一个调度窗口 + 1 片重叠余量）。
func maxWindowASRSegments() int {
	segSec := model.LiveSegmentDurationSec
	if segSec <= 0 {
		segSec = 6
	}
	windowSec := int(model.LiveASRWindowDuration / time.Second)
	if windowSec <= 0 {
		windowSec = 10 * 60
	}
	n := windowSec / segSec
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
		w.logger.Warn("关播 ASR LLM 后处理失败，回退本地段落并继续标记完成",
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

// localASRPostprocessFallback LLM 不可用时用本地规则生成段落，保证关播仍能到 ASR 完成态。
func localASRPostprocessFallback(liveASR string, durationMs int64) asrPostprocessResult {
	out := asrPostprocessResult{}
	utterances := asr.FormatUtterancesForAPI(liveASR)
	if len(utterances) == 0 {
		return out
	}
	ranges := buildParagraphRangesLocally(utterances)
	paragraphs, err := stitchASRParagraphs(utterances, ranges)
	if err != nil {
		return out
	}
	paragraphs, _ = enforceASRParagraphMaxRunes(paragraphs)
	finalizeASRParagraphTimeline(paragraphs, durationMs)
	out.Paragraphs = paragraphs
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

func (w *liveIngestWorker) publishPlaylist(ctx context.Context, material *model.LiveMaterial, segURLs map[int64]string, ended bool, resumeFrom int64) (string, error) {
	if w.storage == nil {
		return "", fmt.Errorf("对象存储未配置")
	}
	segURLs = w.completeSegmentURLs(ctx, material, segURLs, resumeFrom)
	keys := make([]int64, 0, len(segURLs))
	for k := range segURLs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	items := make([]liveingest.PlaylistItem, 0, len(keys))
	for _, k := range keys {
		items = append(items, liveingest.PlaylistItem{
			DurationSec: float64(model.LiveSegmentDurationSec),
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
