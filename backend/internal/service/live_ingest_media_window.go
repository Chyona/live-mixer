package service

import (
	"context"
	"errors"
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

// asrLiveThrottleEveryNWindows 直播中每隔 N 个媒体窗才置 asr_due，降低全量 ASR 与录像抢资源。
const asrLiveThrottleEveryNWindows = 3

// asrLiveMaxMasterLagWindows 直播中若已 seal 媒体领先 master.duration 超过该窗数，推迟 ASR。
const asrLiveMaxMasterLagWindows = 1

// asrLiveMasterLagSlackMS 判定「master 落后几个窗」时的亚秒容差。
// 真实媒体窗落盘时长会比标称 600000ms 多出几十毫秒（实测 600016–600048ms），
// 不补偿的话「恰好落后 1 个窗」这个直播稳态会被误判成落后 2 个窗，导致 ASR 被永久推迟。
const asrLiveMasterLagSlackMS int64 = 2000

// maxUnrecoverableWindowAttempts 窗文件确定找不到时的尝试次数，超过后放弃该任务，避免堵住 Finalize。
const maxUnrecoverableWindowAttempts = 3

// errWindowUnrecoverable 当前目录、旧代数目录、URL、对象键都无法得到窗文件。
var errWindowUnrecoverable = errors.New("窗文件不可恢复")

// windowUnrecoverableError 缺窗且无法找回。网络或上传失败不是这类错误。
type windowUnrecoverableError struct {
	index int
}

func (e *windowUnrecoverableError) Error() string {
	if e == nil {
		return errWindowUnrecoverable.Error()
	}
	return fmt.Sprintf("拼接主片缺少窗 %d 本地文件且无 URL", e.index)
}

func (e *windowUnrecoverableError) Is(target error) bool {
	return target == errWindowUnrecoverable
}

func abandonUnrecoverableWindowJob(err error, tries int) bool {
	return errors.Is(err, errWindowUnrecoverable) && tries >= maxUnrecoverableWindowAttempts
}

// masterJob 异步上传窗并拼接 master；按素材串行处理。
type masterJob struct {
	materialID uint
	winIdx     int
	localPath  string
	epoch      int64
	partial    bool
	// unrecoverableTries 连续「窗文件不可恢复」次数；网络错误不计入。
	unrecoverableTries int
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
		liveRecordFields("seal_ok", material.ID,
			zap.Int("window_index", winIdx),
			zap.Int64("window_dur_ms", durMS),
			zap.Int64("ready_ms", startMS+durMS),
			zap.Bool("partial", partial),
			zap.Int("window_count", windows.ReadyCount()),
			zap.String("local_path", mp4Path),
		)...,
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
			pendingCount := len(q.pending)
			running := q.running
			w.masterMu.Unlock()
			w.logger.Info("异步 master 任务去重跳过",
				liveRecordFields("master_enqueue_dedup", job.materialID,
					zap.Int("window_index", job.winIdx),
					zap.Int64("epoch", job.epoch),
					zap.Int("pending", pendingCount),
					zap.Bool("running", running),
				)...,
			)
			return
		}
	}
	q.pending = append(q.pending, job)
	pendingCount := len(q.pending)
	start := !q.running
	if start {
		q.running = true
	}
	w.masterMu.Unlock()
	w.logger.Info("异步 master 任务已入队",
		liveRecordFields("master_enqueue", job.materialID,
			zap.Int("window_index", job.winIdx),
			zap.Bool("partial", job.partial),
			zap.Int("pending", pendingCount),
			zap.Bool("start_runner", start),
			zap.String("local_path", job.localPath),
		)...,
	)
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
		if !nonEmptyFile(localPath) {
			resolved, rerr := w.resolveWindowFile(context.Background(), material, win)
			if rerr != nil {
				w.logger.Warn("续录补投递跳过：窗文件不可用",
					liveRecordFields("requeue_skip_missing_file", material.ID,
						zap.Int("window_index", win.Index),
						zap.String("local_path", localPath),
						zap.Error(rerr),
					)...,
				)
				continue
			}
			localPath = resolved
		}
		w.logger.Info("续录补投递异步 master",
			liveRecordFields("requeue_master_job", material.ID,
				zap.Int("window_index", win.Index),
				zap.Int64("window_end_ms", win.EndMS),
				zap.Int64("master_duration_ms", material.Duration),
			)...,
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

func nonEmptyFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

func downloadStatusMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "status code: 404") || strings.Contains(msg, "status code: 410")
}

func (w *liveIngestWorker) ingestEpochDir(material *model.LiveMaterial, epoch int64) string {
	root := ""
	if w != nil {
		root = w.web.RootDir
	}
	if root == "" {
		root = os.TempDir()
	}
	if material == nil {
		return ""
	}
	return liveingest.LiveIngestSegmentDir(root, material.ID, epoch)
}

func windowMP4Path(epochDir string, index int) string {
	return filepath.Join(epochDir, liveingest.WindowsDirName(), liveingest.WindowMP4FileName(index))
}

// masterOmitsEarlierWindow 更早的已封窗还没进入已提交 master，不能 append，否则主片会缺段。
func masterOmitsEarlierWindow(material *model.LiveMaterial, winIdx int, masterDur int64) bool {
	if material == nil || winIdx <= 0 {
		return false
	}
	for _, win := range material.ParsedMediaWindows().ReadyWindows() {
		if win.Index >= winIdx {
			continue
		}
		if win.EndMS > masterDur+masterAppendDurationSlackMS {
			return true
		}
	}
	return false
}

// persistWindowMeta 合并写回单个窗的 URL / ObjectKey，不改 duration。
func (w *liveIngestWorker) persistWindowMeta(ctx context.Context, material *model.LiveMaterial, win model.MediaWindow) error {
	if material == nil {
		return fmt.Errorf("material 为空")
	}
	windows := material.ParsedMediaWindows().Upsert(win)
	material.MediaWindows = windows.Marshal()
	if err := w.repo.SealMediaWindow(ctx, material.ID, material.IngestEpoch, material.MediaWindows, material.NextWindowSeg); err != nil {
		return err
	}
	if latest, err := w.repo.GetByID(ctx, material.ID); err == nil && latest != nil {
		material.MediaWindows = latest.MediaWindows
		material.NextWindowSeg = latest.NextWindowSeg
		material.NextSeg = latest.NextSeg
	}
	return nil
}

// uploadAndPersistWindow 把已有本地窗补传到对象存储。上传失败不阻断本次拼接。
func (w *liveIngestWorker) uploadAndPersistWindow(ctx context.Context, material *model.LiveMaterial, win model.MediaWindow, localPath string) {
	if w == nil || w.storage == nil || material == nil || strings.TrimSpace(win.URL) != "" {
		return
	}
	objectKey := win.ObjectKey
	if objectKey == "" {
		objectKey = liveingest.WindowMP4ObjectKey(material.RecordUUID, win.Index)
	}
	mp4URL, err := w.storage.UploadFile(ctx, localPath, objectKey)
	if err != nil {
		w.logger.Warn("补传媒体窗失败，继续使用本地文件",
			liveRecordFields("master_upload_window_fail", material.ID,
				zap.Int("window_index", win.Index),
				zap.Error(err),
			)...,
		)
		return
	}
	win.URL = mp4URL
	win.ObjectKey = objectKey
	if err := w.persistWindowMeta(ctx, material, win); err != nil {
		w.logger.Warn("写回媒体窗 URL 失败",
			liveRecordFields("master_window_url_commit_fail", material.ID,
				zap.Int("window_index", win.Index),
				zap.Error(err),
			)...,
		)
	}
}

// resolveWindowFile 按当前目录、更早代数目录、URL、对象键找回窗 MP4。
// 找到旧目录文件时复制到当前代数并补上传。对象不存在返回 errWindowUnrecoverable；下载网络错误原样返回以便重试。
func (w *liveIngestWorker) resolveWindowFile(ctx context.Context, material *model.LiveMaterial, win model.MediaWindow) (string, error) {
	if material == nil {
		return "", &windowUnrecoverableError{index: win.Index}
	}
	dest := windowMP4Path(w.segmentDir(material), win.Index)
	if nonEmptyFile(dest) {
		return dest, nil
	}
	for epoch := material.IngestEpoch - 1; epoch >= 1; epoch-- {
		old := windowMP4Path(w.ingestEpochDir(material, epoch), win.Index)
		if !nonEmptyFile(old) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := copyFile(old, dest); err != nil {
			w.logger.Warn("复制旧代数窗文件失败，改用原路径",
				liveRecordFields("window_copy_old_epoch_fail", material.ID,
					zap.Int("window_index", win.Index),
					zap.Int64("epoch", epoch),
					zap.Error(err),
				)...,
			)
			w.uploadAndPersistWindow(ctx, material, win, old)
			return old, nil
		}
		w.uploadAndPersistWindow(ctx, material, win, dest)
		return dest, nil
	}

	url := strings.TrimSpace(win.URL)
	fromObjectKey := false
	if url == "" {
		objectKey := strings.TrimSpace(win.ObjectKey)
		if w.storage != nil && objectKey != "" {
			url = strings.TrimSpace(w.storage.PublicURL(objectKey))
			fromObjectKey = url != ""
		}
	}
	if url == "" {
		return "", &windowUnrecoverableError{index: win.Index}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if _, err := utils.DownloadFileWithConfigContext(ctx, url, dest, utils.DownloadConfig{}); err != nil {
		_ = os.Remove(dest)
		if downloadStatusMissing(err) {
			return "", &windowUnrecoverableError{index: win.Index}
		}
		return "", fmt.Errorf("下载窗 %d 失败: %w", win.Index, err)
	}
	if !nonEmptyFile(dest) {
		_ = os.Remove(dest)
		return "", &windowUnrecoverableError{index: win.Index}
	}
	if fromObjectKey && strings.TrimSpace(win.URL) == "" {
		win.URL = url
		win.ObjectKey = strings.TrimSpace(win.ObjectKey)
		if err := w.persistWindowMeta(ctx, material, win); err != nil && w.logger != nil {
			w.logger.Warn("写回对象键下载的窗 URL 失败",
				liveRecordFields("master_window_url_commit_fail", material.ID,
					zap.Int("window_index", win.Index),
					zap.Error(err),
				)...,
			)
		}
	}
	return dest, nil
}

func (w *liveIngestWorker) runMasterQueue(materialID uint) {
	w.logger.Info("异步 master 队列 runner 启动",
		liveRecordFields("master_runner_start", materialID)...,
	)
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
			w.logger.Info("异步 master 队列 runner 退出",
				liveRecordFields("master_runner_exit", materialID)...,
			)
			return
		}
		job := q.pending[0]
		q.pending = q.pending[1:]
		remain := len(q.pending)
		w.masterMu.Unlock()

		w.logger.Info("异步 master 开始处理任务",
			liveRecordFields("master_job_start", job.materialID,
				zap.Int("window_index", job.winIdx),
				zap.Bool("partial", job.partial),
				zap.Int("pending_remain", remain),
			)...,
		)
		ctx := context.Background()
		if err := w.processMasterJob(ctx, job); err != nil {
			w.logger.Warn("异步 master 任务失败，稍后重试",
				liveRecordFields("master_job_fail", job.materialID,
					zap.Int("window_index", job.winIdx),
					zap.Error(err),
				)...,
			)
			if errors.Is(err, errWindowUnrecoverable) {
				job.unrecoverableTries++
			}
			if abandonUnrecoverableWindowJob(err, job.unrecoverableTries) {
				w.logger.Error("窗文件不可恢复，放弃该 master 任务",
					liveRecordFields("master_job_abandon", job.materialID,
						zap.Int("window_index", job.winIdx),
						zap.Int("tries", job.unrecoverableTries),
						zap.Error(err),
					)...,
				)
				continue
			}
			time.Sleep(5 * time.Second)
			w.masterMu.Lock()
			q = w.masterQueues[materialID]
			if q == nil {
				q = &materialMasterQueue{running: true}
				w.masterQueues[materialID] = q
			}
			q.pending = append([]masterJob{job}, q.pending...)
			w.masterMu.Unlock()
		}
	}
}

// waitMasterQueueDrain 等待指定素材的异步 master 队列排空（Finalize 前调用）。
func (w *liveIngestWorker) waitMasterQueueDrain(ctx context.Context, materialID uint) error {
	waitRound := 0
	for {
		w.masterMu.Lock()
		q := w.masterQueues[materialID]
		if q == nil || (!q.running && len(q.pending) == 0) {
			w.masterMu.Unlock()
			return nil
		}
		pending := len(q.pending)
		running := q.running
		ch := make(chan struct{})
		q.waiters = append(q.waiters, ch)
		w.masterMu.Unlock()

		waitRound++
		if waitRound == 1 || waitRound%2 == 0 {
			w.logger.Info("等待异步 master 队列排空",
				liveRecordFields("master_drain_wait", materialID,
					zap.Int("wait_round", waitRound),
					zap.Int("pending", pending),
					zap.Bool("running", running),
				)...,
			)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		case <-time.After(30 * time.Second):
			w.logger.Warn("等待 master 队列超时轮询续等",
				liveRecordFields("master_drain_timeout", materialID,
					zap.Int("wait_round", waitRound),
					zap.Int("pending", w.masterQueuePending(materialID)),
				)...,
			)
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
		// seal 与读库短暂不同步时再读一次；仍缺则失败重试。
		w.logger.Warn("master 任务首次未找到媒体窗，重读库",
			liveRecordFields("master_window_missing_retry", job.materialID,
				zap.Int("window_index", job.winIdx),
			)...,
		)
		if latest, gerr := w.repo.GetByID(ctx, job.materialID); gerr == nil && latest != nil {
			material = latest
			windows = material.ParsedMediaWindows()
			for _, win := range windows {
				if win.Index == job.winIdx {
					winMeta = win
					found = true
					break
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("media_windows 中缺少窗 %d", job.winIdx)
	}

	localPath := job.localPath
	if !nonEmptyFile(localPath) {
		resolved, rerr := w.resolveWindowFile(ctx, material, winMeta)
		if rerr != nil {
			return rerr
		}
		localPath = resolved
	}

	objectKey := winMeta.ObjectKey
	if objectKey == "" {
		objectKey = liveingest.WindowMP4ObjectKey(material.RecordUUID, job.winIdx)
	}
	if strings.TrimSpace(winMeta.URL) == "" && w.storage != nil {
		w.logger.Info("开始上传媒体窗 MP4",
			liveRecordFields("master_upload_window", job.materialID,
				zap.Int("window_index", job.winIdx),
				zap.String("object_key", objectKey),
			)...,
		)
		mp4URL, uerr := w.storage.UploadFile(ctx, localPath, objectKey)
		if uerr != nil {
			w.logger.Error("上传媒体窗 mp4 失败",
				liveRecordFields("master_upload_window_fail", job.materialID,
					zap.Int("window_index", job.winIdx),
					zap.Error(uerr),
				)...,
			)
			return fmt.Errorf("上传媒体窗 mp4 失败: %w", uerr)
		}
		winMeta.URL = mp4URL
		winMeta.ObjectKey = objectKey
		if err := w.persistWindowMeta(ctx, material, winMeta); err != nil {
			return fmt.Errorf("写回媒体窗 URL 失败: %w", err)
		}
	}

	asrDue := liveASRDueForMasterJob(material, job)

	prevDur := material.Duration
	masterPath := filepath.Join(w.segmentDir(material), liveingest.MasterMP4FileName())
	useAppend := job.winIdx > 0 && prevDur > 0
	if useAppend && masterOmitsEarlierWindow(material, job.winIdx, prevDur) {
		w.logger.Info("更早的窗尚未进入 master，改用全量重拼",
			liveRecordFields("master_append_fallback_gap", job.materialID,
				zap.Int("window_index", job.winIdx),
				zap.Int64("prev_ms", prevDur),
			)...,
		)
		useAppend = false
	}
	if useAppend {
		if st, err := os.Stat(masterPath); err != nil || st.Size() == 0 {
			w.logger.Info("本地 master 缺失，改用全量重拼",
				liveRecordFields("master_append_fallback_missing", job.materialID,
					zap.Int("window_index", job.winIdx),
					zap.Error(err),
				)...,
			)
			useAppend = false
		}
	}

	var buildErr error
	if useAppend {
		w.logger.Info("选用 append 拼接 master",
			liveRecordFields("master_build_append", job.materialID,
				zap.Int("window_index", job.winIdx),
				zap.Int64("prev_ms", prevDur),
				zap.Bool("asr_due", asrDue),
			)...,
		)
		buildErr = w.appendMasterWithWindow(ctx, material, localPath, winMeta.DurMS, prevDur, asrDue)
		if buildErr != nil {
			w.logger.Warn("append master 失败，回退全量重拼",
				liveRecordFields("master_append_fail", job.materialID,
					zap.Int("window_index", job.winIdx),
					zap.Error(buildErr),
				)...,
			)
			buildErr = w.rebuildMasterFromWindows(ctx, material, asrDue)
		}
	} else {
		w.logger.Info("选用全量重拼 master",
			liveRecordFields("master_build_rebuild", job.materialID,
				zap.Int("window_index", job.winIdx),
				zap.Bool("asr_due", asrDue),
			)...,
		)
		buildErr = w.rebuildMasterFromWindows(ctx, material, asrDue)
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
		liveRecordFields("master_job_ok", material.ID,
			zap.Int("window_index", job.winIdx),
			zap.Bool("partial", job.partial),
			zap.Bool("asr_due", asrDue),
			zap.Int64("master_ready_ms", material.Duration),
			zap.String("window_url", winMeta.URL),
			zap.String("master_url", material.LiveURL),
		)...,
	)
	if asrDue {
		enqueueWake(w.asrWake, 1)
	}
	return nil
}

// liveASRDueForMasterJob 直播中按窗节流置 asr_due；首窗必跑以便尽快离开「等待解析」。
// 关播尾窗 / 非 live 始终置位。
func liveASRDueForMasterJob(material *model.LiveMaterial, job masterJob) bool {
	if job.partial {
		return true
	}
	if material == nil || material.LiveStatus != model.LiveStatusLive {
		return true
	}
	// 首窗始终触发，避免直播前 20–30 分钟 UI 一直停在「等待解析」。
	if job.winIdx == 0 {
		return true
	}
	every := asrLiveThrottleEveryNWindows
	if every <= 1 {
		return true
	}
	// 之后每 N 窗：winIdx 2/5/8…（第 3/6/9 窗）
	return (job.winIdx+1)%every == 0
}

// shouldDeferLiveASR 直播中 master 明显落后于已 seal 窗，或仍有待拼 master 任务时推迟全量 ASR。
func (w *liveIngestWorker) shouldDeferLiveASR(material *model.LiveMaterial) bool {
	if material == nil || material.LiveStatus != model.LiveStatusLive {
		return false
	}
	if w.masterQueuePending(material.ID) > 0 {
		return true
	}
	windowMS := material.EffectiveMediaWindowMS()
	if windowMS <= 0 {
		windowMS = int64(model.LiveMediaWindowDuration / time.Millisecond)
	}
	sealedMS := material.ParsedMediaWindows().TotalReadyMS()
	lag := sealedMS - material.Duration
	// 按整窗判定并留亚秒容差：见 asrLiveMasterLagSlackMS。
	maxLag := int64(asrLiveMaxMasterLagWindows)*windowMS + asrLiveMasterLagSlackMS
	return lag > maxLag
}

func (w *liveIngestWorker) masterQueuePending(materialID uint) int {
	w.masterMu.Lock()
	defer w.masterMu.Unlock()
	q := w.masterQueues[materialID]
	if q == nil {
		return 0
	}
	return len(q.pending)
}

// appendMasterWithWindow 用已有 master + 新窗 copy-append；时长异常则报错由调用方 fallback。
func (w *liveIngestWorker) appendMasterWithWindow(
	ctx context.Context,
	material *model.LiveMaterial,
	windowPath string,
	windowDurMS, prevMasterMS int64,
	asrDue bool,
) error {
	workDir := w.segmentDir(material)
	masterPath := filepath.Join(workDir, liveingest.MasterMP4FileName())
	tmpPath := filepath.Join(workDir, "master_building.mp4")
	_ = os.Remove(tmpPath)

	w.logger.Info("开始 append 拼接主 MP4",
		liveRecordFields("master_append_start", material.ID,
			zap.String("master", masterPath),
			zap.String("window", windowPath),
			zap.Int64("prev_ms", prevMasterMS),
			zap.Int64("window_ms", windowDurMS),
		)...,
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

	return w.finishMasterBuild(ctx, material, tmpPath, masterPath, durMS, 2, hasMasterTL, masterTL, asrDue)
}

// rebuildMasterFromWindows 将已就绪的前 N 个窗 MP4 拼成 master.mp4，写回 live_url / duration，并按需置 asr_due。
func (w *liveIngestWorker) rebuildMasterFromWindows(ctx context.Context, material *model.LiveMaterial, asrDue bool) error {
	if material == nil {
		w.logger.Warn("全量重拼 master 跳过：material 为空",
			zap.String("pipeline", liveRecordPipeline),
			zap.String("stage", "master_rebuild_skip_nil"),
		)
		return nil
	}
	ready := material.ParsedMediaWindows().ReadyWindows()
	if len(ready) == 0 {
		w.logger.Warn("全量重拼 master 跳过：无就绪媒体窗",
			liveRecordFields("master_rebuild_skip_empty", material.ID)...,
		)
		return nil
	}

	workDir := w.segmentDir(material)
	winDir := filepath.Join(workDir, liveingest.WindowsDirName())
	if err := os.MkdirAll(winDir, 0o755); err != nil {
		return err
	}

	files := make([]string, 0, len(ready))
	for _, win := range ready {
		local, err := w.resolveWindowFile(ctx, material, win)
		if err != nil {
			return err
		}
		files = append(files, local)
	}

	mp4Path := filepath.Join(workDir, liveingest.MasterMP4FileName())
	tmpPath := filepath.Join(workDir, "master_building.mp4")
	_ = os.Remove(tmpPath)

	w.logger.Info("开始拼接前 N 窗主 MP4",
		liveRecordFields("master_rebuild_start", material.ID,
			zap.Int("window_count", len(files)),
			zap.String("tmp", tmpPath),
		)...,
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
				liveRecordFields("master_rebuild_copy_fail", material.ID,
					zap.Int("window_count", len(files)),
					zap.Error(err),
				)...,
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

	return w.finishMasterBuild(ctx, material, tmpPath, mp4Path, durMS, len(files), hasMasterTL, masterTL, asrDue)
}

func (w *liveIngestWorker) finishMasterBuild(
	ctx context.Context,
	material *model.LiveMaterial,
	tmpPath, mp4Path string,
	durMS int64,
	windowCount int,
	hasMasterTL bool,
	masterTL media.MediaTimeline,
	asrDue bool,
) error {
	objectKey := liveingest.MasterObjectKey(material.RecordUUID)
	var mp4URL string
	uploadOK := false
	if w.storage != nil {
		w.logger.Info("开始上传主 MP4",
			liveRecordFields("master_upload_start", material.ID,
				zap.Int("window_count", windowCount),
				zap.String("object_key", objectKey),
				zap.Int64("duration_ms", durMS),
			)...,
		)
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
			w.logger.Error("上传主 MP4 失败",
				liveRecordFields("master_upload_fail", material.ID,
					zap.Int("window_count", windowCount),
					zap.Error(err),
				)...,
			)
			return fmt.Errorf("上传主 MP4 失败: %w", err)
		}
		uploadOK = true
	} else {
		w.logger.Warn("存储未配置，跳过主 MP4 上传，沿用已有 live_url",
			liveRecordFields("master_upload_skip", material.ID,
				zap.Int("window_count", windowCount),
			)...,
		)
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
	material.ASRDue = material.ASRDue || asrDue

	if err := w.repo.CommitMasterMP4(
		ctx,
		material.ID,
		material.IngestEpoch,
		material.MediaWindows,
		material.NextWindowSeg,
		durMS,
		mp4URL,
		asrDue,
	); err != nil {
		w.logger.Error("写回主 MP4 元数据失败",
			liveRecordFields("master_commit_fail", material.ID,
				zap.Int("window_count", windowCount),
				zap.Int64("duration_ms", durMS),
				zap.Bool("asr_due", asrDue),
				zap.Error(err),
			)...,
		)
		return fmt.Errorf("写回主 MP4 元数据失败: %w", err)
	}
	// 提交后回读，避免内存中的 media_windows / next_window_seg 落后于合并结果。
	if latest, err := w.repo.GetByID(ctx, material.ID); err == nil && latest != nil {
		material.MediaWindows = latest.MediaWindows
		material.NextWindowSeg = latest.NextWindowSeg
		material.NextSeg = latest.NextSeg
		material.Duration = latest.Duration
		material.LiveURL = latest.LiveURL
		material.ASRDue = latest.ASRDue
	} else {
		if err != nil {
			w.logger.Warn("master 提交后回读素材失败",
				liveRecordFields("master_commit_reread_fail", material.ID, zap.Error(err))...,
			)
		}
		material.NextSeg = material.NextWindowSeg
	}

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
		liveRecordFields("master_ready", material.ID,
			zap.Int("window_count", windowCount),
			zap.Bool("asr_due", asrDue),
			zap.Int64("duration_ms", material.Duration),
			zap.String("live_url", material.LiveURL),
		)...,
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
