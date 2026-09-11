package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"

	"go.uber.org/zap"
)

// runWindowASR 对下一个待转写媒体窗处理一个短 ASR chunk（默认 ≤2min）。
// 预览/成片仍用完整窗 MP4；此处只从该 MP4 按时间范围抽音，避免整窗一次转写后线性缩放把字幕压歪。
func (w *liveIngestWorker) runWindowASR(ctx context.Context, material *model.LiveMaterial) error {
	logStep := func(step string, fields ...zap.Field) {
		base := []zap.Field{
			zap.Uint("material_id", material.ID),
			zap.String("step", step),
		}
		w.logger.Info("窗口 ASR 步骤", append(base, fields...)...)
	}
	if w.asrService == nil {
		w.logger.Warn("窗口 ASR 跳过：ASR 服务未配置", zap.Uint("material_id", material.ID))
		return nil
	}
	epoch := material.ASREpoch
	if latest, err := w.repo.GetByID(ctx, material.ID); err == nil && latest != nil {
		material.ASRCursorMS = latest.ASRCursorMS
		material.LiveASR = latest.LiveASR
		material.ASREpoch = latest.ASREpoch
		material.MediaWindows = latest.MediaWindows
		material.Duration = latest.Duration
		epoch = latest.ASREpoch
	}
	cursor := material.ASRCursorMS
	windows := material.ParsedMediaWindows()
	win, ok := windows.NextPendingASRWindow(cursor)
	if !ok {
		w.logger.Info("窗口 ASR 跳过：无待转写媒体窗",
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_cursor_ms", cursor),
			zap.Int("ready_windows", windows.ReadyCount()),
		)
		return nil
	}
	targetWindowMS := win.DurMS
	if targetWindowMS <= 0 {
		targetWindowMS = win.EndMS - win.StartMS
	}
	if targetWindowMS <= 0 {
		w.logger.Warn("窗口 ASR 跳过：媒体窗时长无效",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", win.Index),
		)
		return nil
	}
	localCursor := cursor - win.StartMS
	if localCursor < 0 {
		localCursor = 0
	}
	if localCursor >= targetWindowMS {
		newCursor := win.EndMS
		if newCursor < cursor {
			newCursor = cursor
		}
		_, stillPending := windows.NextPendingASRWindow(newCursor)
		progress := windowASRProgress(material.Duration, newCursor)
		if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, material.LiveASR, newCursor, material.Duration, progress, stillPending); err != nil {
			return err
		}
		material.ASRCursorMS = newCursor
		material.ASRProgress = progress
		material.ASRDue = stillPending
		return nil
	}

	chunkMS := int64(model.MaxASRTranscribeDuration / time.Millisecond)
	if chunkMS <= 0 {
		chunkMS = int64((2 * time.Minute) / time.Millisecond)
	}
	remain := targetWindowMS - localCursor
	thisChunkMS := chunkMS
	if thisChunkMS > remain {
		thisChunkMS = remain
	}
	offsetMS := win.StartMS + localCursor
	asrStart := time.Now()
	logStep("resolve_chunk",
		zap.Int("window_index", win.Index),
		zap.Int64("window_start_ms", win.StartMS),
		zap.Int64("window_end_ms", win.EndMS),
		zap.Int64("window_ms", targetWindowMS),
		zap.Int64("local_cursor_ms", localCursor),
		zap.Int64("chunk_ms", thisChunkMS),
		zap.Int64("offset_ms", offsetMS),
		zap.Int64("asr_cursor_ms", cursor),
	)
	mp4Path, err := w.resolveWindowMP4Path(ctx, material, win)
	if err != nil {
		return err
	}

	startSec := float64(localCursor) / 1000.0
	durSec := float64(thisChunkMS) / 1000.0
	align := media.ASRAlignOptions{TargetDurSec: durSec}
	// 仅窗内起点 chunk 应用整文件 A/V 对齐；中段 seek 后再垫片会把时间轴再次弄歪。
	if localCursor == 0 && w.prober != nil {
		if tl, perr := w.prober.ProbeMediaTimeline(ctx, mp4Path); perr == nil {
			opts := tl.AlignOptions()
			align.LeadPadMs = opts.LeadPadMs
			align.TrimStartSec = opts.TrimStartSec
			align.TargetDurSec = durSec
		}
	}

	tmpMP3 := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_win_%d_%d.mp3", win.Index, localCursor))
	logStep("mp3_start",
		zap.String("mp4", mp4Path),
		zap.Float64("start_sec", startSec),
		zap.Float64("dur_sec", durSec),
		zap.Float64("target_dur_sec", align.TargetDurSec),
		zap.Int64("lead_pad_ms", align.LeadPadMs),
	)
	mp3Start := time.Now()
	// 必须：窗 MP4 → 与目标时长等长的 MP3 → 再送 ASR。禁止回退到无时长约束的抽音。
	if err := w.ffmpeg.ConvertRangeToASRMP3Aligned(ctx, mp4Path, tmpMP3, startSec, durSec, align); err != nil {
		return fmt.Errorf("从窗 MP4 抽等长 ASR MP3 失败: %w", err)
	}
	defer os.Remove(tmpMP3)
	logStep("mp3_ok", zap.Duration("elapsed", time.Since(mp3Start)), zap.Float64("target_dur_sec", align.TargetDurSec))
	if w.storage == nil {
		return fmt.Errorf("对象存储未配置")
	}
	logStep("upload_start")
	uploadStart := time.Now()
	objectKey := fmt.Sprintf("%s/live-asr-%d-win%d-%d.mp3", storage.SubDirTemp, material.ID, win.Index, localCursor)
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, objectKey)
	if err != nil {
		return err
	}
	logStep("upload_ok", zap.Duration("elapsed", time.Since(uploadStart)))
	logStep("transcribe_start")
	transcribeStart := time.Now()
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		w.logger.Warn("窗口 ASR 识别失败，跳过本 chunk",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", win.Index),
			zap.Int64("local_cursor_ms", localCursor),
			zap.Duration("elapsed", time.Since(transcribeStart)),
			zap.Error(err),
		)
		return nil
	}
	logStep("transcribe_ok", zap.Duration("elapsed", time.Since(transcribeStart)))

	asrDur := asr.ParseDurationMs(raw)
	maxUttEnd := asr.MaxUtteranceEndMS(raw)
	scaledRaw := raw
	scaleFactor := 1.0
	if asr.ShouldScaleUtteranceTimestamps(asrDur, thisChunkMS, maxUttEnd, asr.DefaultScaleSkewThresholdMS) {
		scaled, scale, scaleErr := asr.ScaleTimestampsToDuration(raw, thisChunkMS)
		if scaleErr != nil {
			w.logger.Warn("窗口 ASR chunk 兜底缩放失败，沿用厂商时间戳",
				zap.Uint("material_id", material.ID),
				zap.Error(scaleErr),
			)
		} else {
			scaledRaw = scaled
			scaleFactor = scale
		}
	} else if rewritten, err := asr.SetAudioInfoDuration(raw, thisChunkMS); err == nil {
		scaledRaw = rewritten
	}
	merged, _, err := asr.MergeWindowASR(material.LiveASR, scaledRaw, offsetMS, cursor)
	if err != nil {
		return err
	}
	newLocal := localCursor + thisChunkMS
	newCursor := win.StartMS + newLocal
	if newLocal >= targetWindowMS {
		newCursor = win.EndMS
	}
	if newCursor < cursor {
		newCursor = cursor
	}
	progress := windowASRProgress(material.Duration, newCursor)
	_, stillPending := material.ParsedMediaWindows().NextPendingASRWindow(newCursor)
	logStep("db_start", zap.Int64("asr_cursor_ms", newCursor), zap.Bool("still_due", stillPending))
	if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, merged, newCursor, material.Duration, progress, stillPending); err != nil {
		return err
	}
	material.ASRCursorMS = newCursor
	material.LiveASR = merged
	material.ASRProgress = progress
	material.ASRDue = stillPending
	w.logger.Info("窗口 ASR chunk 完成",
		zap.Uint("material_id", material.ID),
		zap.Duration("elapsed", time.Since(asrStart)),
		zap.Int("window_index", win.Index),
		zap.Int64("offset_ms", offsetMS),
		zap.Int64("chunk_ms", thisChunkMS),
		zap.Int64("asr_cursor_ms", newCursor),
		zap.Int64("window_ms", targetWindowMS),
		zap.Int64("asr_vendor_duration_ms", asrDur),
		zap.Float64("asr_time_scale", scaleFactor),
		zap.Int16("asr_progress", progress),
	)
	return nil
}

func windowASRProgress(durationMS, cursorMS int64) int16 {
	progress := int16(10)
	if durationMS > 0 {
		progress = int16(20 + 79*cursorMS/durationMS)
		if progress > 99 {
			progress = 99
		}
	}
	return progress
}
