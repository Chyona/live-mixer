package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

type workerMockRepo struct {
	materials map[uint]*model.LiveMaterial
}

func (m *workerMockRepo) Create(ctx context.Context, material *model.LiveMaterial) error { return nil }
func (m *workerMockRepo) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	material, ok := m.materials[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	stored := *material
	return &stored, nil
}
func (m *workerMockRepo) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	return nil
}
func (m *workerMockRepo) List(ctx context.Context, filter repository.LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	return nil, 0, nil
}

func (m *workerMockRepo) Delete(ctx context.Context, id uint) error { return nil }

func (m *workerMockRepo) UpdateASRProcessing(ctx context.Context, id uint) error {
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	material.ASRStatus = model.ASRStatusProcessing
	material.ASRProgress = 5
	material.ASRErrorMsg = ""
	return nil
}

func (m *workerMockRepo) UpdateASRProgress(ctx context.Context, id uint, progress int16) error {
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	material.ASRProgress = progress
	return nil
}

func (m *workerMockRepo) UpdateASRCompleted(ctx context.Context, id uint, liveASR string, duration int64) error {
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	material.ASRStatus = model.ASRStatusCompleted
	material.ASRProgress = 100
	material.LiveASR = liveASR
	material.Duration = duration
	return nil
}

func (m *workerMockRepo) UpdateASRFailed(ctx context.Context, id uint, progress int16, errorMsg string) error {
	material := m.materials[id]
	if material == nil {
		return gorm.ErrRecordNotFound
	}
	material.ASRStatus = model.ASRStatusFailed
	material.ASRProgress = progress
	material.ASRErrorMsg = errorMsg
	return nil
}

type workerMockASR struct {
	transcribeFn func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error)
}

type mockASRAudioPreparer struct {
	prepareFn func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error)
}

func (m *mockASRAudioPreparer) Prepare(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error) {
	return m.prepareFn(ctx, materialID, sourceURL, onProgress)
}

func (m *workerMockASR) Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error) {
	return m.TranscribeWithProgress(ctx, audioURL, nil)
}

func (m *workerMockASR) TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
	if m.transcribeFn != nil {
		return m.transcribeFn(ctx, audioURL, onProgress)
	}
	return nil, nil
}

func TestLiveMaterialASRWorker_Process_Success(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			1: {
				ID:        1,
				LiveURL:   "https://example.com/live.mp4",
				ASRStatus: model.ASRStatusPending,
			},
		},
	}
	var asrAudioURL string
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			asrAudioURL = audioURL
			if onProgress != nil {
				onProgress(50)
			}
			return json.RawMessage(`{"audio_info":{"duration":1200},"result":{"text":"hello"}}`), nil
		},
	}
	preparer := &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error) {
			if sourceURL != "https://example.com/live.mp4" {
				t.Errorf("sourceURL = %q, want live mp4 url", sourceURL)
			}
			if onProgress != nil {
				onProgress(45)
			}
			return "https://bucket.example.com/video_editing/temp/asr/1/test.mp3", func() {}, nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, preparer, nil)

	if err := worker.Process(context.Background(), 1); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	material := repo.materials[1]
	if material.ASRStatus != model.ASRStatusCompleted {
		t.Errorf("ASRStatus = %q, want completed", material.ASRStatus)
	}
	if material.ASRProgress != 100 {
		t.Errorf("ASRProgress = %d, want 100", material.ASRProgress)
	}
	if material.Duration != 1200 {
		t.Errorf("Duration = %d, want 1200", material.Duration)
	}
	if material.LiveASR == "" {
		t.Error("LiveASR should not be empty")
	}
	if asrAudioURL != "https://bucket.example.com/video_editing/temp/asr/1/test.mp3" {
		t.Errorf("ASR audio URL = %q, want uploaded mp3 url", asrAudioURL)
	}
}

func TestLiveMaterialASRWorker_Process_Failed(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			2: {
				ID:        2,
				LiveURL:   "https://example.com/live.mp3",
				ASRStatus: model.ASRStatusPending,
			},
		},
	}
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			if onProgress != nil {
				onProgress(20)
			}
			return nil, errors.New("ASR 提交失败")
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error) {
			return "https://bucket.example.com/temp/asr.mp3", func() {}, nil
		},
	}, nil)

	if err := worker.Process(context.Background(), 2); err == nil {
		t.Fatal("Process() error = nil, want error")
	}

	material := repo.materials[2]
	if material.ASRStatus != model.ASRStatusFailed {
		t.Errorf("ASRStatus = %q, want failed", material.ASRStatus)
	}
	if material.ASRErrorMsg == "" {
		t.Error("ASRErrorMsg should not be empty")
	}
}

func TestLiveMaterialASRWorker_Process_SkipNonPending(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			3: {ID: 3, ASRStatus: model.ASRStatusCompleted},
		},
	}
	called := false
	prepareCalled := false
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			called = true
			return nil, nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error) {
			prepareCalled = true
			return "https://bucket.example.com/temp/asr.mp3", func() {}, nil
		},
	}, nil)

	if err := worker.Process(context.Background(), 3); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if called || prepareCalled {
		t.Error("ASR and preparer should not be called for non-pending material")
	}
}

func TestLiveMaterialASRWorker_Process_PrepareFailed(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			4: {ID: 4, LiveURL: "https://example.com/live.mp4", ASRStatus: model.ASRStatusPending},
		},
	}
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{}, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error) {
			return "", nil, errors.New("下载直播素材失败")
		},
	}, nil)

	if err := worker.Process(context.Background(), 4); err == nil {
		t.Fatal("Process() error = nil, want prepare error")
	}
	if repo.materials[4].ASRStatus != model.ASRStatusFailed {
		t.Errorf("ASRStatus = %q, want failed", repo.materials[4].ASRStatus)
	}
}

func TestLiveMaterialASRWorker_Process_FallbackWithoutPreparer(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			5: {ID: 5, LiveURL: "https://example.com/direct.mp3", ASRStatus: model.ASRStatusPending},
		},
	}
	var asrURL string
	worker := NewLiveMaterialASRWorker(repo, &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			asrURL = audioURL
			return json.RawMessage(`{"audio_info":{"duration":1}}`), nil
		},
	}, nil, nil)

	if err := worker.Process(context.Background(), 5); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if asrURL != "https://example.com/direct.mp3" {
		t.Errorf("ASR URL = %q, want original live url", asrURL)
	}
}
