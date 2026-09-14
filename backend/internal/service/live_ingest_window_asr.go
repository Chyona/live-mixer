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

// runFullMasterASR 对当前整份 master.mp4 做 1 次完整 ASR，覆盖写入 live_asr。
// 每次 master 变长（asr_due）触发；不再按 2 分钟 cursor 切块 merge。
// 若转写过程中 master 已再次变长：仍写入本次结果作过渡，并保持 asr_due 以便立即再跑全量。
func (w *liveIngestWorker) runFullMasterASR(ctx context.Context, material *model.LiveMaterial) error {
	logStep := func(step string, fields ...zap.Field) {
		base := []zap.Field{
			zap.Uint("material_id", material.ID),
			zap.String("step", step),
		}
		w.logger.Info("主 MP4 全量 ASR 步骤", append(base, fields...)...)
	}
	if w.asrService == nil {
		w.logger.Warn("主 MP4 全量 ASR 跳过：ASR 服务未配置", zap.Uint("material_id", material.ID))
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
		material.ASRParagraphs = latest.ASRParagraphs
		epoch = latest.ASREpoch
	}

	targetReadyMS := material.MasterReadyMS()
	if targetReadyMS <= 0 {
		w.logger.Info("主 MP4 全量 ASR 跳过：主文件尚未就绪",
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_cursor_ms", material.ASRCursorMS),
		)
		return nil
	}
	if material.ASRCursorMS >= targetReadyMS {
		progress := windowASRProgress(targetReadyMS, targetReadyMS)
		paras := w.rebuildLiveASRParagraphs(material.LiveASR, targetReadyMS)
		if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, material.LiveASR, targetReadyMS, targetReadyMS, progress, false, paras); err != nil {
			return err
		}
		material.ASRCursorMS = targetReadyMS
		material.Duration = targetReadyMS
		material.ASRProgress = progress
		material.ASRDue = false
		material.ASRParagraphs = paras
		return nil
	}

	asrStart := time.Now()
	logStep("resolve_full",
		zap.Int64("target_ready_ms", targetReadyMS),
		zap.Int64("asr_cursor_ms", material.ASRCursorMS),
	)
	// 厂商文档：单次 ≤5h / <512MB；10/30/60+ 分钟会显著增加耗时与费用（O(N²) 累计）。
	readyDur := time.Duration(targetReadyMS) * time.Millisecond
	if readyDur > asr.VendorMaxAudioDuration {
		w.logger.Error("主 MP4 全量 ASR 超过厂商时长上限，仍将尝试提交（失败不推进游标）",
			zap.Uint("material_id", material.ID),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
			zap.Duration("vendor_max", asr.VendorMaxAudioDuration),
		)
	} else if readyDur >= 60*time.Minute {
		w.logger.Warn("主 MP4 全量 ASR ≥60 分钟，关注轮询超时与 512MB/拒识码",
			zap.Uint("material_id", material.ID),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
		)
	} else if readyDur >= 30*time.Minute {
		w.logger.Warn("主 MP4 全量 ASR ≥30 分钟，关注厂商耗时与费用",
			zap.Uint("material_id", material.ID),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
		)
	} else if readyDur >= 10*time.Minute {
		w.logger.Info("主 MP4 全量 ASR ≥10 分钟",
			zap.Uint("material_id", material.ID),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
		)
	}

	localMaster := filepath.Join(w.segmentDir(material), "master.mp4")
	existedBefore := false
	if st, err := os.Stat(localMaster); err == nil && st.Size() > 0 {
		existedBefore = true
	}
	mp4Path, err := w.resolveMasterMP4Path(ctx, material)
	if err != nil {
		return err
	}
	asrSource := resolveMasterASRSource(existedBefore)

	durSec := float64(targetReadyMS) / 1000.0
	align := media.ASRAlignOptions{TargetDurSec: durSec}
	if w.prober != nil {
		if tl, perr := w.prober.ProbeMediaTimeline(ctx, mp4Path); perr == nil {
			opts := tl.AlignOptions()
			align.LeadPadMs = opts.LeadPadMs
			align.TrimStartSec = opts.TrimStartSec
			align.TargetDurSec = durSec
		}
	}

	chunkEv := AlignDiagEvent{
		Event:        "asr_full_start",
		LocalPath:    mp4Path,
		ASRSource:    asrSource,
		ASRCursorMS:  material.ASRCursorMS,
		ReadyMS:      targetReadyMS,
		ChunkMS:      targetReadyMS,
		LeadPadMS:    align.LeadPadMs,
		TrimStartSec: align.TrimStartSec,
	}
	if tl, ok := w.probeAlignTimeline(ctx, mp4Path); ok {
		fillAlignTimeline(&chunkEv, tl)
	}
	w.emitAlignDiag(material, chunkEv)

	tmpMP3 := filepath.Join(w.segmentDir(material), fmt.Sprintf("asr_master_full_%d.mp3", targetReadyMS))
	logStep("mp3_start",
		zap.String("mp4", mp4Path),
		zap.String("asr_source", asrSource),
		zap.Float64("start_sec", 0),
		zap.Float64("dur_sec", durSec),
		zap.Float64("target_dur_sec", align.TargetDurSec),
		zap.Int64("lead_pad_ms", align.LeadPadMs),
	)
	mp3Start := time.Now()
	if err := w.ffmpeg.ConvertRangeToASRMP3Aligned(ctx, mp4Path, tmpMP3, 0, durSec, align); err != nil {
		return fmt.Errorf("从主 MP4 抽全量 ASR MP3 失败: %w", err)
	}
	defer os.Remove(tmpMP3)
	logStep("mp3_ok", zap.Duration("elapsed", time.Since(mp3Start)), zap.Float64("target_dur_sec", align.TargetDurSec))
	if w.storage == nil {
		return fmt.Errorf("对象存储未配置")
	}
	logStep("upload_start")
	uploadStart := time.Now()
	objectKey := fmt.Sprintf("%s/live-asr-%d-master-full-%d.mp3", storage.SubDirTemp, material.ID, targetReadyMS)
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, objectKey)
	if err != nil {
		return err
	}
	logStep("upload_ok", zap.Duration("elapsed", time.Since(uploadStart)))
	logStep("transcribe_start")
	transcribeStart := time.Now()
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		if asr.IsSilenceAudioError(err) {
			w.logger.Warn("主 MP4 全量 ASR 静音，覆盖为空结果并推进游标",
				zap.Uint("material_id", material.ID),
				zap.Int64("target_ready_ms", targetReadyMS),
				zap.Duration("elapsed", time.Since(transcribeStart)),
				zap.Error(err),
			)
			w.emitAlignDiag(material, AlignDiagEvent{
				Event:       "asr_full_skipped",
				LocalPath:   mp4Path,
				ASRSource:   asrSource,
				ASRCursorMS: material.ASRCursorMS,
				ReadyMS:     targetReadyMS,
				ChunkMS:     targetReadyMS,
				LeadPadMS:   align.LeadPadMs,
				Error:       err.Error(),
			})
			empty := `{"audio_info":{"duration":0},"result":{"utterances":[]}}`
			return w.commitFullMasterASR(ctx, material, epoch, targetReadyMS, empty, 0, 1.0, asrStart)
		}
		failKind := asr.ClassifyTranscribeFailure(err)
		w.logger.Warn("主 MP4 全量 ASR 识别失败，不推进游标、不覆盖 live_asr",
			zap.Uint("material_id", material.ID),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", time.Duration(targetReadyMS)*time.Millisecond),
			zap.Duration("elapsed", time.Since(transcribeStart)),
			zap.String("failure_kind", string(failKind)),
			zap.Error(err),
		)
		w.emitAlignDiag(material, AlignDiagEvent{
			Event:       "asr_full_failed",
			LocalPath:   mp4Path,
			ASRSource:   asrSource,
			ASRCursorMS: material.ASRCursorMS,
			ReadyMS:     targetReadyMS,
			ChunkMS:     targetReadyMS,
			LeadPadMS:   align.LeadPadMs,
			Error:       fmt.Sprintf("%s: %v", failKind, err),
		})
		return fmt.Errorf("主 MP4 全量 ASR 失败(%s): %w", failKind, err)
	}
	logStep("transcribe_ok", zap.Duration("elapsed", time.Since(transcribeStart)))

	asrDur := asr.ParseDurationMs(raw)
	minUttStart, maxUttEnd := utteranceBoundsMS(raw)
	scaledRaw := raw
	scaleFactor := 1.0
	if asr.ShouldScaleUtteranceTimestamps(asrDur, targetReadyMS, maxUttEnd, asr.DefaultScaleSkewThresholdMS) {
		scaled, scale, scaleErr := asr.ScaleTimestampsToDuration(raw, targetReadyMS)
		if scaleErr != nil {
			w.logger.Warn("主 MP4 全量 ASR 兜底缩放失败，沿用厂商时间戳",
				zap.Uint("material_id", material.ID),
				zap.Error(scaleErr),
			)
		} else {
			scaledRaw = scaled
			scaleFactor = scale
		}
	} else if rewritten, err := asr.SetAudioInfoDuration(raw, targetReadyMS); err == nil {
		scaledRaw = rewritten
	}
	w.emitAlignDiag(material, AlignDiagEvent{
		Event:         "asr_full_done",
		LocalPath:     mp4Path,
		ASRSource:     asrSource,
		ASRCursorMS:   material.ASRCursorMS,
		ReadyMS:       targetReadyMS,
		ChunkMS:       targetReadyMS,
		LeadPadMS:     align.LeadPadMs,
		TrimStartSec:  align.TrimStartSec,
		VendorDurMS:   asrDur,
		MinUttStartMS: minUttStart,
		MaxUttEndMS:   maxUttEnd,
		ScaleFactor:   scaleFactor,
	})
	return w.commitFullMasterASR(ctx, material, epoch, targetReadyMS, string(scaledRaw), asrDur, scaleFactor, asrStart)
}

// runWindowASR 兼容旧名：跟播路径已改为全量 master ASR。
func (w *liveIngestWorker) runWindowASR(ctx context.Context, material *model.LiveMaterial) error {
	return w.runFullMasterASR(ctx, material)
}

// commitFullMasterASR 覆盖写入 live_asr / asr_paragraphs，游标推进到本次识别的 targetReadyMS。
// 若提交前 master 已更长，保持 asr_due=true 以便再跑全量。
func (w *liveIngestWorker) commitFullMasterASR(
	ctx context.Context,
	material *model.LiveMaterial,
	epoch int64,
	targetReadyMS int64,
	liveASR string,
	asrVendorDurMS int64,
	scaleFactor float64,
	asrStart time.Time,
) error {
	currentReadyMS := targetReadyMS
	if latest, err := w.repo.GetByID(ctx, material.ID); err == nil && latest != nil {
		material.MediaWindows = latest.MediaWindows
		material.Duration = latest.Duration
		material.LiveURL = latest.LiveURL
		currentReadyMS = latest.MasterReadyMS()
		if currentReadyMS <= 0 {
			currentReadyMS = latest.Duration
		}
		if currentReadyMS < targetReadyMS {
			currentReadyMS = targetReadyMS
		}
	}

	stillPending := currentReadyMS > targetReadyMS
	progress := windowASRProgress(currentReadyMS, targetReadyMS)
	if !stillPending {
		progress = windowASRProgress(targetReadyMS, targetReadyMS)
	}

	w.logger.Info("主 MP4 全量 ASR 步骤",
		zap.Uint("material_id", material.ID),
		zap.String("step", "db_start"),
		zap.Int64("target_ready_ms", targetReadyMS),
		zap.Int64("current_ready_ms", currentReadyMS),
		zap.Bool("still_due", stillPending),
	)

	paras := w.rebuildLiveASRParagraphs(liveASR, targetReadyMS)
	// duration 写库用当前 master 就绪时长，避免把已变长的 duration 写短。
	durationMS := currentReadyMS
	if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, liveASR, targetReadyMS, durationMS, progress, stillPending, paras); err != nil {
		return err
	}
	material.ASRCursorMS = targetReadyMS
	material.LiveASR = liveASR
	material.ASRProgress = progress
	material.ASRDue = stillPending
	material.Duration = durationMS
	material.ASRParagraphs = paras
	w.logger.Info("主 MP4 全量 ASR 完成",
		zap.Uint("material_id", material.ID),
		zap.Duration("elapsed", time.Since(asrStart)),
		zap.Int64("target_ready_ms", targetReadyMS),
		zap.Int64("current_ready_ms", currentReadyMS),
		zap.Int64("asr_cursor_ms", targetReadyMS),
		zap.Int64("asr_vendor_duration_ms", asrVendorDurMS),
		zap.Float64("asr_time_scale", scaleFactor),
		zap.Int16("asr_progress", progress),
		zap.Bool("asr_due", stillPending),
		zap.Int("asr_paragraphs", len(paras)),
	)
	if stillPending {
		w.logger.Info("全量 ASR 跑输：master 已变长，保持 asr_due 等待下一轮全量",
			zap.Uint("material_id", material.ID),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Int64("current_ready_ms", currentReadyMS),
		)
	}
	return nil
}

// commitMasterASRProgress 保留给单测：模拟「覆盖写 + 游标推进」；跟播生产路径走 commitFullMasterASR。
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
	_ = thisChunkMS
	_ = cursor
	return w.commitFullMasterASR(ctx, material, epoch, readyMS, liveASR, asrVendorDurMS, scaleFactor, asrStart)
}

// rebuildLiveASRParagraphs 跟播中对当前 live_asr 全量重算 asr_paragraphs。
// 硬校验失败时降级保留 MinGap+finalize 结果，禁止写空 [] 导致 UI 回退碎句。
func (w *liveIngestWorker) rebuildLiveASRParagraphs(liveASR string, durationMs int64) []model.ASRParagraph {
	utterances := asr.FormatUtterancesForAPI(liveASR)
	if len(utterances) == 0 {
		return []model.ASRParagraph{}
	}
	paras, _, err := BuildASRParagraphsAlgo(utterances, durationMs, asrParagraphMaxRunes, w.logger)
	if err == nil {
		if paras == nil {
			return []model.ASRParagraph{}
		}
		return paras
	}
	w.logger.Warn("跟播重算 asr_paragraphs 校验失败，降级保留未硬校验结果",
		zap.Error(err),
		zap.Int("utterance_count", len(utterances)),
		zap.Int64("duration_ms", durationMs),
	)
	paras, _ = BuildASRParagraphsByMinGap(utterances, asrParagraphMaxRunes, w.logger)
	paras, _ = enforceASRParagraphMaxRunes(paras)
	finalizeASRParagraphTimeline(paras, durationMs)
	if paras == nil {
		return []model.ASRParagraph{}
	}
	return paras
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
