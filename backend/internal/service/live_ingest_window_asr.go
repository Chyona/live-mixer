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
	offsetMS := win.StartMS
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
	asrStart := time.Now()
	logStep("resolve_window",
		zap.Int("window_index", win.Index),
		zap.Int64("start_ms", win.StartMS),
		zap.Int64("end_ms", win.EndMS),
		zap.Int64("dur_ms", targetWindowMS),
		zap.Int64("asr_cursor_ms", cursor),
	)
	mp4Path, err := w.resolveWindowMP4Path(ctx, material, win)
	if err != nil {
		return err
	}
	tmpMP3 := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_win_%d.mp3", win.Index))
	align := media.ASRAlignOptions{TargetDurSec: float64(targetWindowMS) / 1000.0}
	if w.prober != nil {
		if tl, perr := w.prober.ProbeMediaTimeline(ctx, mp4Path); perr == nil {
			opts := tl.AlignOptions()
			align.LeadPadMs = opts.LeadPadMs
			align.TrimStartSec = opts.TrimStartSec
			align.TargetDurSec = float64(targetWindowMS) / 1000.0
		}
	}
	logStep("mp3_start", zap.String("mp4", mp4Path), zap.Float64("target_dur_sec", align.TargetDurSec))
	mp3Start := time.Now()
	if err := w.ffmpeg.ConvertToASRMP3Aligned(ctx, mp4Path, tmpMP3, align); err != nil {
		w.logger.Warn("窗口 ASR 对齐转 MP3 失败，回退普通转码",
			zap.Uint("material_id", material.ID),
			zap.Duration("elapsed", time.Since(mp3Start)),
			zap.Error(err),
		)
		mp3Start = time.Now()
		if err := w.ffmpeg.ConvertToASRMP3(ctx, mp4Path, tmpMP3); err != nil {
			return err
		}
	}
	defer os.Remove(tmpMP3)
	logStep("mp3_ok", zap.Duration("elapsed", time.Since(mp3Start)))
	if w.storage == nil {
		return fmt.Errorf("对象存储未配置")
	}
	logStep("upload_start")
	uploadStart := time.Now()
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, fmt.Sprintf("%s/live-asr-%d-win%d.mp3", storage.SubDirTemp, material.ID, win.Index))
	if err != nil {
		return err
	}
	logStep("upload_ok", zap.Duration("elapsed", time.Since(uploadStart)))
	logStep("transcribe_start")
	transcribeStart := time.Now()
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		w.logger.Warn("窗口 ASR 识别失败，跳过本窗",
			zap.Uint("material_id", material.ID),
			zap.Int("window_index", win.Index),
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
	if asr.ShouldScaleUtteranceTimestamps(asrDur, targetWindowMS, maxUttEnd, asr.DefaultScaleSkewThresholdMS) {
		scaled, scale, scaleErr := asr.ScaleTimestampsToDuration(raw, targetWindowMS)
		if scaleErr != nil {
			w.logger.Warn("窗口 ASR 兜底缩放失败，沿用厂商时间戳",
				zap.Uint("material_id", material.ID),
				zap.Error(scaleErr),
			)
		} else {
			scaledRaw = scaled
			scaleFactor = scale
		}
	} else if rewritten, err := asr.SetAudioInfoDuration(raw, targetWindowMS); err == nil {
		scaledRaw = rewritten
	}
	merged, _, err := asr.MergeWindowASR(material.LiveASR, scaledRaw, offsetMS, cursor)
	if err != nil {
		return err
	}
	newCursor := win.EndMS
	if newCursor < cursor {
		newCursor = cursor
	}
	progress := int16(10)
	if material.Duration > 0 {
		progress = int16(20 + 79*newCursor/material.Duration)
		if progress > 99 {
			progress = 99
		}
	}
	_, stillPending := material.ParsedMediaWindows().NextPendingASRWindow(newCursor)
	logStep("db_start", zap.Int64("asr_cursor_ms", newCursor), zap.Bool("still_due", stillPending))
	if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, merged, newCursor, material.Duration, progress, stillPending); err != nil {
		return err
	}
	material.ASRCursorMS = newCursor
	material.LiveASR = merged
	material.ASRProgress = progress
	material.ASRDue = stillPending
	w.logger.Info("窗口 ASR 完成",
		zap.Uint("material_id", material.ID),
		zap.Duration("elapsed", time.Since(asrStart)),
		zap.Int("window_index", win.Index),
		zap.Int64("offset_ms", offsetMS),
		zap.Int64("asr_cursor_ms", newCursor),
		zap.Int64("window_ms", targetWindowMS),
		zap.Int64("asr_vendor_duration_ms", asrDur),
		zap.Float64("asr_time_scale", scaleFactor),
		zap.Int16("asr_progress", progress),
	)
	return nil
}
