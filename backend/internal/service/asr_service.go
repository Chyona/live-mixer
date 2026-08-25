// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"live-mixer/internal/pkg/asr"
)

// ASRTranscriber 豆包 ASR 转写能力抽象，便于单元测试注入 mock。
type ASRTranscriber interface {
	Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error)
	TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error)
}

// ASRService 语音识别业务接口。
type ASRService interface {
	Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error)
	TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error)
}

type asrService struct {
	transcriber ASRTranscriber
}

// NewASRService 创建 ASR 业务服务实例。
func NewASRService(transcriber ASRTranscriber) ASRService {
	return &asrService{transcriber: transcriber}
}

// NewASRServiceFromConfig 根据应用配置创建 ASR 业务服务。
func NewASRServiceFromConfig(cfg asr.Config) ASRService {
	return NewASRService(asr.NewClient(cfg))
}

// Transcribe 同步转写公网音频 URL，返回豆包 ASR 完整 JSON 结果。
func (s *asrService) Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error) {
	return s.TranscribeWithProgress(ctx, audioURL, nil)
}

// TranscribeWithProgress 同步转写并在轮询时回调估算进度。
func (s *asrService) TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
	if s.transcriber == nil {
		return nil, fmt.Errorf("ASR 服务未初始化")
	}
	return s.transcriber.TranscribeWithProgress(ctx, audioURL, onProgress)
}
