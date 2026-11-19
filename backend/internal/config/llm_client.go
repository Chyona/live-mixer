package config

import (
	"time"

	"live-mixer/internal/pkg/llm"
)

const defaultLLMFlashModel = "qwen3.7-flash"

// DefaultLLMTimeout 单次 Chat Completions 客户端超时默认值。
// 直接复用 llm 包的常量，避免两处默认值漂移。
const DefaultLLMTimeout = llm.DefaultTimeout

// Timeout 返回单次 Chat Completions 的客户端超时；TimeoutSec <= 0 时回落 DefaultLLMTimeout。
func (c LLMConfig) Timeout() time.Duration {
	if c.TimeoutSec <= 0 {
		return DefaultLLMTimeout
	}
	return time.Duration(c.TimeoutSec) * time.Second
}

// LLMClientConfig 将应用 LLM 配置转换为 OpenAI 兼容客户端配置（默认 Model，供 AI 切片等使用）。
func (c LLMConfig) LLMClientConfig() llm.Config {
	return llm.Config{
		APIKey:  c.APIKey,
		BaseURL: c.BaseURL,
		Model:   c.Model,
		Timeout: c.Timeout(),
	}
}

// FlashModelOrDefault 返回 ASR 后处理使用的轻量模型名；未配置时回退到默认 Flash，再回退到 Model。
func (c LLMConfig) FlashModelOrDefault() string {
	if c.FlashModel != "" {
		return c.FlashModel
	}
	if defaultLLMFlashModel != "" {
		return defaultLLMFlashModel
	}
	return c.Model
}

// LLMClientConfigForASR 返回添加视频后 ASR 后处理专用客户端配置（使用 FlashModel）。
// 该客户端同时被成片字幕 LLM 断句复用：断句按 15 秒/条的 ctx 预算，不受这里的大超时影响。
func (c LLMConfig) LLMClientConfigForASR() llm.Config {
	return llm.Config{
		APIKey:  c.APIKey,
		BaseURL: c.BaseURL,
		Model:   c.FlashModelOrDefault(),
		Timeout: c.Timeout(),
	}
}
