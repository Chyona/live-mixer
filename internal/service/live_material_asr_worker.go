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
	repo       repository.LiveMaterialRepository
	asrService ASRService
	logger     *zap.Logger
	queue      chan uint
	startOnce  sync.Once
}

// NewLiveMaterialASRWorker 创建直播素材 ASR 后台 worker。
func NewLiveMaterialASRWorker(
	repo repository.LiveMaterialRepository,
	asrService ASRService,
	logger *zap.Logger,
) LiveMaterialASRWorker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &liveMaterialASRWorker{
		repo:       repo,
		asrService: asrService,
		logger:     logger,
		queue:      make(chan uint, liveMaterialASRQueueSize),
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
				w.logger.Error("直播素材 ASR 处理失败",
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
		return fmt.Errorf("查询素材失败: %w", err)
	}
	if material.ASRStatus != model.ASRStatusPending {
		return nil
	}

	if err := w.repo.UpdateASRProcessing(ctx, materialID); err != nil {
		return fmt.Errorf("更新 ASR 处理中状态失败: %w", err)
	}

	var lastProgress int16 = 5
	raw, err := w.asrService.TranscribeWithProgress(ctx, material.LiveURL, func(progress int) {
		p := int16(progress)
		if p == lastProgress {
			return
		}
		lastProgress = p
		if updateErr := w.repo.UpdateASRProgress(ctx, materialID, p); updateErr != nil {
			w.logger.Warn("更新 ASR 进度失败",
				zap.Uint("material_id", materialID),
				zap.Int16("progress", p),
				zap.Error(updateErr),
			)
		}
	})
	if err != nil {
		if failErr := w.repo.UpdateASRFailed(ctx, materialID, lastProgress, err.Error()); failErr != nil {
			return fmt.Errorf("ASR 失败且写入失败状态异常: asr=%w, db=%v", err, failErr)
		}
		return err
	}

	duration := asr.ParseDurationMs(raw)
	liveASR := string(raw)
	if !json.Valid(raw) {
		liveASR = "{}"
	}

	if err := w.repo.UpdateASRCompleted(ctx, materialID, liveASR, duration); err != nil {
		return fmt.Errorf("写入 ASR 成功结果失败: %w", err)
	}
	return nil
}
