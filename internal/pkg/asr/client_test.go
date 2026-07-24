package asr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_Transcribe_Success(t *testing.T) {
	var queryCount atomic.Int32
	expected := map[string]interface{}{
		"audio_info": map[string]interface{}{"duration": 1000},
		"result":     map[string]interface{}{"text": "你好"},
	}
	expectedBody, _ := json.Marshal(expected)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/submit"):
			if got := r.Header.Get("X-Api-Key"); got != "test-key" {
				t.Errorf("X-Api-Key = %q, want test-key", got)
			}
			if got := r.Header.Get("X-Api-Resource-Id"); got != DefaultResourceID {
				t.Errorf("X-Api-Resource-Id = %q, want %s", got, DefaultResourceID)
			}
			if r.Header.Get("X-Api-Request-Id") == "" {
				t.Error("X-Api-Request-Id should not be empty")
			}
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/query"):
			n := queryCount.Add(1)
			if n == 1 {
				w.Header().Set(headerStatusCode, statusProcessing1)
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(expectedBody)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: 5 * time.Millisecond,
		MaxPolls:     5,
	})

	raw, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err != nil {
		t.Fatalf("Transcribe() error = %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["result"] == nil {
		t.Fatalf("result missing in response: %s", string(raw))
	}
	if queryCount.Load() < 2 {
		t.Errorf("queryCount = %d, want >= 2", queryCount.Load())
	}
}

func TestClient_Transcribe_SubmitFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerStatusCode, "40000001")
		w.Header().Set(headerMessage, "invalid key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	_, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil || !strings.Contains(err.Error(), "ASR 提交失败") {
		t.Fatalf("Transcribe() error = %v, want submit failure", err)
	}
}

func TestClient_Transcribe_QueryFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerStatusCode, "40000002")
		w.Header().Set(headerMessage, "task error")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: time.Millisecond,
		MaxPolls:     2,
	})

	_, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil || !strings.Contains(err.Error(), "ASR 查询失败") {
		t.Fatalf("Transcribe() error = %v, want query failure", err)
	}
}

// TestClient_Transcribe_QueryTimeoutThenSuccess 单次 query 读超时后应继续轮询并最终成功。
func TestClient_Transcribe_QueryTimeoutThenSuccess(t *testing.T) {
	var queryCount atomic.Int32
	expected := map[string]interface{}{
		"audio_info": map[string]interface{}{"duration": 2000},
		"result":     map[string]interface{}{"text": "超时后成功"},
	}
	expectedBody, _ := json.Marshal(expected)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/query") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		n := queryCount.Add(1)
		if n == 1 {
			// 超过 HTTPClient.Timeout，触发 Client.Timeout / deadline exceeded。
			time.Sleep(80 * time.Millisecond)
			w.Header().Set(headerStatusCode, statusProcessing1)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerStatusCode, statusSuccess)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(expectedBody)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: 5 * time.Millisecond,
		MaxPolls:     5,
		HTTPClient:   &http.Client{Timeout: 30 * time.Millisecond},
	})

	raw, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err != nil {
		t.Fatalf("Transcribe() error = %v, want success after transient timeout", err)
	}
	if queryCount.Load() < 2 {
		t.Errorf("queryCount = %d, want >= 2 (at least one timeout + one success)", queryCount.Load())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["result"] == nil {
		t.Fatalf("result missing in response: %s", string(raw))
	}
}

// TestClient_Transcribe_QueryConnectionDropThenSuccess 连接被中断后应软重试并成功。
func TestClient_Transcribe_QueryConnectionDropThenSuccess(t *testing.T) {
	var queryCount atomic.Int32
	expectedBody := []byte(`{"result":{"text":"ok"}}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			return
		}
		n := queryCount.Add(1)
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("Hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set(headerStatusCode, statusSuccess)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(expectedBody)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: 5 * time.Millisecond,
		MaxPolls:     5,
	})

	raw, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err != nil {
		t.Fatalf("Transcribe() error = %v, want success after connection drop", err)
	}
	if queryCount.Load() < 2 {
		t.Errorf("queryCount = %d, want >= 2", queryCount.Load())
	}
	if !json.Valid(raw) {
		t.Fatalf("invalid result JSON: %s", string(raw))
	}
}

// TestClient_Transcribe_QueryTimeoutExhaustsMaxPolls 连续瞬时超时耗尽 MaxPolls 仍返回轮询超时。
func TestClient_Transcribe_QueryTimeoutExhaustsMaxPolls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			return
		}
		time.Sleep(50 * time.Millisecond)
		w.Header().Set(headerStatusCode, statusProcessing1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: time.Millisecond,
		MaxPolls:     2,
		HTTPClient:   &http.Client{Timeout: 20 * time.Millisecond},
	})

	_, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil || !strings.Contains(err.Error(), "轮询超时") {
		t.Fatalf("Transcribe() error = %v, want 轮询超时", err)
	}
}

func TestIsTransientQueryError(t *testing.T) {
	ctx := context.Background()
	if isTransientQueryError(ctx, nil) {
		t.Error("nil should not be transient")
	}
	if !isTransientQueryError(ctx, context.DeadlineExceeded) {
		t.Error("DeadlineExceeded with live parent ctx should be transient")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if isTransientQueryError(canceled, context.DeadlineExceeded) {
		t.Error("any error under canceled parent ctx should not be transient")
	}
	if isTransientQueryError(ctx, context.Canceled) {
		t.Error("Canceled should not be transient")
	}
	if isTransientQueryError(ctx, fmt.Errorf("ASR 查询失败: 40000002 task error")) {
		t.Error("business query failure should not be transient")
	}
}

func TestClient_Transcribe_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerStatusCode, statusProcessing1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: time.Millisecond,
		MaxPolls:     2,
	})

	_, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil || !strings.Contains(err.Error(), "轮询超时") {
		t.Fatalf("Transcribe() error = %v, want timeout", err)
	}
}

func TestClient_Transcribe_MissingAPIKey(t *testing.T) {
	client := NewClient(Config{})
	_, err := client.Transcribe(context.Background(), "https://example.com/test.wav")
	if err == nil || !strings.Contains(err.Error(), "API Key 未配置") {
		t.Fatalf("Transcribe() error = %v, want missing api key", err)
	}
}

func TestClient_Transcribe_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			w.Header().Set(headerStatusCode, statusSuccess)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set(headerStatusCode, statusProcessing1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		PollInterval: 50 * time.Millisecond,
		MaxPolls:     10,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Transcribe(ctx, "https://example.com/test.wav")
	if err == nil {
		t.Fatal("Transcribe() error = nil, want context canceled")
	}
}

func TestCalcPollProgress(t *testing.T) {
	if got := calcPollProgress(0, 360); got != 5 {
		t.Errorf("progress(0) = %d, want 5", got)
	}
	if got := calcPollProgress(360, 360); got != 95 {
		t.Errorf("progress(360) = %d, want 95", got)
	}
}

func TestParseDurationMs(t *testing.T) {
	raw := json.RawMessage(`{"audio_info":{"duration":1500}}`)
	if got := ParseDurationMs(raw); got != 1500 {
		t.Errorf("ParseDurationMs() = %d, want 1500", got)
	}
}

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(Config{APIKey: "k"})
	if client.cfg.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", client.cfg.BaseURL, DefaultBaseURL)
	}
	if client.cfg.ResourceID != DefaultResourceID {
		t.Errorf("ResourceID = %q, want %q", client.cfg.ResourceID, DefaultResourceID)
	}
	if client.cfg.PollInterval != defaultPollInterval {
		t.Errorf("PollInterval = %v, want %v", client.cfg.PollInterval, defaultPollInterval)
	}
	if client.cfg.MaxPolls != defaultMaxPolls {
		t.Errorf("MaxPolls = %d, want %d", client.cfg.MaxPolls, defaultMaxPolls)
	}
}
