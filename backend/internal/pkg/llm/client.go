package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBaseURL 阿里云 DashScope OpenAI 兼容接口默认地址。
	DefaultBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// DefaultModel AI 切片默认模型。
	DefaultModel = "qwen3.7-plus"
	// DefaultTimeout 单次 Chat Completions 的超时。
	// 注意这是「整次生成」的预算：请求非流式，响应头要等模型生成完才返回，所以耗时长等于生成耗时。
	// AI 切片是思考模式 + clips0 全量 ASR（440~570 句段），线上实测单次 391~526 秒（6.5~8.8 分钟），
	// 旧的 600 秒会随机踩线（2026-09-28 一键成片 3/10 被自己掐死，报 "awaiting headers"）。
	// 默认给到 30 分钟；可用 llm.timeout_sec / APP_LLM_TIMEOUT_SEC 调整。
	DefaultTimeout = 30 * time.Minute
)

// Config OpenAI 兼容协议 LLM 客户端配置。
type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client 兼容 OpenAI Chat Completions 协议的 HTTP 客户端。
type Client struct {
	cfg     Config
	http    *http.Client
	timeout time.Duration
}

// NewClient 创建 LLM 客户端；未设置的字段使用默认值。
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout > 0 {
		// 自带 HTTPClient 时以它自己的超时为准，错误信息里照实写，避免报出未生效的预算。
		timeout = httpClient.Timeout
	}
	cfg.Timeout = timeout
	return &Client{cfg: cfg, http: httpClient, timeout: timeout}
}

// Timeout 返回当前客户端生效的单次请求超时。
func (c *Client) Timeout() time.Duration {
	return c.timeout
}

// ChatMessage OpenAI 风格的对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatOptions 单次 Chat Completions 可选参数。
type ChatOptions struct {
	// Temperature 采样温度；nil 表示不传（由上游默认）。
	Temperature *float64
	// JSONMode 启用 response_format=json_object，要求模型返回合法 JSON。
	JSONMode bool
	// EnableThinking 控制 DashScope/Qwen 混合思考模式；nil 表示不传（由上游默认）。
	// HTTP 直连时作为顶层字段 enable_thinking 发送。
	EnableThinking *bool
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	EnableThinking *bool           `json:"enable_thinking,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// ChatCompletions 调用 /chat/completions，返回上游模型的完整 JSON 响应体。
func (c *Client) ChatCompletions(ctx context.Context, messages []ChatMessage) (json.RawMessage, error) {
	return c.chatCompletions(ctx, messages, ChatOptions{})
}

// ChatCompletionsWithOptions 带可选参数调用 /chat/completions。
func (c *Client) ChatCompletionsWithOptions(ctx context.Context, messages []ChatMessage, opts ChatOptions) (json.RawMessage, error) {
	return c.chatCompletions(ctx, messages, opts)
}

func (c *Client) chatCompletions(ctx context.Context, messages []ChatMessage, opts ChatOptions) (json.RawMessage, error) {
	if c.cfg.APIKey == "" {
		return nil, fmt.Errorf("LLM API Key 未配置")
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages 不能为空")
	}

	reqBody := chatRequest{
		Model:          c.cfg.Model,
		Messages:       messages,
		Temperature:    opts.Temperature,
		EnableThinking: opts.EnableThinking,
	}
	if opts.JSONMode {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	url := c.cfg.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	started := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		// 把「谁超的时 / 跑了多久 / 预算是多少」写进错误：只写 "Post ...: context deadline exceeded" 时，
		// 运维分不清是 LLM 客户端预算用尽（本项目默认 30 分钟，可配 llm.timeout_sec），
		// 还是任务级硬超时（processHardTimeout，至少 6 小时）。
		// 预算照原样打印（不四舍五入），便于与 llm.timeout_sec 直接对照。
		return nil, fmt.Errorf("请求 LLM 失败(%s, 耗时 %s, 客户端超时上限 %s): %w",
			timeoutReason(ctx, err), time.Since(started).Round(time.Second),
			c.timeout, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 LLM 响应失败: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析 LLM 响应失败: %w; body=%s", err, truncate(string(raw), 512))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("LLM 返回错误: %s", parsed.Error.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, truncate(string(raw), 512))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("LLM 响应无 choices")
	}
	return json.RawMessage(raw), nil
}

// timeoutReason 判定请求失败的性质：调用方 context 到期 / 本客户端超时 / 其它传输失败。
func timeoutReason(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "调用方 context 已取消或超时"
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "LLM 客户端超时"
	}
	return "传输失败"
}

// Chat 调用 /chat/completions，返回助手文本内容。
func (c *Client) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	return c.chat(ctx, messages, ChatOptions{})
}

// ChatStructured 以 temperature=0 + JSON mode 调用，并显式关闭思考模式（适合 ASR 后处理等结构化抽取）。
func (c *Client) ChatStructured(ctx context.Context, messages []ChatMessage) (string, error) {
	temp := 0.0
	thinking := false
	return c.chat(ctx, messages, ChatOptions{
		Temperature:    &temp,
		JSONMode:       true,
		EnableThinking: &thinking,
	})
}

// ChatThinking 显式开启思考模式调用（适合 AI 切片等需更深推理的场景）。
func (c *Client) ChatThinking(ctx context.Context, messages []ChatMessage) (string, error) {
	thinking := true
	return c.chat(ctx, messages, ChatOptions{
		EnableThinking: &thinking,
	})
}

func (c *Client) chat(ctx context.Context, messages []ChatMessage, opts ChatOptions) (string, error) {
	raw, err := c.chatCompletions(ctx, messages, opts)
	if err != nil {
		return "", err
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("解析 LLM 响应失败: %w; body=%s", err, truncate(string(raw), 512))
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("LLM 响应内容为空")
	}
	return content, nil
}

// Model 返回当前使用的模型名。
func (c *Client) Model() string {
	return c.cfg.Model
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
