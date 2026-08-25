package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"live-mixer/internal/pkg/llm"

	"go.uber.org/zap"
)

// mockLLMCompletions 用于 ChatService 单元测试的 LLM mock。
type mockLLMCompletions struct {
	model        string
	chatFn       func(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error)
	lastMessages []llm.ChatMessage
}

func (m *mockLLMCompletions) ChatCompletions(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error) {
	m.lastMessages = append([]llm.ChatMessage(nil), messages...)
	return m.chatFn(ctx, messages)
}

func (m *mockLLMCompletions) Model() string {
	if m.model == "" {
		return "test-model"
	}
	return m.model
}

// TestChatService_Chat_WithSysPrompt 验证同时传入系统与用户提示词时消息组装正确。
func TestChatService_Chat_WithSysPrompt(t *testing.T) {
	expected := json.RawMessage(`{"id":"1","choices":[{"message":{"content":"ok"}}]}`)
	mock := &mockLLMCompletions{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error) {
			return expected, nil
		},
	}
	svc := NewChatService(mock, zap.NewNop())

	got, err := svc.Chat(context.Background(), "你是助手", "你好")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if string(got) != string(expected) {
		t.Errorf("Chat() = %s, want %s", got, expected)
	}
	if len(mock.lastMessages) != 2 {
		t.Fatalf("message count = %d, want 2", len(mock.lastMessages))
	}
	if mock.lastMessages[0].Role != "system" || mock.lastMessages[0].Content != "你是助手" {
		t.Errorf("system message = %+v", mock.lastMessages[0])
	}
	if mock.lastMessages[1].Role != "user" || mock.lastMessages[1].Content != "你好" {
		t.Errorf("user message = %+v", mock.lastMessages[1])
	}
}

// TestChatService_Chat_EmptySysPrompt 验证系统提示词为空时仅发送用户消息。
func TestChatService_Chat_EmptySysPrompt(t *testing.T) {
	mock := &mockLLMCompletions{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
	}
	svc := NewChatService(mock, zap.NewNop())

	_, err := svc.Chat(context.Background(), "  ", " 用户问题 ")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if len(mock.lastMessages) != 1 {
		t.Fatalf("message count = %d, want 1", len(mock.lastMessages))
	}
	if mock.lastMessages[0].Role != "user" || mock.lastMessages[0].Content != "用户问题" {
		t.Errorf("user message = %+v", mock.lastMessages[0])
	}
}

// TestChatService_Chat_EmptyUsrPrompt 验证用户提示词为空时返回错误。
func TestChatService_Chat_EmptyUsrPrompt(t *testing.T) {
	svc := NewChatService(&mockLLMCompletions{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error) {
			t.Fatal("should not call LLM")
			return nil, nil
		},
	}, zap.NewNop())

	_, err := svc.Chat(context.Background(), "sys", "  ")
	if err == nil {
		t.Fatal("Chat() error = nil, want error")
	}
	if !errors.Is(err, ErrInvalidChatRequest) {
		t.Errorf("Chat() error = %v, want ErrInvalidChatRequest", err)
	}
}

// TestChatService_Chat_ClientError 验证上游失败时透传错误。
func TestChatService_Chat_ClientError(t *testing.T) {
	svc := NewChatService(&mockLLMCompletions{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error) {
			return nil, errors.New("upstream timeout")
		},
	}, zap.NewNop())

	_, err := svc.Chat(context.Background(), "", "hello")
	if err == nil || err.Error() != "upstream timeout" {
		t.Fatalf("Chat() error = %v, want upstream timeout", err)
	}
}

// TestChatService_Chat_NotInitialized 验证客户端未注入时返回错误。
func TestChatService_Chat_NotInitialized(t *testing.T) {
	svc := NewChatService(nil, zap.NewNop())
	_, err := svc.Chat(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("Chat() error = nil, want not initialized error")
	}
}
