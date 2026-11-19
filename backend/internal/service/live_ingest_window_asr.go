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

// fullMasterASRTiming 单次全量 ASR 各阶段耗时，结束时打「耗时汇总」便于按窗评估。
type fullMasterASRTiming struct {
	StartedAt         time.Time
	WindowCount       int
	TargetReadyMS     int64
	ASRCursorBeforeMS int64
	ResolveElapsed    time.Duration
	MP3Elapsed        time.Duration
	MP3Bytes          int64
	UploadElapsed     time.Duration
	TranscribeElapsed time.Duration
	ScaleElapsed      time.Duration
	ParagraphsElapsed time.Duration
	DBElapsed         time.Duration
	Outcome           string // ok | silence | failed | skipped_caught_up
}

func (t fullMasterASRTiming) totalElapsed() time.Duration {
	if t.StartedAt.IsZero() {
		return 0
	}
	return time.Since(t.StartedAt)
}

// realtimeFactor 墙钟耗时 / 媒体时长；>1 表示 ASR 比实时慢。
func (t fullMasterASRTiming) realtimeFactor() float64 {
	if t.TargetReadyMS <= 0 {
		return 0
	}
	return t.totalElapsed().Seconds() / (float64(t.TargetReadyMS) / 1000.0)
}

func (w *liveIngestWorker) logFullMasterASRTiming(material *model.LiveMaterial, t fullMasterASRTiming, extra ...zap.Field) {
	fields := []zap.Field{
		zap.Uint("material_id", material.ID),
		zap.Int("window_count", t.WindowCount),
		zap.Int64("target_ready_ms", t.TargetReadyMS),
		zap.Duration("target_ready", time.Duration(t.TargetReadyMS)*time.Millisecond),
		zap.Int64("asr_cursor_before_ms", t.ASRCursorBeforeMS),
		zap.String("outcome", t.Outcome),
		zap.Duration("resolve_elapsed", t.ResolveElapsed),
		zap.Duration("mp3_elapsed", t.MP3Elapsed),
		zap.Int64("mp3_bytes", t.MP3Bytes),
		zap.Duration("upload_elapsed", t.UploadElapsed),
		zap.Duration("transcribe_elapsed", t.TranscribeElapsed),
		zap.Duration("scale_elapsed", t.ScaleElapsed),
		zap.Duration("paragraphs_elapsed", t.ParagraphsElapsed),
		zap.Duration("db_elapsed", t.DBElapsed),
		zap.Duration("total_elapsed", t.totalElapsed()),
		zap.Float64("realtime_factor", t.realtimeFactor()),
	}
	fields = append(fields, extra...)
	w.logger.Info("主 MP4 全量 ASR 耗时汇总", fields...)
}

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
	windowCount := material.ParsedMediaWindows().ReadyCount()
	if targetReadyMS <= 0 {
		w.logger.Info("主 MP4 全量 ASR 跳过：主文件尚未就绪",
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_cursor_ms", material.ASRCursorMS),
			zap.Int("window_count", windowCount),
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
		w.logFullMasterASRTiming(material, fullMasterASRTiming{
			StartedAt:         time.Now(),
			WindowCount:       windowCount,
			TargetReadyMS:     targetReadyMS,
			ASRCursorBeforeMS: targetReadyMS,
			Outcome:           "skipped_caught_up",
		})
		return nil
	}

	timing := fullMasterASRTiming{
		StartedAt:         time.Now(),
		WindowCount:       windowCount,
		TargetReadyMS:     targetReadyMS,
		ASRCursorBeforeMS: material.ASRCursorMS,
	}
	resolveStart := time.Now()
	logStep("resolve_full",
		zap.Int("window_count", windowCount),
		zap.Int64("target_ready_ms", targetReadyMS),
		zap.Duration("target_ready", time.Duration(targetReadyMS)*time.Millisecond),
		zap.Int64("asr_cursor_ms", material.ASRCursorMS),
	)
	// 厂商文档：单次 ≤5h / <512MB；10/30/60+ 分钟会显著增加耗时与费用（O(N²) 累计）。
	readyDur := time.Duration(targetReadyMS) * time.Millisecond
	if readyDur > asr.VendorMaxAudioDuration {
		w.logger.Error("主 MP4 全量 ASR 超过厂商时长上限，仍将尝试提交（失败不推进游标）",
			zap.Uint("material_id", material.ID),
			zap.Int("window_count", windowCount),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
			zap.Duration("vendor_max", asr.VendorMaxAudioDuration),
		)
	} else if readyDur >= 60*time.Minute {
		w.logger.Warn("主 MP4 全量 ASR ≥60 分钟，关注轮询超时与 512MB/拒识码",
			zap.Uint("material_id", material.ID),
			zap.Int("window_count", windowCount),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
		)
	} else if readyDur >= 30*time.Minute {
		w.logger.Warn("主 MP4 全量 ASR ≥30 分钟，关注厂商耗时与费用",
			zap.Uint("material_id", material.ID),
			zap.Int("window_count", windowCount),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
		)
	} else if readyDur >= 10*time.Minute {
		w.logger.Info("主 MP4 全量 ASR ≥10 分钟",
			zap.Uint("material_id", material.ID),
			zap.Int("window_count", windowCount),
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
		timing.Outcome = "failed"
		timing.ResolveElapsed = time.Since(resolveStart)
		w.logFullMasterASRTiming(material, timing, zap.String("error", err.Error()))
		return err
	}
	snapPath, snapErr := w.snapshotMasterMP4(mp4Path, material.ID, targetReadyMS)
	if snapErr != nil {
		timing.Outcome = "failed"
		timing.ResolveElapsed = time.Since(resolveStart)
		w.logFullMasterASRTiming(material, timing, zap.String("error", snapErr.Error()))
		return fmt.Errorf("快照主 MP4 失败: %w", snapErr)
	}
	defer os.Remove(snapPath)
	mp4Path = snapPath
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
	timing.ResolveElapsed = time.Since(resolveStart)

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
		zap.Int("window_count", windowCount),
		zap.String("mp4", mp4Path),
		zap.String("asr_source", asrSource),
		zap.Float64("start_sec", 0),
		zap.Float64("dur_sec", durSec),
		zap.Float64("target_dur_sec", align.TargetDurSec),
		zap.Int64("lead_pad_ms", align.LeadPadMs),
	)
	mp3Start := time.Now()
	if err := w.ffmpeg.ConvertRangeToASRMP3Aligned(ctx, mp4Path, tmpMP3, 0, durSec, align); err != nil {
		timing.Outcome = "failed"
		timing.MP3Elapsed = time.Since(mp3Start)
		w.logFullMasterASRTiming(material, timing, zap.String("error", err.Error()))
		return fmt.Errorf("从主 MP4 抽全量 ASR MP3 失败: %w", err)
	}
	defer os.Remove(tmpMP3)
	timing.MP3Elapsed = time.Since(mp3Start)
	if st, sterr := os.Stat(tmpMP3); sterr == nil {
		timing.MP3Bytes = st.Size()
	}
	logStep("mp3_ok",
		zap.Duration("elapsed", timing.MP3Elapsed),
		zap.Int64("mp3_bytes", timing.MP3Bytes),
		zap.Float64("target_dur_sec", align.TargetDurSec),
	)
	if w.storage == nil {
		timing.Outcome = "failed"
		w.logFullMasterASRTiming(material, timing, zap.String("error", "对象存储未配置"))
		return fmt.Errorf("对象存储未配置")
	}
	logStep("upload_start", zap.Int("window_count", windowCount), zap.Int64("mp3_bytes", timing.MP3Bytes))
	uploadStart := time.Now()
	objectKey := fmt.Sprintf("%s/live-asr-%d-master-full-%d.mp3", storage.SubDirTemp, material.ID, targetReadyMS)
	audioURL, err := w.storage.UploadFile(ctx, tmpMP3, objectKey)
	if err != nil {
		timing.Outcome = "failed"
		timing.UploadElapsed = time.Since(uploadStart)
		w.logFullMasterASRTiming(material, timing, zap.String("error", err.Error()))
		return err
	}
	timing.UploadElapsed = time.Since(uploadStart)
	logStep("upload_ok", zap.Duration("elapsed", timing.UploadElapsed))
	logStep("transcribe_start",
		zap.Int("window_count", windowCount),
		zap.Duration("target_ready", readyDur),
	)
	transcribeStart := time.Now()
	raw, err := w.asrService.Transcribe(ctx, audioURL)
	if err != nil {
		timing.TranscribeElapsed = time.Since(transcribeStart)
		if asr.IsSilenceAudioError(err) {
			timing.Outcome = "silence"
			w.logger.Warn("主 MP4 全量 ASR 静音，覆盖为空结果并推进游标",
				zap.Uint("material_id", material.ID),
				zap.Int("window_count", windowCount),
				zap.Int64("target_ready_ms", targetReadyMS),
				zap.Duration("transcribe_elapsed", timing.TranscribeElapsed),
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
			return w.commitFullMasterASR(ctx, material, epoch, targetReadyMS, empty, 0, 1.0, timing)
		}
		failKind := asr.ClassifyTranscribeFailure(err)
		timing.Outcome = "failed"
		w.logger.Warn("主 MP4 全量 ASR 识别失败，不推进游标、不覆盖 live_asr",
			zap.Uint("material_id", material.ID),
			zap.Int("window_count", windowCount),
			zap.Int64("target_ready_ms", targetReadyMS),
			zap.Duration("target_ready", readyDur),
			zap.Duration("transcribe_elapsed", timing.TranscribeElapsed),
			zap.Duration("total_elapsed", timing.totalElapsed()),
			zap.String("failure_kind", string(failKind)),
			zap.Error(err),
		)
		w.logFullMasterASRTiming(material, timing,
			zap.String("failure_kind", string(failKind)),
			zap.String("error", err.Error()),
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
	timing.TranscribeElapsed = time.Since(transcribeStart)
	logStep("transcribe_ok",
		zap.Duration("elapsed", timing.TranscribeElapsed),
		zap.Int("window_count", windowCount),
	)

	scaleStart := time.Now()
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
	timing.ScaleElapsed = time.Since(scaleStart)
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
	timing.Outcome = "ok"
	return w.commitFullMasterASR(ctx, material, epoch, targetReadyMS, string(scaledRaw), asrDur, scaleFactor, timing)
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
	timing fullMasterASRTiming,
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
		if timing.WindowCount <= 0 {
			timing.WindowCount = latest.ParsedMediaWindows().ReadyCount()
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
		zap.Int("window_count", timing.WindowCount),
		zap.Int64("target_ready_ms", targetReadyMS),
		zap.Int64("current_ready_ms", currentReadyMS),
		zap.Bool("still_due", stillPending),
	)

	paraStart := time.Now()
	paras := w.rebuildLiveASRParagraphs(liveASR, targetReadyMS)
	timing.ParagraphsElapsed = time.Since(paraStart)
	utteranceCount := len(asr.FormatUtterancesForAPI(liveASR))

	// duration 写库用当前 master 就绪时长，避免把已变长的 duration 写短。
	durationMS := currentReadyMS
	dbStart := time.Now()
	if err := w.repo.AppendWindowASR(ctx, material.ID, epoch, liveASR, targetReadyMS, durationMS, progress, stillPending, paras); err != nil {
		if timing.Outcome == "" {
			timing.Outcome = "failed"
		}
		timing.DBElapsed = time.Since(dbStart)
		w.logFullMasterASRTiming(material, timing, zap.String("error", err.Error()))
		return err
	}
	timing.DBElapsed = time.Since(dbStart)
	if timing.Outcome == "" {
		timing.Outcome = "ok"
	}
	material.ASRCursorMS = targetReadyMS
	material.LiveASR = liveASR
	material.ASRProgress = progress
	material.ASRDue = stillPending
	material.Duration = durationMS
	material.ASRParagraphs = paras
	w.logger.Info("主 MP4 全量 ASR 完成",
		zap.Uint("material_id", material.ID),
		zap.Int("window_count", timing.WindowCount),
		zap.Duration("elapsed", timing.totalElapsed()),
		zap.Duration("mp3_elapsed", timing.MP3Elapsed),
		zap.Duration("upload_elapsed", timing.UploadElapsed),
		zap.Duration("transcribe_elapsed", timing.TranscribeElapsed),
		zap.Duration("paragraphs_elapsed", timing.ParagraphsElapsed),
		zap.Duration("db_elapsed", timing.DBElapsed),
		zap.Float64("realtime_factor", timing.realtimeFactor()),
		zap.Int64("target_ready_ms", targetReadyMS),
		zap.Int64("current_ready_ms", currentReadyMS),
		zap.Int64("asr_cursor_ms", targetReadyMS),
		zap.Int64("asr_vendor_duration_ms", asrVendorDurMS),
		zap.Float64("asr_time_scale", scaleFactor),
		zap.Int16("asr_progress", progress),
		zap.Bool("asr_due", stillPending),
		zap.Int("asr_utterances", utteranceCount),
		zap.Int("asr_paragraphs", len(paras)),
		zap.Int64("mp3_bytes", timing.MP3Bytes),
		zap.String("outcome", timing.Outcome),
	)
	w.logFullMasterASRTiming(material, timing,
		zap.Int64("current_ready_ms", currentReadyMS),
		zap.Int64("asr_cursor_ms", targetReadyMS),
		zap.Int64("asr_vendor_duration_ms", asrVendorDurMS),
		zap.Float64("asr_time_scale", scaleFactor),
		zap.Int16("asr_progress", progress),
		zap.Bool("asr_due", stillPending),
		zap.Int("asr_utterances", utteranceCount),
		zap.Int("asr_paragraphs", len(paras)),
	)
	if stillPending {
		w.logger.Info("全量 ASR 跑输：master 已变长，保持 asr_due 等待下一轮全量",
			zap.Uint("material_id", material.ID),
			zap.Int("window_count", timing.WindowCount),
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
	timing := fullMasterASRTiming{
		StartedAt:     asrStart,
		TargetReadyMS: readyMS,
		Outcome:       "ok",
	}
	return w.commitFullMasterASR(ctx, material, epoch, readyMS, liveASR, asrVendorDurMS, scaleFactor, timing)
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
