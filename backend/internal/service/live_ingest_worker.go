package service

import (
	"context"
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
	}
}

func (w *liveIngestWorker) Enqueue() {
	enqueueWake(w.wake, w.concurrency)
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
			w.logger.Warn("跟播任务结束",
				zap.Uint("material_id", material.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 执行一场跟播（无 4 小时硬超时）。
func (w *liveIngestWorker) Process(ctx context.Context, material *model.LiveMaterial) error {
	stopHB := w.startHeartbeat(ctx, material.ID, material.IngestEpoch)
	defer stopHB()

	switch material.LiveStatus {
	case model.LiveStatusWaiting, model.LiveStatusConnecting:
		if err := w.waitForMedia(ctx, material); err != nil {
			return err
		}
		return w.recordAndFinalize(ctx, material)
	case model.LiveStatusLive:
		return w.recordAndFinalize(ctx, material)
	case model.LiveStatusEnding:
		return w.finalizeRecording(ctx, material, material.NextSeg)
	default:
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
		if tl, err := w.prober.ProbeMediaTimeline(ctx, material.M3U8URL); err == nil {
			width, height = tl.Width, tl.Height
		}
	}
	if err := w.repo.MarkLiveStarted(ctx, material.ID, material.IngestEpoch, width, height); err != nil {
		w.logger.Warn("标记 live 失败", zap.Error(err))
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
	scheduleWindowASR := func() {
		asrMu.Lock()
		if asrBusy {
			asrMu.Unlock()
			return
		}
		asrBusy = true
		asrMu.Unlock()
		go func() {
			defer func() {
				asrMu.Lock()
				asrBusy = false
				asrMu.Unlock()
			}()
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

		onSeg := func(index int, path string) {
			url, err := w.uploadSegment(ctx, material, int64(index), path, resumeFrom)
			if err != nil {
				w.logger.Warn("上传分片失败", zap.Int("index", index), zap.Error(err))
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
				w.logger.Warn("发布播放列表失败", zap.Error(plErr))
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

		recErr := w.ffmpeg.RecordHLSSegments(ctx, material.M3U8URL, workDir, startIndex, model.LiveSegmentDurationSec, onSeg)
		_ = w.runWindowASR(ctx, material)
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
	_ = w.runWindowASR(ctx, material)
	if err := w.finishASRPostprocess(ctx, material, dur); err != nil {
		w.logger.Warn("关播 ASR 后处理失败", zap.Error(err))
	}
	_ = os.Remove(finalPath)
	_ = os.RemoveAll(workDir)
	return nil
}

func (w *liveIngestWorker) runWindowASR(ctx context.Context, material *model.LiveMaterial) error {
	if w.asrService == nil || w.audioPreparer == nil {
		return nil
	}
	latest, err := w.repo.GetByID(ctx, material.ID)
	if err == nil && latest != nil {
		material = latest
	}
	cursor := material.ASRCursorMS
	if material.Duration <= cursor {
		return nil
	}
	startSeg := liveingest.WindowStartIndex(cursor, model.LiveSegmentDurationSec)
	files := globLocalSegmentsFrom(w.segmentDir(material), int(startSeg))
	if len(files) == 0 {
		return nil
	}
	// 偏移必须等于「startSeg 之前各分片的真实总时长」。标称 6s×index 会随分片时长抖动累积漂移，
	// 表现为成片里前段字幕准、后段逐渐错位。
	offsetMS := w.measureSegmentPrefixDurationMS(ctx, material, startSeg)
	if offsetMS <= 0 {
		offsetMS = liveingest.WindowOffsetMS(cursor, model.LiveSegmentDurationSec)
	}
	tmpMP3 := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_%d.mp3", cursor))
	concatTS := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_%d.ts", cursor))
	if err := w.ffmpeg.ConcatMediaFiles(ctx, files, concatTS); err != nil {
		return err
	}
	defer os.Remove(concatTS)
	if err := w.ffmpeg.ConvertToASRMP3(ctx, concatTS, tmpMP3); err != nil {
		return err
	}
	defer os.Remove(tmpMP3)
	if w.storage == nil {
		return nil
	}
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, fmt.Sprintf("%s/live-asr-%d-%d.mp3", storage.SubDirTemp, material.ID, cursor))
	if err != nil {
		return err
	}
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		w.logger.Warn("窗口 ASR 失败，跳过本窗", zap.Error(err))
		return nil
	}
	merged, mergedDur, err := asr.MergeWindowASR(material.LiveASR, raw, offsetMS, cursor)
	if err != nil {
		return err
	}
	windowDur := asr.ParseDurationMs(raw)
	newCursor := offsetMS + windowDur
	if mergedDur > newCursor {
		newCursor = mergedDur
	}
	if newCursor < cursor {
		newCursor = cursor
	}
	// 覆盖进度仍以录像时长为分母，便于跟播 UI；cursor 用真实已转写终点。
	progress := int16(10)
	if material.Duration > 0 {
		progress = int16(20 + 70*newCursor/material.Duration)
		if progress > 99 {
			progress = 99
		}
	}
	return w.repo.AppendWindowASR(ctx, material.ID, material.IngestEpoch, merged, newCursor, material.Duration, progress)
}

// measureSegmentPrefixDurationMS 累加 startSeg 之前本地分片的真实时长，作为窗口 ASR 时间原点。
func (w *liveIngestWorker) measureSegmentPrefixDurationMS(ctx context.Context, material *model.LiveMaterial, startSeg int64) int64 {
	if startSeg <= 0 {
		return 0
	}
	prefix := make([]string, 0, startSeg)
	for _, f := range globLocalSegments(w.segmentDir(material)) {
		idx, ok := parseLocalSegIndex(f)
		if !ok || int64(idx) >= startSeg {
			continue
		}
		prefix = append(prefix, f)
	}
	return media.SumDurationMS(ctx, w.prober, prefix)
}

func (w *liveIngestWorker) finishASRPostprocess(ctx context.Context, material *model.LiveMaterial, duration int64) error {
	latest, err := w.repo.GetByID(ctx, material.ID)
	if err != nil {
		return err
	}
	post, err := runASRPostprocess(ctx, w.llmClient, latest.LiveASR, duration, nil, w.logger)
	if err != nil {
		return err
	}
	return w.repo.FinalizeASR(ctx, latest.ID, latest.IngestEpoch, latest.LiveASR, duration, latest.Width, latest.Height, post.Summaries, post.Paragraphs)
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
