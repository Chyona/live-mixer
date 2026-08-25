package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const (
	// liveMaterialASRDefaultConcurrency 单实例内并行抢占/执行 ASR 的 Worker 数。
	// 即同一进程最多可同时处理的视频 ASR 数量。
	liveMaterialASRDefaultConcurrency = 6
	// liveMaterialASRPollInterval 无待处理任务时的 DB 轮询间隔（多实例兜底唤醒）。
	liveMaterialASRPollInterval = 3 * time.Second
	// liveMaterialASRStaleTimeout processing 超时未更新进度则自动改回 pending。
	liveMaterialASRStaleTimeout = 60 * time.Minute
)

// LiveMaterialASRWorker 直播素材 ASR 后台识别调度器：DB 乐观锁抢占 + 定时 poll。
type LiveMaterialASRWorker interface {
	// Enqueue 非阻塞唤醒调度循环尝试领取任务。
	Enqueue()
	// Process 执行已抢占（processing）的 ASR 识别流程。
	Process(ctx context.Context, material *model.LiveMaterial) error
	// Start 启动后台调度循环。
	Start(ctx context.Context)
}

type liveMaterialASRWorker struct {
	repo          repository.LiveMaterialRepository
	asrService    ASRService
	audioPreparer LiveMaterialASRAudioPreparer
	llmClient     LLMChatClient
	web           webroot.Config
	logger        *zap.Logger
	concurrency   int
	pollInterval  time.Duration
	staleTimeout  time.Duration

	wake      chan struct{}
	startOnce sync.Once
}

// NewLiveMaterialASRWorker 创建直播素材 ASR 后台 worker。
// concurrency 为单实例并行 Worker 数；<=0 时使用内置默认值（6）。
// staleTimeout 为 processing 孤儿回收阈值；<=0 时使用内置默认值（60 分钟）。
// llmClient 用于 ASR 完成后的 summaries/paragraphs 后处理；可为 nil（后处理将失败）。
// web.RootDir 非空时将处理过程调试数据落盘到 staging/asr/。
func NewLiveMaterialASRWorker(
	repo repository.LiveMaterialRepository,
	asrService ASRService,
	audioPreparer LiveMaterialASRAudioPreparer,
	llmClient LLMChatClient,
	logger *zap.Logger,
	concurrency int,
	staleTimeout time.Duration,
	web webroot.Config,
) LiveMaterialASRWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if concurrency <= 0 {
		concurrency = liveMaterialASRDefaultConcurrency
	}
	if staleTimeout <= 0 {
		staleTimeout = liveMaterialASRStaleTimeout
	}
	return &liveMaterialASRWorker{
		repo:          repo,
		asrService:    asrService,
		audioPreparer: audioPreparer,
		llmClient:     llmClient,
		web:           web,
		logger:        logger,
		concurrency:   concurrency,
		pollInterval:  liveMaterialASRPollInterval,
		staleTimeout:  staleTimeout,
		wake:          make(chan struct{}, 1),
	}
}

// Enqueue 非阻塞唤醒调度器，创建/重试后调用以尽快领取。
func (w *liveMaterialASRWorker) Enqueue() {
	select {
	case w.wake <- struct{}{}:
	default:
		// 已有唤醒信号在队列中，无需重复投递。
	}
}

// Start 启动若干并发领取循环 + 定时 poll，并在启动时立即回收孤儿 processing 任务。
func (w *liveMaterialASRWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		n := w.concurrency
		if n <= 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			go w.loop(ctx, i)
		}
		go w.pollLoop(ctx)
		// 启动即回收：服务重启后 processing 孤儿无需等待第一个 poll 周期。
		w.requeueStale(ctx)
		w.Enqueue()
		w.logger.Info("直播素材 ASR Worker 已启动",
			zap.Int("concurrency", n),
			zap.Duration("stale_timeout", w.staleTimeout),
		)
	})
}

func (w *liveMaterialASRWorker) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.requeueStale(ctx)
			w.Enqueue()
		}
	}
}

// requeueStale 将 asr_updated_at 超时未刷新的 processing 任务改回 pending。
func (w *liveMaterialASRWorker) requeueStale(ctx context.Context) {
	n, err := w.repo.RequeueStaleProcessingASR(ctx, w.staleTimeout)
	if err != nil {
		w.logger.Warn("回收超时 ASR 任务失败", zap.Error(err))
		return
	}
	if n > 0 {
		w.logger.Warn("已将超时 ASR 任务重新排队", zap.Int64("count", n))
	}
}

func (w *liveMaterialASRWorker) loop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			w.drain(ctx, workerID)
		}
	}
}

// drain 循环抢占并处理，直到当前没有更多 pending ASR。
func (w *liveMaterialASRWorker) drain(ctx context.Context, workerID int) {
	for {
		if ctx.Err() != nil {
			return
		}
		material, err := w.repo.ClaimPendingASR(ctx)
		if err != nil {
			w.logger.Error("抢占直播素材 ASR 任务失败",
				zap.Int("worker_id", workerID),
				zap.Error(err),
			)
			return
		}
		if material == nil {
			return
		}
		w.logger.Info("已抢占直播素材 ASR 任务",
			zap.Int("worker_id", workerID),
			zap.Uint("material_id", material.ID),
			zap.Int64("asr_version", material.ASRVersion),
		)
		if err := w.Process(ctx, material); err != nil {
			w.logger.Error("直播素材 ASR 任务执行失败",
				zap.Uint("material_id", material.ID),
				zap.Error(err),
			)
		}
	}
}

// Process 执行已抢占素材的 ASR：音频预处理 → 识别 → LLM 后处理 → 写回结果。
func (w *liveMaterialASRWorker) Process(ctx context.Context, material *model.LiveMaterial) error {
	if material == nil {
		return fmt.Errorf("素材不能为空")
	}
	materialID := material.ID
	if material.ASRStatus != model.ASRStatusProcessing {
		w.logger.Debug("跳过非处理中素材的 ASR 任务",
			zap.Uint("material_id", materialID),
			zap.String("asr_status", string(material.ASRStatus)),
		)
		return nil
	}

	asrVersion := material.ASRVersion
	rec := newASRDebugRecorder(w.web.ASRStagingDir(materialID, asrVersion), w.logger)
	w.logger.Info("开始处理直播素材 ASR",
		zap.Uint("material_id", materialID),
		zap.Int64("asr_version", asrVersion),
		zap.String("live_url", material.LiveURL),
	)

	var lastProgress int16 = 5
	updateProgress := func(progress int16) {
		if progress <= lastProgress {
			return
		}
		lastProgress = progress
		if updateErr := w.repo.UpdateASRProgress(ctx, materialID, asrVersion, progress); updateErr != nil {
			w.logger.Warn("更新 ASR 进度失败",
				zap.Uint("material_id", materialID),
				zap.Int16("progress", progress),
				zap.Error(updateErr),
			)
		}
	}

	// 下载源媒体 → 探测分辨率 → 转标准 MP3 → 上传对象存储，得到 ASR 可用的公网音频 URL。
	w.logger.Info("开始音频预处理",
		zap.Uint("material_id", materialID),
		zap.String("source_url", material.LiveURL),
	)
	prep, err := w.prepareAudio(ctx, materialID, material.LiveURL, updateProgress)
	if prep.Cleanup != nil {
		defer prep.Cleanup()
	}
	if err != nil {
		return w.failASR(ctx, materialID, asrVersion, lastProgress, "prepare", rec, err)
	}
	rec.Write("001_prepare.json", map[string]any{
		"recorded_at": asrDebugRecordedAt(),
		"source_url":  material.LiveURL,
		"audio_url":   prep.AudioURL,
		"width":       prep.Width,
		"height":      prep.Height,
	})
	w.logger.Info("音频预处理完成",
		zap.Uint("material_id", materialID),
		zap.String("audio_url", prep.AudioURL),
		zap.Int("width", prep.Width),
		zap.Int("height", prep.Height),
	)

	w.logger.Info("开始 ASR 语音识别",
		zap.Uint("material_id", materialID),
		zap.String("audio_url", prep.AudioURL),
	)
	raw, err := w.asrService.TranscribeWithProgress(ctx, prep.AudioURL, func(progress int) {
		// ASR 轮询阶段映射到 50~95，与预处理阶段 5~45 衔接。
		mapped := int16(50 + progress*45/100)
		if mapped > 95 {
			mapped = 95
		}
		updateProgress(mapped)
	})
	if err != nil {
		return w.failASR(ctx, materialID, asrVersion, lastProgress, "asr", rec, err)
	}

	duration := asr.ParseDurationMs(raw)
	liveASR := string(raw)
	if !json.Valid(raw) {
		liveASR = "{}"
	}
	rec.Write("002_asr_raw.json", map[string]any{
		"recorded_at": asrDebugRecordedAt(),
		"duration_ms": duration,
		"live_asr":    json.RawMessage(liveASR),
	})

	updateProgress(96)
	w.logger.Info("开始 ASR LLM 后处理",
		zap.Uint("material_id", materialID),
	)
	post, err := runASRPostprocess(ctx, w.llmClient, liveASR, duration, rec, w.logger)
	if err != nil {
		return w.failASR(ctx, materialID, asrVersion, lastProgress, "postprocess", rec, err)
	}
	updateProgress(98)
	rec.Write("005_postprocess_result.json", map[string]any{
		"recorded_at": asrDebugRecordedAt(),
		"summaries":   post.Summaries,
		"paragraphs":  post.Paragraphs,
	})

	if err := w.repo.UpdateASRCompleted(
		ctx, materialID, asrVersion, liveASR, duration, prep.Width, prep.Height,
		post.Summaries, post.Paragraphs,
	); err != nil {
		return fmt.Errorf("写入 ASR 成功结果失败: %w", err)
	}
	w.logger.Info("直播素材 ASR 处理完成",
		zap.Uint("material_id", materialID),
		zap.Int64("duration_ms", duration),
		zap.Int("width", prep.Width),
		zap.Int("height", prep.Height),
		zap.Int("summaries", len(post.Summaries)),
		zap.Int("paragraphs", len(post.Paragraphs)),
	)
	return nil
}

// prepareAudio 调用音频预处理服务；未配置时回退为直接使用素材原始 URL（无法探测分辨率）。
func (w *liveMaterialASRWorker) prepareAudio(
	ctx context.Context,
	materialID uint,
	sourceURL string,
	updateProgress func(progress int16),
) (ASRAudioPrepareResult, error) {
	if w.audioPreparer == nil {
		return ASRAudioPrepareResult{AudioURL: sourceURL}, nil
	}
	return w.audioPreparer.Prepare(ctx, materialID, sourceURL, updateProgress)
}

func (w *liveMaterialASRWorker) failASR(ctx context.Context, materialID uint, asrVersion int64, progress int16, stage string, rec *asrDebugRecorder, err error) error {
	rec.Write("999_fail.json", map[string]any{
		"recorded_at": asrDebugRecordedAt(),
		"stage":       stage,
		"error":       err.Error(),
		"progress":    progress,
	})
	w.logger.Error("直播素材 ASR 流程失败",
		zap.Uint("material_id", materialID),
		zap.String("stage", stage),
		zap.Int16("progress", progress),
		zap.Error(err),
	)
	if failErr := w.repo.UpdateASRFailed(ctx, materialID, asrVersion, progress, err.Error()); failErr != nil {
		return fmt.Errorf("ASR 失败且写入失败状态异常: asr=%w, db=%v", err, failErr)
	}
	return err
}
