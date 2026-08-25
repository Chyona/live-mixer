package service

import (
	"context"
	"encoding/json"
	"testing"

	"live-mixer/internal/pkg/asr"
)

type mockASRTranscriber struct {
	transcribeFn func(ctx context.Context, audioURL string) (json.RawMessage, error)
}

func (m *mockASRTranscriber) Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error) {
	return m.transcribeFn(ctx, audioURL)
}

func (m *mockASRTranscriber) TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
	if onProgress != nil {
		onProgress(5)
	}
	return m.transcribeFn(ctx, audioURL)
}

func TestASRService_Transcribe_Success(t *testing.T) {
	expected := json.RawMessage(`{"result":{"text":"你好"}}`)
	svc := NewASRService(&mockASRTranscriber{
		transcribeFn: func(ctx context.Context, audioURL string) (json.RawMessage, error) {
			if audioURL != "https://example.com/test.wav" {
				t.Errorf("audioURL = %q, want https://example.com/test.wav", audioURL)
			}
			return expected, nil
		},
	})

	got, err := svc.Transcribe(context.Background(), "https://example.com/test.wav")
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}
	if string(got) != string(expected) {
		t.Errorf("Transcribe() = %s, want %s", got, expected)
	}
}

func TestASRService_Transcribe_Error(t *testing.T) {
	svc := NewASRService(&mockASRTranscriber{
		transcribeFn: func(ctx context.Context, audioURL string) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		},
	})

	_, err := svc.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil {
		t.Fatal("Transcribe() error = nil, want error")
	}
}

func TestASRService_Transcribe_NotInitialized(t *testing.T) {
	svc := NewASRService(nil)
	_, err := svc.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil {
		t.Fatal("Transcribe() error = nil, want not initialized error")
	}
}
