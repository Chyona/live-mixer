package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/utils"

	"go.uber.org/zap"
)

// planMediaWindowSeal 计算下一增长步应覆盖的分片区间（满 MediaWindowMS 再封入主 MP4）。
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

// sealReadyMediaWindows 按步长递增主 MP4：每次将分片 [0, segEnd) 重合成一份更长的 master.mp4
//（约 10/20/30… 分钟），上传后写回 media_windows（单条）与 duration，并触发 ASR。
// forcePartial：关播时即使未满一步也封入剩余分片。
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
	if err := os.MkdirAll(workDir, 0o755); err != nil {
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
			w.logger.Debug("主 MP4 未满一步，等待更多分片",
				zap.Uint("material_id", material.ID),
				zap.Int64("seg_start", segStart),
				zap.Int64("seg_end", segEnd),
				zap.Int64("sum_ms", sumMS),
				zap.Int64("step_ms", windowMS),
			)
			break
		}

		// 权威时钟：始终用 [0, segEnd) 全部分片重合成主文件，保证 10/20/30… 递增且时间轴连续。
		files := make([]string, 0, segEnd)
		for i := int64(0); i < segEnd; i++ {
			p := filepath.Join(workDir, liveingest.SegmentFileName(int(i)))
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				return fmt.Errorf("合成主 MP4 缺少分片 seg=%d path=%s", i, p)
			}
			files = append(files, p)
		}
		mp4Path := filepath.Join(workDir, liveingest.MasterMP4FileName())
		tmpPath := filepath.Join(workDir, "master_building.mp4")
		_ = os.Remove(tmpPath)

		w.logger.Info("开始合成递增主 MP4",
			zap.Uint("material_id", material.ID),
			zap.Int64("seg_end", segEnd),
			zap.Int("seg_count", len(files)),
			zap.Int64("step_new_ms", sumMS),
			zap.Int64("step_ms", windowMS),
			zap.Bool("partial", partial),
			zap.String("mp4", mp4Path),
		)
		if err := w.ffmpeg.ConcatMediaFiles(ctx, files, tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("合成主 MP4 失败: %w", err)
		}
		_ = os.Remove(mp4Path)
		if err := os.Rename(tmpPath, mp4Path); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("替换主 MP4 失败: %w", err)
		}

		durMS := int64(0)
		for i := int64(0); i < segEnd; i++ {
			ms := int64(0)
			if segDurMS != nil {
				ms = segDurMS[i]
			}
			if ms <= 0 {
				ms = nominal
			}
			durMS += ms
		}
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

		wmeta := model.MediaWindow{
			Index:     0,
			StartMS:   0,
			EndMS:     durMS,
			DurMS:     durMS,
			URL:       mp4URL,
			ObjectKey: objectKey,
			SegStart:  0,
			SegEnd:    segEnd,
			Ready:     true,
		}
		material.MediaWindows = model.WithMaster(wmeta).Marshal()
		material.NextWindowSeg = segEnd
		material.Duration = durMS
		if mp4URL != "" {
			material.LiveURL = mp4URL
		}

		if err := w.repo.CommitMasterMP4(ctx, material.ID, material.IngestEpoch, material.MediaWindows, material.NextWindowSeg, durMS, mp4URL, true); err != nil {
			return fmt.Errorf("写回主 MP4 元数据失败: %w", err)
		}
		material.ASRDue = true
		w.logger.Info("主 MP4 已递增就绪",
			zap.Uint("material_id", material.ID),
			zap.Int64("ready_ms", durMS),
			zap.Int64("seg_end", segEnd),
			zap.String("mp4_url", mp4URL),
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

// resolveMasterMP4Path 优先本地 master.mp4，否则按 URL 下载。
func (w *liveIngestWorker) resolveMasterMP4Path(ctx context.Context, material *model.LiveMaterial) (string, error) {
	workDir := w.segmentDir(material)
	local := filepath.Join(workDir, liveingest.MasterMP4FileName())
	if st, err := os.Stat(local); err == nil && st.Size() > 0 {
		return local, nil
	}
	master, ok := material.ParsedMediaWindows().Master()
	if !ok {
		return "", fmt.Errorf("主 MP4 尚未就绪")
	}
	url := strings.TrimSpace(master.URL)
	if url == "" {
		url = strings.TrimSpace(material.LiveURL)
	}
	if url == "" {
		return "", fmt.Errorf("主 MP4 无本地文件且无下载地址")
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
