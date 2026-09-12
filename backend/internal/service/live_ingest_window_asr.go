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

// runWindowASR 对拼接主 MP4 处理一个短 ASR chunk（默认 ≤2min）。
// 预览/成片/ASR 同源 master.mp4（由前 N 窗拼接）；按 [asr_cursor, cursor+chunk) 抽等长 MP3。
func (w *liveIngestWorker) runWindowASR(ctx context.Context, material *model.LiveMaterial) error {
	logStep := func(step string, fields ...zap.Field) {
		base := []zap.Field{
			zap.Uint("material_id", material.ID),
			zap.String("step", step),
		}
		w.logger.Info("主 MP4 ASR 步骤", append(base, fields...)...)
	}
	if w.asrService == nil {
		w.logger.Warn("主 MP4 ASR 跳过：ASR 服务未配置", zap.Uint("material_id", material.ID))
		return nil
	}
	epoch := material.ASREpoch
	if latest, err := w.repo.GetByID(ctx, material.ID); err == nil && latest != nil {
		material.ASRCursorMS = latest.ASRCursorMS
		material.LiveASR = latest.LiveASR
		material.ASREpoch = latest.ASREpoch
		material.MediaWindows = latest.MediaWindows
		material.Duration = latest.Duration
		material.LiveURL = latest.LiveURL
		epoch = latest.ASREpoch
	}
	cursor := material.ASRCursorMS
	readyMS := material.MasterReadyMS()
	if readyMS <= 0 {
		w.logger.Info("主 MP4 ASR 跳过：主文件尚未就绪",
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_cursor_ms", cursor),
		)
		return nil
	}
	if cursor >= readyMS {
		progress := windowASRProgress(readyMS, readyMS)
		if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, material.LiveASR, readyMS, readyMS, progress, false); err != nil {
			return err
		}
		material.ASRCursorMS = readyMS
		material.Duration = readyMS
		material.ASRProgress = progress
		material.ASRDue = false
		return nil
	}

	chunkMS := int64(model.MaxASRTranscribeDuration / time.Millisecond)
	if chunkMS <= 0 {
		chunkMS = int64((2 * time.Minute) / time.Millisecond)
	}
	remain := readyMS - cursor
	thisChunkMS := chunkMS
	if thisChunkMS > remain {
		thisChunkMS = remain
	}
	offsetMS := cursor
	asrStart := time.Now()
	logStep("resolve_chunk",
		zap.Int64("ready_ms", readyMS),
		zap.Int64("chunk_ms", thisChunkMS),
		zap.Int64("offset_ms", offsetMS),
		zap.Int64("asr_cursor_ms", cursor),
	)
	mp4Path, err := w.resolveMasterMP4Path(ctx, material)
	if err != nil {
		return err
	}

	startSec := float64(cursor) / 1000.0
	durSec := float64(thisChunkMS) / 1000.0
	align := media.ASRAlignOptions{TargetDurSec: durSec}
	// 仅主文件起点 chunk 应用整文件 A/V 对齐；中段 seek 后再垫片会把时间轴再次弄歪。
	if cursor == 0 && w.prober != nil {
		if tl, perr := w.prober.ProbeMediaTimeline(ctx, mp4Path); perr == nil {
			opts := tl.AlignOptions()
			align.LeadPadMs = opts.LeadPadMs
			align.TrimStartSec = opts.TrimStartSec
			align.TargetDurSec = durSec
		}
	}

	tmpMP3 := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_master_%d.mp3", cursor))
	logStep("mp3_start",
		zap.String("mp4", mp4Path),
		zap.Float64("start_sec", startSec),
		zap.Float64("dur_sec", durSec),
		zap.Float64("target_dur_sec", align.TargetDurSec),
		zap.Int64("lead_pad_ms", align.LeadPadMs),
	)
	mp3Start := time.Now()
	if err := w.ffmpeg.ConvertRangeToASRMP3Aligned(ctx, mp4Path, tmpMP3, startSec, durSec, align); err != nil {
		return fmt.Errorf("从主 MP4 抽等长 ASR MP3 失败: %w", err)
	}
	defer os.Remove(tmpMP3)
	logStep("mp3_ok", zap.Duration("elapsed", time.Since(mp3Start)), zap.Float64("target_dur_sec", align.TargetDurSec))
	if w.storage == nil {
		return fmt.Errorf("对象存储未配置")
	}
	logStep("upload_start")
	uploadStart := time.Now()
	objectKey := fmt.Sprintf("%s/live-asr-%d-master-%d.mp3", storage.SubDirTemp, material.ID, cursor)
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, objectKey)
	if err != nil {
		return err
	}
	logStep("upload_ok", zap.Duration("elapsed", time.Since(uploadStart)))
	logStep("transcribe_start")
	transcribeStart := time.Now()
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		advanceReason := "识别失败跳过"
		if asr.IsSilenceAudioError(err) {
			advanceReason = "静音跳过"
		}
		w.logger.Warn("主 MP4 ASR "+advanceReason+"，推进游标",
			zap.Uint("material_id", material.ID),
			zap.Int64("cursor_ms", cursor),
			zap.Int64("chunk_ms", thisChunkMS),
			zap.Duration("elapsed", time.Since(transcribeStart)),
			zap.Error(err),
		)
		return w.commitMasterASRProgress(ctx, material, epoch, readyMS, thisChunkMS, cursor, material.LiveASR, 0, 1.0, asrStart)
	}
	logStep("transcribe_ok", zap.Duration("elapsed", time.Since(transcribeStart)))

	asrDur := asr.ParseDurationMs(raw)
	maxUttEnd := asr.MaxUtteranceEndMS(raw)
	scaledRaw := raw
	scaleFactor := 1.0
	if asr.ShouldScaleUtteranceTimestamps(asrDur, thisChunkMS, maxUttEnd, asr.DefaultScaleSkewThresholdMS) {
		scaled, scale, scaleErr := asr.ScaleTimestampsToDuration(raw, thisChunkMS)
		if scaleErr != nil {
			w.logger.Warn("主 MP4 ASR chunk 兜底缩放失败，沿用厂商时间戳",
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
	return w.commitMasterASRProgress(ctx, material, epoch, readyMS, thisChunkMS, cursor, merged, asrDur, scaleFactor, asrStart)
}

func (w *liveIngestWorker) commitMasterASRProgress(
	ctx context.Context,
	material *model.LiveMaterial,
	epoch int64,
	readyMS, thisChunkMS, cursor int64,
	liveASR string,
	asrVendorDurMS int64,
	scaleFactor float64,
	asrStart time.Time,
) error {
	newCursor := cursor + thisChunkMS
	if newCursor > readyMS {
		newCursor = readyMS
	}
	if newCursor < cursor {
		newCursor = cursor
	}
	stillPending := newCursor < readyMS
	progress := windowASRProgress(readyMS, newCursor)
	w.logger.Info("主 MP4 ASR 步骤",
		zap.Uint("material_id", material.ID),
		zap.String("step", "db_start"),
		zap.Int64("asr_cursor_ms", newCursor),
		zap.Bool("still_due", stillPending),
	)
	if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, liveASR, newCursor, readyMS, progress, stillPending); err != nil {
		return err
	}
	material.ASRCursorMS = newCursor
	material.LiveASR = liveASR
	material.ASRProgress = progress
	material.ASRDue = stillPending
	material.Duration = readyMS
	w.logger.Info("主 MP4 ASR chunk 完成",
		zap.Uint("material_id", material.ID),
		zap.Duration("elapsed", time.Since(asrStart)),
		zap.Int64("offset_ms", cursor),
		zap.Int64("chunk_ms", thisChunkMS),
		zap.Int64("asr_cursor_ms", newCursor),
		zap.Int64("ready_ms", readyMS),
		zap.Int64("asr_vendor_duration_ms", asrVendorDurMS),
		zap.Float64("asr_time_scale", scaleFactor),
		zap.Int16("asr_progress", progress),
	)
	return nil
}

func windowASRProgress(readyMS, cursorMS int64) int16 {
	progress := int16(10)
	if readyMS > 0 {
		progress = int16(20 + 79*cursorMS/readyMS)
		if progress > 99 {
			progress = 99
		}
	}
	return progress
}
