package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"live-mixer/internal/pkg/llm"

	"go.uber.org/zap"
)

// ErrInvalidChatRequest 表示对话请求参数不合法（应由 HTTP 层映射为 400）。
var ErrInvalidChatRequest = errors.New("无效的对话请求")

// LLMCompletionsClient 大模型 Chat Completions 能力抽象，便于单元测试注入 mock。
type LLMCompletionsClient interface {
	// ChatCompletions 调用上游并返回完整 JSON 响应体。
	ChatCompletions(ctx context.Context, messages []llm.ChatMessage) (json.RawMessage, error)
	// Model 返回当前配置的模型名，用于日志。
	Model() string
}

// ChatService 同步大模型对话业务接口。
type ChatService interface {
	// Chat 根据系统提示词与用户提示词调用 LLM，返回模型完整响应 JSON。
	Chat(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error)
}

type chatService struct {
	client LLMCompletionsClient
	logger *zap.Logger
}

// NewChatService 创建同步对话业务服务。
func NewChatService(client LLMCompletionsClient, logger *zap.Logger) ChatService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &chatService{client: client, logger: logger}
}

// Chat 组装 messages 并调用 LLM；sys_prompt 为空时仅发送用户消息。
func (s *chatService) Chat(ctx context.Context, sysPrompt, usrPrompt string) (json.RawMessage, error) {
	if s.client == nil {
		return nil, fmt.Errorf("LLM 服务未初始化")
	}

	usrPrompt = strings.TrimSpace(usrPrompt)
	if usrPrompt == "" {
		return nil, fmt.Errorf("%w: usr_prompt 不能为空", ErrInvalidChatRequest)
	}

	messages := make([]llm.ChatMessage, 0, 2)
	sysPrompt = strings.TrimSpace(sysPrompt)
	if sysPrompt != "" {
		messages = append(messages, llm.ChatMessage{Role: "system", Content: sysPrompt})
	}
	messages = append(messages, llm.ChatMessage{Role: "user", Content: usrPrompt})

	model := s.client.Model()
	s.logger.Info("开始调用 LLM",
		zap.String("model", model),
		zap.Int("message_count", len(messages)),
		zap.Bool("has_sys_prompt", sysPrompt != ""),
		zap.Int("usr_prompt_runes", utf8.RuneCountInString(usrPrompt)),
	)

	raw, err := s.client.ChatCompletions(ctx, messages)
	if err != nil {
		s.logger.Error("LLM 调用失败",
			zap.String("model", model),
			zap.Error(err),
		)
		return nil, err
	}

	s.logger.Info("LLM 调用成功",
		zap.String("model", model),
		zap.Int("response_bytes", len(raw)),
	)
	return raw, nil
}
