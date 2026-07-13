package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
)

const liveMaterialASRQueueSize = 256

// LiveMaterialASRWorker 直播素材 ASR 后台识别调度器。
type LiveMaterialASRWorker interface {
	// Enqueue 非阻塞入队，创建素材后触发后台识别。
	Enqueue(materialID uint)
	// Process 执行单条素材的 ASR 识别流程。
	Process(ctx context.Context, materialID uint) error
	// Start 启动后台调度循环。
	Start(ctx context.Context)
}

type liveMaterialASRWorker struct {
	repo           repository.LiveMaterialRepository
	asrService     ASRService
	audioPreparer  LiveMaterialASRAudioPreparer
	logger         *zap.Logger
	queue          chan uint
	startOnce      sync.Once
}

// NewLiveMaterialASRWorker 创建直播素材 ASR 后台 worker。
func NewLiveMaterialASRWorker(
	repo repository.LiveMaterialRepository,
	asrService ASRService,
	audioPreparer LiveMaterialASRAudioPreparer,
	logger *zap.Logger,
) LiveMaterialASRWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &liveMaterialASRWorker{
		repo:          repo,
		asrService:    asrService,
		audioPreparer: audioPreparer,
		logger:        logger,
		queue:         make(chan uint, liveMaterialASRQueueSize),
	}
}

func (w *liveMaterialASRWorker) Enqueue(materialID uint) {
	select {
	case w.queue <- materialID:
	default:
		// 队列满时异步等待入队，保证 Enqueue 对调用方非阻塞。
		go func() { w.queue <- materialID }()
	}
}

func (w *liveMaterialASRWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		go w.run(ctx)
	})
}

func (w *liveMaterialASRWorker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case materialID := <-w.queue:
			if err := w.Process(ctx, materialID); err != nil {
				// 常见失败场景已在 Process / failASR 内记录，此处仅兜底未显式打日志的错误。
				w.logger.Error("直播素材 ASR 任务结束（失败）",
					zap.Uint("material_id", materialID),
					zap.Error(err),
				)
			}
		}
	}
}

func (w *liveMaterialASRWorker) Process(ctx context.Context, materialID uint) error {
	material, err := w.repo.GetByID(ctx, materialID)
	if err != nil {
		w.logger.Error("直播素材 ASR 查询失败",
			zap.Uint("material_id", materialID),
			zap.Error(err),
		)
		return fmt.Errorf("查询素材失败: %w", err)
	}
	if material.ASRStatus != model.ASRStatusPending {
		w.logger.Debug("跳过非待处理素材的 ASR 任务",
			zap.Uint("material_id", materialID),
			zap.String("asr_status", string(material.ASRStatus)),
		)
		return nil
	}

	w.logger.Info("开始处理直播素材 ASR",
		zap.Uint("material_id", materialID),
		zap.String("live_url", material.LiveURL),
	)

	if err := w.repo.UpdateASRProcessing(ctx, materialID); err != nil {
		return fmt.Errorf("更新 ASR 处理中状态失败: %w", err)
	}

	var lastProgress int16 = 5
	updateProgress := func(progress int16) {
		if progress <= lastProgress {
			return
		}
		lastProgress = progress
		if updateErr := w.repo.UpdateASRProgress(ctx, materialID, progress); updateErr != nil {
			w.logger.Warn("更新 ASR 进度失败",
				zap.Uint("material_id", materialID),
				zap.Int16("progress", progress),
				zap.Error(updateErr),
			)
		}
	}

	// 下载源媒体 → 转标准 MP3 → 上传对象存储，得到 ASR 可用的公网音频 URL。
	w.logger.Info("开始音频预处理",
		zap.Uint("material_id", materialID),
		zap.String("source_url", material.LiveURL),
	)
	audioURL, cleanup, err := w.prepareAudioURL(ctx, materialID, material.LiveURL, updateProgress)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return w.failASR(ctx, materialID, lastProgress, err)
	}
	w.logger.Info("音频预处理完成",
		zap.Uint("material_id", materialID),
		zap.String("audio_url", audioURL),
	)

	w.logger.Info("开始 ASR 语音识别",
		zap.Uint("material_id", materialID),
		zap.String("audio_url", audioURL),
	)
	raw, err := w.asrService.TranscribeWithProgress(ctx, audioURL, func(progress int) {
		// ASR 轮询阶段映射到 50~95，与预处理阶段 5~45 衔接。
		mapped := int16(50 + progress*45/100)
		if mapped > 95 {
			mapped = 95
		}
		updateProgress(mapped)
	})
	if err != nil {
		return w.failASR(ctx, materialID, lastProgress, err)
	}

	duration := asr.ParseDurationMs(raw)
	liveASR := string(raw)
	if !json.Valid(raw) {
		liveASR = "{}"
	}

	if err := w.repo.UpdateASRCompleted(ctx, materialID, liveASR, duration); err != nil {
		return fmt.Errorf("写入 ASR 成功结果失败: %w", err)
	}
	w.logger.Info("直播素材 ASR 处理完成",
		zap.Uint("material_id", materialID),
		zap.Int64("duration_ms", duration),
	)
	return nil
}

// prepareAudioURL 调用音频预处理服务；未配置时回退为直接使用素材原始 URL。
func (w *liveMaterialASRWorker) prepareAudioURL(
	ctx context.Context,
	materialID uint,
	sourceURL string,
	updateProgress func(progress int16),
) (string, func(), error) {
	if w.audioPreparer == nil {
		return sourceURL, nil, nil
	}
	return w.audioPreparer.Prepare(ctx, materialID, sourceURL, updateProgress)
}

func (w *liveMaterialASRWorker) failASR(ctx context.Context, materialID uint, progress int16, err error) error {
	w.logger.Error("直播素材 ASR 流程失败",
		zap.Uint("material_id", materialID),
		zap.Int16("progress", progress),
		zap.Error(err),
	)
	if failErr := w.repo.UpdateASRFailed(ctx, materialID, progress, err.Error()); failErr != nil {
		return fmt.Errorf("ASR 失败且写入失败状态异常: asr=%w, db=%v", err, failErr)
	}
	return err
}
