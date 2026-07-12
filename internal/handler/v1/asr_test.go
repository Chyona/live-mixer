package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"live-mixer/internal/pkg/asr"

	"github.com/gin-gonic/gin"
)

type mockASRService struct {
	transcribeFn func(ctx context.Context, audioURL string) (json.RawMessage, error)
}

func (m *mockASRService) Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error) {
	return m.transcribeFn(ctx, audioURL)
}

func (m *mockASRService) TranscribeWithProgress(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
	return m.transcribeFn(ctx, audioURL)
}

func TestASRHandler_Transcribe_Success(t *testing.T) {
	handler := NewASRHandler(&mockASRService{
		transcribeFn: func(ctx context.Context, audioURL string) (json.RawMessage, error) {
			return json.RawMessage(`{"result":{"text":"你好"}}`), nil
		},
	})

	r := gin.New()
	r.POST("/asr", handler.Transcribe)

	body := []byte(`{"audio_url":"https://example.com/test.wav"}`)
	req := httptest.NewRequest(http.MethodPost, "/asr", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Code != 0 {
		t.Errorf("code = %d, want 0", resp.Code)
	}
	if resp.Data["result"] == nil {
		t.Errorf("data.result missing: %+v", resp.Data)
	}
}

func TestASRHandler_Transcribe_MissingAudioURL(t *testing.T) {
	handler := NewASRHandler(&mockASRService{})

	r := gin.New()
	r.POST("/asr", handler.Transcribe)

	req := httptest.NewRequest(http.MethodPost, "/asr", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestASRHandler_Transcribe_ServiceError(t *testing.T) {
	handler := NewASRHandler(&mockASRService{
		transcribeFn: func(ctx context.Context, audioURL string) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		},
	})

	r := gin.New()
	r.POST("/asr", handler.Transcribe)

	body := []byte(`{"audio_url":"https://example.com/test.wav"}`)
	req := httptest.NewRequest(http.MethodPost, "/asr", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestASRHandler_Transcribe_InvalidJSONResult(t *testing.T) {
	handler := NewASRHandler(&mockASRService{
		transcribeFn: func(ctx context.Context, audioURL string) (json.RawMessage, error) {
			return json.RawMessage(`not-json`), nil
		},
	})

	r := gin.New()
	r.POST("/asr", handler.Transcribe)

	body := []byte(`{"audio_url":"https://example.com/test.wav"}`)
	req := httptest.NewRequest(http.MethodPost, "/asr", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
