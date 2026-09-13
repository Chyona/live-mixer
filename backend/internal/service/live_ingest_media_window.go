package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/utils"

	"go.uber.org/zap"
)

// minRecordedWindowMS 有效窗最短时长；更短视为空录。
const minRecordedWindowMS int64 = 1000

// commitRecordedWindow 将已录好的 window_N.mp4 登记、上传，并拼接 master.mp4。
// SegStart/SegEnd 使用窗下标语义（[winIdx, winIdx+1)），不再表示 TS 分片区间。
func (w *liveIngestWorker) commitRecordedWindow(
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
	var mp4URL string
	if w.storage != nil {
		var uerr error
		mp4URL, uerr = w.storage.UploadFile(ctx, mp4Path, objectKey)
		if uerr != nil {
			return fmt.Errorf("上传媒体窗 mp4 失败: %w", uerr)
		}
	}

	wmeta := model.MediaWindow{
		Index:     winIdx,
		StartMS:   startMS,
		EndMS:     startMS + durMS,
		DurMS:     durMS,
		URL:       mp4URL,
		ObjectKey: objectKey,
		SegStart:  int64(winIdx),
		SegEnd:    int64(winIdx + 1),
		Ready:     true,
	}
	windows = windows.Upsert(wmeta)
	material.MediaWindows = windows.Marshal()
	nextIdx := int64(winIdx + 1)
	material.NextWindowSeg = nextIdx
	material.NextSeg = nextIdx

	winEv := AlignDiagEvent{
		Event:       "window_sealed",
		WindowIndex: winIdx,
		WindowCount: windows.ReadyCount(),
		LocalPath:   mp4Path,
		URL:         mp4URL,
		ReadyMS:     startMS + durMS,
	}
	if hasWinTL {
		fillAlignTimeline(&winEv, winTL)
	}
	w.emitAlignDiag(material, winEv)

	if err := w.rebuildMasterFromWindows(ctx, material); err != nil {
		w.emitAlignDiag(material, AlignDiagEvent{
			Event:       "master_rebuild_failed",
			WindowIndex: winIdx,
			WindowCount: windows.ReadyCount(),
			Error:       err.Error(),
		})
		return err
	}

	w.logger.Info("媒体窗已就绪并完成主片拼接",
		zap.Uint("material_id", material.ID),
		zap.Int("window_index", winIdx),
		zap.Int64("window_dur_ms", durMS),
		zap.Bool("partial", partial),
		zap.Int64("master_ready_ms", material.Duration),
		zap.String("window_url", mp4URL),
		zap.String("master_url", material.LiveURL),
	)
	enqueueWake(w.asrWake, 1)
	return nil
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
		if err := w.ffmpeg.ConcatMP4ContinuousTimeline(ctx, files, tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("拼接主 MP4 失败: %w", err)
		}
	}

	durMS := ready.TotalReadyMS()
	var masterTL media.MediaTimeline
	var hasMasterTL bool
	if w.prober != nil {
		if tl, err := w.prober.ProbeMediaTimeline(ctx, tmpPath); err == nil {
			masterTL, hasMasterTL = tl, true
			// 权威时长优先视频轴，避免 format 跟偏长音轨。
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
				WindowCount: len(files),
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

	// 上传成功后再替换本地正式 master。
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
		WindowCount:    len(files),
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

	w.logger.Info("主 MP4 已由前 N 窗拼接就绪",
		zap.Uint("material_id", material.ID),
		zap.Int("window_count", len(ready)),
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
