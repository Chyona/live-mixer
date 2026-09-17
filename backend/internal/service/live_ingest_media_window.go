package service

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/utils"

	"go.uber.org/zap"
)

// minRecordedWindowMS 有效窗最短时长；更短视为空录。
const minRecordedWindowMS int64 = 1000

// masterAppendDurationSlackMS append 后探测时长相对「旧 master + 新窗」允许的偏差。
const masterAppendDurationSlackMS int64 = 1500

// masterJob 异步上传窗并拼接 master；按素材串行处理。
type masterJob struct {
	materialID uint
	winIdx     int
	localPath  string
	epoch      int64
	partial    bool
}

type materialMasterQueue struct {
	pending []masterJob
	running bool
	waiters []chan struct{}
}

// sealRecordedWindow 快路径：本地探测 + 登记 media_windows，投递异步 master 任务。
// 不上传 COS、不拼 master，以便录像循环在数秒内进入下一窗。
func (w *liveIngestWorker) sealRecordedWindow(
	ctx context.Context,
	material *model.LiveMaterial,
	winIdx int,
	mp4Path string,
	partial bool,
) error {
	if material == nil {
		return fmt.Errorf("material 为空")
	}
	st, err := os.Stat(mp4Path)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("媒体窗文件无效: %s", mp4Path)
	}

	durMS := int64(0)
	var winTL media.MediaTimeline
	var hasWinTL bool
	if w.prober != nil {
		if tl, perr := w.prober.ProbeMediaTimeline(ctx, mp4Path); perr == nil {
			winTL, hasWinTL = tl, true
			if tl.FormatDurationSec > 0 {
				if probed := int64(tl.FormatDurationSec * 1000); probed > 0 {
					durMS = probed
				}
			}
			if tl.Width > 0 {
				material.Width = tl.Width
			}
			if tl.Height > 0 {
				material.Height = tl.Height
			}
		}
	}
	if durMS < minRecordedWindowMS {
		return fmt.Errorf("媒体窗过短 dur_ms=%d path=%s", durMS, mp4Path)
	}

	windows := material.ParsedMediaWindows()
	startMS := windows.TotalReadyMS()
	objectKey := liveingest.WindowMP4ObjectKey(material.RecordUUID, winIdx)

	wmeta := model.MediaWindow{
		Index:     winIdx,
		StartMS:   startMS,
		EndMS:     startMS + durMS,
		DurMS:     durMS,
		URL:       "", // 异步 master 任务上传后回填
		ObjectKey: objectKey,
		SegStart:  int64(winIdx),
		SegEnd:    int64(winIdx + 1),
		Ready:     true,
	}
	windows = windows.Upsert(wmeta)
	nextIdx := int64(winIdx + 1)
	prevWindows := material.MediaWindows
	prevNextWindow := material.NextWindowSeg
	prevNextSeg := material.NextSeg
	material.MediaWindows = windows.Marshal()
	material.NextWindowSeg = nextIdx
	material.NextSeg = nextIdx

	if err := w.repo.SealMediaWindow(ctx, material.ID, material.IngestEpoch, material.MediaWindows, nextIdx); err != nil {
		material.MediaWindows = prevWindows
		material.NextWindowSeg = prevNextWindow
		material.NextSeg = prevNextSeg
		return fmt.Errorf("登记媒体窗失败: %w", err)
	}

	winEv := AlignDiagEvent{
		Event:       "window_sealed",
		WindowIndex: winIdx,
		WindowCount: windows.ReadyCount(),
		LocalPath:   mp4Path,
		ReadyMS:     startMS + durMS,
	}
	if hasWinTL {
		fillAlignTimeline(&winEv, winTL)
	}
	w.emitAlignDiag(material, winEv)

	w.logger.Info("媒体窗已登记，投递异步 master 拼接",
		zap.Uint("material_id", material.ID),
		zap.Int("window_index", winIdx),
		zap.Int64("window_dur_ms", durMS),
		zap.Bool("partial", partial),
		zap.String("local_path", mp4Path),
	)

	w.enqueueMasterJob(masterJob{
		materialID: material.ID,
		winIdx:     winIdx,
		localPath:  mp4Path,
		epoch:      material.IngestEpoch,
		partial:    partial,
	})
	return nil
}

// commitRecordedWindow 兼容旧名：快路径 seal + 异步 master。
func (w *liveIngestWorker) commitRecordedWindow(
	ctx context.Context,
	material *model.LiveMaterial,
	winIdx int,
	mp4Path string,
	partial bool,
) error {
	return w.sealRecordedWindow(ctx, material, winIdx, mp4Path, partial)
}

func (w *liveIngestWorker) enqueueMasterJob(job masterJob) {
	w.masterMu.Lock()
	if w.masterQueues == nil {
		w.masterQueues = make(map[uint]*materialMasterQueue)
	}
	q := w.masterQueues[job.materialID]
	if q == nil {
		q = &materialMasterQueue{}
		w.masterQueues[job.materialID] = q
	}
	// 同窗去重，避免续录重复投递。
	for _, pending := range q.pending {
		if pending.winIdx == job.winIdx && pending.epoch == job.epoch {
			w.masterMu.Unlock()
			return
		}
	}
	q.pending = append(q.pending, job)
	start := !q.running
	if start {
		q.running = true
	}
	w.masterMu.Unlock()
	if start {
		go w.runMasterQueue(job.materialID)
	}
}

// requeuePendingMasterBuilds 将 EndMS 超过当前 duration 的本地就绪窗重新投入 master 队列。
func (w *liveIngestWorker) requeuePendingMasterBuilds(material *model.LiveMaterial) {
	if material == nil {
		return
	}
	ready := material.ParsedMediaWindows().ReadyWindows()
	if len(ready) == 0 {
		return
	}
	winDir := filepath.Join(w.segmentDir(material), liveingest.WindowsDirName())
	for _, win := range ready {
		if material.Duration > 0 && win.EndMS <= material.Duration {
			continue
		}
		localPath := filepath.Join(winDir, liveingest.WindowMP4FileName(win.Index))
		if st, err := os.Stat(localPath); err != nil || st.Size() == 0 {
			continue
		}
		w.logger.Info("续录补投递异步 master",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", win.Index),
			zap.Int64("window_end_ms", win.EndMS),
			zap.Int64("master_duration_ms", material.Duration),
		)
		w.enqueueMasterJob(masterJob{
			materialID: material.ID,
			winIdx:     win.Index,
			localPath:  localPath,
			epoch:      material.IngestEpoch,
			partial:    false,
		})
	}
}

func (w *liveIngestWorker) runMasterQueue(materialID uint) {
	for {
		w.masterMu.Lock()
		q := w.masterQueues[materialID]
		if q == nil || len(q.pending) == 0 {
			if q != nil {
				q.running = false
				for _, ch := range q.waiters {
					close(ch)
				}
				q.waiters = nil
				if len(q.pending) == 0 {
					delete(w.masterQueues, materialID)
				}
			}
			w.masterMu.Unlock()
			return
		}
		job := q.pending[0]
		q.pending = q.pending[1:]
		w.masterMu.Unlock()

		ctx := context.Background()
		if err := w.processMasterJob(ctx, job); err != nil {
			w.logger.Warn("异步 master 任务失败，稍后重试",
				zap.Uint("material_id", job.materialID),
				zap.Int("window_index", job.winIdx),
				zap.Error(err),
			)
			select {
			case <-time.After(3 * time.Second):
			default:
			}
			w.masterMu.Lock()
			q = w.masterQueues[materialID]
			if q == nil {
				q = &materialMasterQueue{running: true}
				w.masterQueues[materialID] = q
			}
			q.pending = append([]masterJob{job}, q.pending...)
			w.masterMu.Unlock()
			time.Sleep(5 * time.Second)
		}
	}
}

// waitMasterQueueDrain 等待指定素材的异步 master 队列排空（Finalize 前调用）。
func (w *liveIngestWorker) waitMasterQueueDrain(ctx context.Context, materialID uint) error {
	for {
		w.masterMu.Lock()
		q := w.masterQueues[materialID]
		if q == nil || (!q.running && len(q.pending) == 0) {
			w.masterMu.Unlock()
			return nil
		}
		ch := make(chan struct{})
		q.waiters = append(q.waiters, ch)
		w.masterMu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		case <-time.After(30 * time.Second):
			// 继续轮询，避免永久卡死
		}
	}
}

func (w *liveIngestWorker) processMasterJob(ctx context.Context, job masterJob) error {
	material, err := w.repo.GetByID(ctx, job.materialID)
	if err != nil {
		return err
	}
	if material.IngestEpoch != job.epoch {
		w.logger.Info("跳过过期 master 任务",
			zap.Uint("material_id", job.materialID),
			zap.Int64("job_epoch", job.epoch),
			zap.Int64("current_epoch", material.IngestEpoch),
		)
		return nil
	}

	st, err := os.Stat(job.localPath)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("本地窗文件无效: %s", job.localPath)
	}

	windows := material.ParsedMediaWindows()
	var winMeta model.MediaWindow
	found := false
	for _, win := range windows {
		if win.Index == job.winIdx {
			winMeta = win
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("media_windows 中缺少窗 %d", job.winIdx)
	}

	objectKey := winMeta.ObjectKey
	if objectKey == "" {
		objectKey = liveingest.WindowMP4ObjectKey(material.RecordUUID, job.winIdx)
	}
	if strings.TrimSpace(winMeta.URL) == "" && w.storage != nil {
		mp4URL, uerr := w.storage.UploadFile(ctx, job.localPath, objectKey)
		if uerr != nil {
			return fmt.Errorf("上传媒体窗 mp4 失败: %w", uerr)
		}
		winMeta.URL = mp4URL
		winMeta.ObjectKey = objectKey
		windows = windows.Upsert(winMeta)
		material.MediaWindows = windows.Marshal()
	}

	prevDur := material.Duration
	masterPath := filepath.Join(w.segmentDir(material), liveingest.MasterMP4FileName())
	useAppend := job.winIdx > 0 && prevDur > 0
	if useAppend {
		if st, err := os.Stat(masterPath); err != nil || st.Size() == 0 {
			useAppend = false
		}
	}

	var buildErr error
	if useAppend {
		buildErr = w.appendMasterWithWindow(ctx, material, job.localPath, winMeta.DurMS, prevDur)
		if buildErr != nil {
			w.logger.Warn("append master 失败，回退全量重拼",
				zap.Uint("material_id", material.ID),
				zap.Int("window_index", job.winIdx),
				zap.Error(buildErr),
			)
			buildErr = w.rebuildMasterFromWindows(ctx, material)
		}
	} else {
		buildErr = w.rebuildMasterFromWindows(ctx, material)
	}
	if buildErr != nil {
		w.emitAlignDiag(material, AlignDiagEvent{
			Event:       "master_rebuild_failed",
			WindowIndex: job.winIdx,
			WindowCount: windows.ReadyCount(),
			Error:       buildErr.Error(),
		})
		return buildErr
	}

	w.logger.Info("异步 master 拼接完成",
		zap.Uint("material_id", material.ID),
		zap.Int("window_index", job.winIdx),
		zap.Bool("partial", job.partial),
		zap.Int64("master_ready_ms", material.Duration),
		zap.String("window_url", winMeta.URL),
		zap.String("master_url", material.LiveURL),
	)
	enqueueWake(w.asrWake, 1)
	return nil
}

// appendMasterWithWindow 用已有 master + 新窗 copy-append；时长异常则报错由调用方 fallback。
func (w *liveIngestWorker) appendMasterWithWindow(
	ctx context.Context,
	material *model.LiveMaterial,
	windowPath string,
	windowDurMS, prevMasterMS int64,
) error {
	workDir := w.segmentDir(material)
	masterPath := filepath.Join(workDir, liveingest.MasterMP4FileName())
	tmpPath := filepath.Join(workDir, "master_building.mp4")
	_ = os.Remove(tmpPath)

	w.logger.Info("开始 append 拼接主 MP4",
		zap.Uint("material_id", material.ID),
		zap.String("master", masterPath),
		zap.String("window", windowPath),
		zap.Int64("prev_ms", prevMasterMS),
		zap.Int64("window_ms", windowDurMS),
	)

	if err := w.ffmpeg.ConcatMediaFiles(ctx, []string{masterPath, windowPath}, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("append copy 拼接失败: %w", err)
	}

	wantMS := prevMasterMS + windowDurMS
	durMS := wantMS
	var masterTL media.MediaTimeline
	var hasMasterTL bool
	if w.prober != nil {
		if tl, err := w.prober.ProbeMediaTimeline(ctx, tmpPath); err == nil {
			masterTL, hasMasterTL = tl, true
			probedSec := tl.VideoDurationSec
			if probedSec <= 0 {
				probedSec = tl.FormatDurationSec
			}
			if probed := int64(probedSec * 1000); probed > 0 {
				durMS = probed
			}
			if tl.Width > 0 {
				material.Width = tl.Width
			}
			if tl.Height > 0 {
				material.Height = tl.Height
			}
		}
	}
	if math.Abs(float64(durMS-wantMS)) > float64(masterAppendDurationSlackMS) {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("append 时长异常 got=%d want≈%d slack=%d", durMS, wantMS, masterAppendDurationSlackMS)
	}

	return w.finishMasterBuild(ctx, material, tmpPath, masterPath, durMS, 2, hasMasterTL, masterTL)
}

// rebuildMasterFromWindows 将已就绪的前 N 个窗 MP4 拼成 master.mp4，写回 live_url / duration，并置 asr_due。
func (w *liveIngestWorker) rebuildMasterFromWindows(ctx context.Context, material *model.LiveMaterial) error {
	if material == nil {
		return nil
	}
	ready := material.ParsedMediaWindows().ReadyWindows()
	if len(ready) == 0 {
		return nil
	}

	workDir := w.segmentDir(material)
	winDir := filepath.Join(workDir, liveingest.WindowsDirName())
	if err := os.MkdirAll(winDir, 0o755); err != nil {
		return err
	}

	files := make([]string, 0, len(ready))
	for _, win := range ready {
		local := filepath.Join(winDir, liveingest.WindowMP4FileName(win.Index))
		if st, err := os.Stat(local); err != nil || st.Size() == 0 {
			url := strings.TrimSpace(win.URL)
			if url == "" {
				return fmt.Errorf("拼接主片缺少窗 %d 本地文件且无 URL", win.Index)
			}
			if _, err := utils.DownloadFileWithConfigContext(ctx, url, local, utils.DownloadConfig{}); err != nil {
				_ = os.Remove(local)
				return fmt.Errorf("下载窗 %d 失败: %w", win.Index, err)
			}
		}
		files = append(files, local)
	}

	mp4Path := filepath.Join(workDir, liveingest.MasterMP4FileName())
	tmpPath := filepath.Join(workDir, "master_building.mp4")
	_ = os.Remove(tmpPath)

	w.logger.Info("开始拼接前 N 窗主 MP4",
		zap.Uint("material_id", material.ID),
		zap.Int("window_count", len(files)),
		zap.String("tmp", tmpPath),
	)

	// 先写临时文件：上传失败时不覆盖正式 master，避免 ASR 读到坏片而预览仍用旧 CDN。
	if len(files) == 1 {
		if err := copyFile(files[0], tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("复制单窗为主片失败: %w", err)
		}
	} else {
		if err := w.ffmpeg.ConcatMediaFiles(ctx, files, tmpPath); err != nil {
			w.logger.Warn("copy 拼接主片失败，回退连续时间轴重编码",
				zap.Uint("material_id", material.ID),
				zap.Int("window_count", len(files)),
				zap.Error(err),
			)
			_ = os.Remove(tmpPath)
			if err2 := w.ffmpeg.ConcatMP4ContinuousTimeline(ctx, files, tmpPath); err2 != nil {
				_ = os.Remove(tmpPath)
				return fmt.Errorf("拼接主 MP4 失败: copy=%v continuous=%w", err, err2)
			}
		}
	}

	durMS := ready.TotalReadyMS()
	var masterTL media.MediaTimeline
	var hasMasterTL bool
	if w.prober != nil {
		if tl, err := w.prober.ProbeMediaTimeline(ctx, tmpPath); err == nil {
			masterTL, hasMasterTL = tl, true
			probedSec := tl.VideoDurationSec
			if probedSec <= 0 {
				probedSec = tl.FormatDurationSec
			}
			if probed := int64(probedSec * 1000); probed > 0 {
				durMS = probed
			}
			if tl.Width > 0 {
				material.Width = tl.Width
			}
			if tl.Height > 0 {
				material.Height = tl.Height
			}
		}
	}

	return w.finishMasterBuild(ctx, material, tmpPath, mp4Path, durMS, len(files), hasMasterTL, masterTL)
}

func (w *liveIngestWorker) finishMasterBuild(
	ctx context.Context,
	material *model.LiveMaterial,
	tmpPath, mp4Path string,
	durMS int64,
	windowCount int,
	hasMasterTL bool,
	masterTL media.MediaTimeline,
) error {
	objectKey := liveingest.MasterObjectKey(material.RecordUUID)
	var mp4URL string
	uploadOK := false
	if w.storage != nil {
		var err error
		mp4URL, err = w.storage.UploadFile(ctx, tmpPath, objectKey)
		if err != nil {
			_ = os.Remove(tmpPath)
			fail := false
			w.emitAlignDiag(material, AlignDiagEvent{
				Event:       "master_upload_failed",
				WindowCount: windowCount,
				LocalPath:   tmpPath,
				UploadOK:    &fail,
				Error:       err.Error(),
				ReadyMS:     durMS,
			})
			return fmt.Errorf("上传主 MP4 失败: %w", err)
		}
		uploadOK = true
	}
	if strings.TrimSpace(mp4URL) == "" {
		mp4URL = strings.TrimSpace(material.LiveURL)
	}

	_ = os.Remove(mp4Path)
	if err := os.Rename(tmpPath, mp4Path); err != nil {
		if copyErr := copyFile(tmpPath, mp4Path); copyErr != nil {
			return fmt.Errorf("替换主 MP4 失败: rename=%v copy=%w", err, copyErr)
		}
		_ = os.Remove(tmpPath)
	}

	material.Duration = durMS
	if mp4URL != "" {
		material.LiveURL = mp4URL
	}
	material.ASRDue = true

	if err := w.repo.CommitMasterMP4(
		ctx,
		material.ID,
		material.IngestEpoch,
		material.MediaWindows,
		material.NextWindowSeg,
		durMS,
		mp4URL,
		true,
	); err != nil {
		return fmt.Errorf("写回主 MP4 元数据失败: %w", err)
	}
	material.NextSeg = material.NextWindowSeg

	ok := uploadOK
	ev := AlignDiagEvent{
		Event:          "master_ready",
		WindowCount:    windowCount,
		LocalPath:      mp4Path,
		URL:            mp4URL,
		ReadyMS:        durMS,
		MasterReplaced: true,
		UploadOK:       &ok,
	}
	if hasMasterTL {
		fillAlignTimeline(&ev, masterTL)
	}
	w.emitAlignDiag(material, ev)

	w.logger.Info("主 MP4 已就绪",
		zap.Uint("material_id", material.ID),
		zap.Int("window_count", windowCount),
		zap.Int64("ready_ms", durMS),
		zap.String("mp4_url", mp4URL),
	)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// snapshotMasterMP4 复制正式 master 到临时快照，供 ASR 抽音，避免与 rename 竞态。
func (w *liveIngestWorker) snapshotMasterMP4(src string, materialID uint, readyMS int64) (string, error) {
	dst := filepath.Join(filepath.Dir(src), fmt.Sprintf("asr_master_snap_%d_%d.mp4", materialID, readyMS))
	_ = os.Remove(dst)
	if err := copyFile(src, dst); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// resolveMasterMP4Path 优先本地 master.mp4，否则按 live_url 下载。
func (w *liveIngestWorker) resolveMasterMP4Path(ctx context.Context, material *model.LiveMaterial) (string, error) {
	workDir := w.segmentDir(material)
	local := filepath.Join(workDir, liveingest.MasterMP4FileName())
	if st, err := os.Stat(local); err == nil && st.Size() > 0 {
		return local, nil
	}
	url := ""
	if material != nil {
		url = material.MasterMP4URL()
		if url == "" {
			url = strings.TrimSpace(material.LiveURL)
			if model.IsProbablyM3U8URL(url) {
				url = ""
			}
		}
	}
	if url == "" {
		return "", fmt.Errorf("主 MP4 尚未就绪")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	if _, err := utils.DownloadFileWithConfigContext(ctx, url, local, utils.DownloadConfig{}); err != nil {
		_ = os.Remove(local)
		return "", fmt.Errorf("下载主 MP4 失败: %w", err)
	}
	return local, nil
}

// probeLocalWindowDurationMS 探测本地窗文件时长；失败返回 0。
func (w *liveIngestWorker) probeLocalWindowDurationMS(ctx context.Context, path string) int64 {
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return 0
	}
	if w.prober == nil {
		return 0
	}
	tl, err := w.prober.ProbeMediaTimeline(ctx, path)
	if err != nil {
		return 0
	}
	sec := tl.FormatDurationSec
	if sec <= 0 {
		sec = tl.VideoDurationSec
	}
	if sec <= 0 {
		return 0
	}
	return int64(sec * 1000)
}

// nextWindowIndex 下一要录的窗下标（= 已就绪窗数，并与 NextWindowSeg 取较大者）。
func nextWindowIndex(material *model.LiveMaterial) int {
	if material == nil {
		return 0
	}
	ready := material.ParsedMediaWindows().ReadyCount()
	idx := ready
	if int(material.NextWindowSeg) > idx {
		idx = int(material.NextWindowSeg)
	}
	if int(material.NextSeg) > idx {
		idx = int(material.NextSeg)
	}
	return idx
}
