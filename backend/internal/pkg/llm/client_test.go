package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
