package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseClipRanges(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantLen int
		wantErr bool
	}{
		{
			name:    "object",
			content: `{"clips":[{"start_time":100,"end_time":200},{"start_time":300,"end_time":500}]}`,
			wantLen: 2,
		},
		{
			name:    "array",
			content: `[{"start_time":0,"end_time":10}]`,
			wantLen: 1,
		},
		{
			name: "markdown",
			content: "```json\n{\"clips\":[{\"start_time\":1,\"end_time\":2}]}\n```",
			wantLen: 1,
		},
		{
			name:    "invalid range",
			content: `{"clips":[{"start_time":10,"end_time":5}]}`,
			wantErr: true,
		},
		{
			name:    "empty",
			content: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseClipRanges(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseClipRanges() error = %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestClient_Chat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != DefaultModel {
			t.Errorf("model = %q", req.Model)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: `{"clips":[{"start_time":0,"end_time":1000}]}`}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: srv.URL, Model: DefaultModel, HTTPClient: srv.Client()})
	content, err := client.Chat(context.Background(), []ChatMessage{
		{Role: "user", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if content == "" {
		t.Fatal("empty content")
	}

	ranges, err := ParseClipRanges(content)
	if err != nil || len(ranges) != 1 {
		t.Fatalf("parse clips = %v, err=%v", ranges, err)
	}
}

func TestClient_ChatStructured_SendsTempJSONModeAndDisablesThinking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Temperature == nil || *req.Temperature != 0 {
			t.Errorf("temperature = %v, want 0", req.Temperature)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format = %+v, want json_object", req.ResponseFormat)
		}
		if req.EnableThinking == nil || *req.EnableThinking {
			t.Errorf("enable_thinking = %v, want false", req.EnableThinking)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: `{"items":[]}`}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: srv.URL, Model: DefaultModel, HTTPClient: srv.Client()})
	content, err := client.ChatStructured(context.Background(), []ChatMessage{
		{Role: "user", Content: "return json"},
	})
	if err != nil {
		t.Fatalf("ChatStructured() error = %v", err)
	}
	if content != `{"items":[]}` {
		t.Errorf("content = %q", content)
	}
}

func TestClient_ChatThinking_EnablesThinking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.EnableThinking == nil || !*req.EnableThinking {
			t.Errorf("enable_thinking = %v, want true", req.EnableThinking)
		}
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: `[0,1]`}},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: srv.URL, Model: DefaultModel, HTTPClient: srv.Client()})
	content, err := client.ChatThinking(context.Background(), []ChatMessage{
		{Role: "user", Content: "pick clips"},
	})
	if err != nil {
		t.Fatalf("ChatThinking() error = %v", err)
	}
	if content != `[0,1]` {
		t.Errorf("content = %q", content)
	}
}

func TestClient_Chat_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(chatResponse{
			Error: &struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			}{Message: "invalid model"},
		})
	}))
	defer srv.Close()

	client := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestClient_ChatCompletions 验证返回上游完整 JSON，且保留 choices 等字段。
func TestClient_ChatCompletions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"model":   DefaultModel,
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "你好"}},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3},
		})
	}))
	defer srv.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: srv.URL, Model: DefaultModel, HTTPClient: srv.Client()})
	raw, err := client.ChatCompletions(context.Background(), []ChatMessage{
		{Role: "system", Content: "你是助手"},
		{Role: "user", Content: "你好"},
	})
	if err != nil {
		t.Fatalf("ChatCompletions() error = %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if data["id"] != "chatcmpl-test" {
		t.Errorf("id = %v, want chatcmpl-test", data["id"])
	}
	if data["usage"] == nil {
		t.Error("usage missing in full response")
	}
}

// TestClient_ChatCompletions_EmptyMessages 验证空 messages 被拒绝。
func TestClient_ChatCompletions_EmptyMessages(t *testing.T) {
	client := NewClient(Config{APIKey: "k", BaseURL: "http://example.com"})
	_, err := client.ChatCompletions(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty messages")
	}
}

// TestNewClient_ResolvesTimeout 验证超时回落与自带 HTTPClient 的优先级。
func TestNewClient_ResolvesTimeout(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want time.Duration
	}{
		{name: "未配置回落默认", cfg: Config{}, want: DefaultTimeout},
		{name: "非正值回落默认", cfg: Config{Timeout: -time.Second}, want: DefaultTimeout},
		{name: "显式配置生效", cfg: Config{Timeout: 30 * time.Second}, want: 30 * time.Second},
		{
			name: "自带 client 时以它的超时为准",
			cfg:  Config{Timeout: 30 * time.Second, HTTPClient: &http.Client{Timeout: 5 * time.Second}},
			want: 5 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.cfg)
			if got := client.Timeout(); got != tt.want {
				t.Fatalf("Timeout() = %v, want %v", got, tt.want)
			}
			if got := client.http.Timeout; got != tt.want {
				t.Fatalf("http.Timeout = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTimeoutReason 验证失败原因分类（决定错误信息里写「客户端超时」还是「调用方取消」）。
func TestTimeoutReason(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want string
	}{
		{name: "调用方 context 已取消", ctx: canceled, err: errors.New("boom"), want: "调用方 context 已取消或超时"},
		{name: "客户端超时", ctx: context.Background(), err: context.DeadlineExceeded, want: "LLM 客户端超时"},
		{
			name: "被 url.Error 包装的客户端超时",
			ctx:  context.Background(),
			err:  &url.Error{Op: "Post", URL: "https://example.com", Err: context.DeadlineExceeded},
			want: "LLM 客户端超时",
		},
		{name: "其它传输失败", ctx: context.Background(), err: errors.New("connection reset"), want: "传输失败"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timeoutReason(tt.ctx, tt.err); got != tt.want {
				t.Fatalf("timeoutReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClient_TimeoutErrorNamesConfiguredTimeout 验证超时错误自带「预算是多少 / 跑了多久」，
// 且仍然 unwrap 到 context.DeadlineExceeded（worker 与 ASR 重试都依赖这个判定）。
func TestClient_TimeoutErrorNamesConfiguredTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(chatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{{Message: ChatMessage{Role: "assistant", Content: "ok"}}},
		})
	}))
	defer srv.Close()

	const budget = 30 * time.Millisecond
	client := NewClient(Config{APIKey: "k", BaseURL: srv.URL, Model: DefaultModel, Timeout: budget})
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	for _, want := range []string{"请求 LLM 失败", "LLM 客户端超时", budget.String()} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want contains %q", msg, want)
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(DeadlineExceeded) = false, err = %v", err)
	}
}
