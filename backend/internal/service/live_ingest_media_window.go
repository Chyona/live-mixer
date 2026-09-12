package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/utils"

	"go.uber.org/zap"
)

// planMediaWindowSeal 计算下一窗应覆盖的分片区间（满 MediaWindowMS 再封窗）。
func planMediaWindowSeal(
	segStart, nextSeg int64,
	segDurMS map[int64]int64,
	windowMS, nominalMS int64,
	forcePartial bool,
) (segEnd, sumMS int64, partial bool) {
	if windowMS <= 0 {
		windowMS = int64(model.LiveMediaWindowDuration / time.Millisecond)
	}
	if nominalMS <= 0 {
		nominalMS = 6000
	}
	if segStart < 0 {
		segStart = 0
	}
	segEnd = segStart
	for segEnd < nextSeg {
		ms := int64(0)
		if segDurMS != nil {
			ms = segDurMS[segEnd]
		}
		if ms <= 0 {
			ms = nominalMS
		}
		sumMS += ms
		segEnd++
		if sumMS >= windowMS {
			break
		}
	}
	partial = sumMS < windowMS
	if partial && !forcePartial {
		return segEnd, sumMS, true
	}
	return segEnd, sumMS, partial
}

// sealReadyMediaWindows 离散封窗 + 拼接 master：
// 1) 只将本窗分片 [segStart, segEnd) 合成 window_N.mp4；
// 2) 再把已就绪的前 N 个窗 MP4 拼成 master.mp4（预览/ASR/成片同源）；
// 3) 封窗成功后可删除本窗本地 TS（后续合成只依赖窗 MP4）。
// forcePartial：关播时即使未满一窗也封入剩余分片。
func (w *liveIngestWorker) sealReadyMediaWindows(
	ctx context.Context,
	material *model.LiveMaterial,
	segDurMS map[int64]int64,
	forcePartial bool,
) error {
	if material == nil || material.NextSeg <= 0 {
		return nil
	}
	windowMS := material.EffectiveMediaWindowMS()
	workDir := w.segmentDir(material)
	winDir := filepath.Join(workDir, liveingest.WindowsDirName())
	if err := os.MkdirAll(winDir, 0o755); err != nil {
		return err
	}
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}

	for {
		segStart := material.NextWindowSeg
		if segStart < 0 {
			segStart = 0
		}
		if segStart >= material.NextSeg {
			break
		}

		segEnd, sumMS, partial := planMediaWindowSeal(segStart, material.NextSeg, segDurMS, windowMS, nominal, forcePartial)
		if segEnd <= segStart {
			break
		}
		if partial && !forcePartial {
			w.logger.Debug("媒体窗未满，等待更多分片",
				zap.Uint("material_id", material.ID),
				zap.Int64("seg_start", segStart),
				zap.Int64("seg_end", segEnd),
				zap.Int64("sum_ms", sumMS),
				zap.Int64("window_ms", windowMS),
			)
			break
		}

		windows := material.ParsedMediaWindows()
		winIdx := 0
		if len(windows) > 0 {
			winIdx = windows[len(windows)-1].Index + 1
		}
		startMS := windows.TotalReadyMS()

		files := make([]string, 0, segEnd-segStart)
		for i := segStart; i < segEnd; i++ {
			p := filepath.Join(workDir, liveingest.SegmentFileName(int(i)))
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				return fmt.Errorf("封窗缺少分片 seg=%d path=%s", i, p)
			}
			files = append(files, p)
		}
		mp4Path := filepath.Join(winDir, liveingest.WindowMP4FileName(winIdx))

		w.logger.Info("开始合成媒体窗",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", winIdx),
			zap.Int64("seg_start", segStart),
			zap.Int64("seg_end", segEnd),
			zap.Int64("est_dur_ms", sumMS),
			zap.Int64("window_ms", windowMS),
			zap.Bool("partial", partial),
			zap.String("mp4", mp4Path),
		)
		if err := w.ffmpeg.ConcatMediaFiles(ctx, files, mp4Path); err != nil {
			return fmt.Errorf("合成媒体窗 mp4 失败: %w", err)
		}

		durMS := sumMS
		if w.prober != nil {
			if tl, err := w.prober.ProbeMediaTimeline(ctx, mp4Path); err == nil && tl.FormatDurationSec > 0 {
				if probed := int64(tl.FormatDurationSec * 1000); probed > 0 {
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

		objectKey := liveingest.WindowMP4ObjectKey(material.RecordUUID, winIdx)
		var mp4URL string
		if w.storage != nil {
			var err error
			mp4URL, err = w.storage.UploadFile(ctx, mp4Path, objectKey)
			if err != nil {
				return fmt.Errorf("上传媒体窗 mp4 失败: %w", err)
			}
		}

		wmeta := model.MediaWindow{
			Index:     winIdx,
			StartMS:   startMS,
			EndMS:     startMS + durMS,
			DurMS:     durMS,
			URL:       mp4URL,
			ObjectKey: objectKey,
			SegStart:  segStart,
			SegEnd:    segEnd,
			Ready:     true,
		}
		windows = windows.Upsert(wmeta)
		material.MediaWindows = windows.Marshal()
		material.NextWindowSeg = segEnd

		if err := w.rebuildMasterFromWindows(ctx, material); err != nil {
			return err
		}

		w.releaseLocalSegments(workDir, segStart, segEnd)

		w.logger.Info("媒体窗已就绪并完成主片拼接",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", winIdx),
			zap.Int64("window_dur_ms", durMS),
			zap.Int64("master_ready_ms", material.Duration),
			zap.String("window_url", mp4URL),
			zap.String("master_url", material.LiveURL),
		)
		enqueueWake(w.asrWake, 1)

		if !forcePartial {
			break
		}
		if material.NextWindowSeg >= material.NextSeg {
			break
		}
	}
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
		zap.String("mp4", mp4Path),
	)

	if len(files) == 1 {
		if err := copyFile(files[0], tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("复制单窗为主片失败: %w", err)
		}
	} else {
		if err := w.ffmpeg.ConcatMediaFiles(ctx, files, tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("拼接主 MP4 失败: %w", err)
		}
	}
	_ = os.Remove(mp4Path)
	if err := os.Rename(tmpPath, mp4Path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("替换主 MP4 失败: %w", err)
	}

	durMS := ready.TotalReadyMS()
	if w.prober != nil {
		if tl, err := w.prober.ProbeMediaTimeline(ctx, mp4Path); err == nil && tl.FormatDurationSec > 0 {
			if probed := int64(tl.FormatDurationSec * 1000); probed > 0 {
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
	if w.storage != nil {
		var err error
		mp4URL, err = w.storage.UploadFile(ctx, mp4Path, objectKey)
		if err != nil {
			return fmt.Errorf("上传主 MP4 失败: %w", err)
		}
	}
	if strings.TrimSpace(mp4URL) == "" {
		mp4URL = strings.TrimSpace(material.LiveURL)
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

	w.logger.Info("主 MP4 已由前 N 窗拼接就绪",
		zap.Uint("material_id", material.ID),
		zap.Int("window_count", len(ready)),
		zap.Int64("ready_ms", durMS),
		zap.String("mp4_url", mp4URL),
	)
	return nil
}

func (w *liveIngestWorker) releaseLocalSegments(workDir string, segStart, segEnd int64) {
	for i := segStart; i < segEnd; i++ {
		p := filepath.Join(workDir, liveingest.SegmentFileName(int(i)))
		_ = os.Remove(p)
	}
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
