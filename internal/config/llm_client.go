package config

import "live-mixer/internal/pkg/llm"

const defaultLLMFlashModel = "qwen3.7-flash"

// LLMClientConfig 将应用 LLM 配置转换为 OpenAI 兼容客户端配置（默认 Model，供 AI 切片等使用）。
func (c LLMConfig) LLMClientConfig() llm.Config {
	return llm.Config{
		APIKey:  c.APIKey,
		BaseURL: c.BaseURL,
		Model:   c.Model,
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
func (c LLMConfig) LLMClientConfigForASR() llm.Config {
	return llm.Config{
		APIKey:  c.APIKey,
		BaseURL: c.BaseURL,
		Model:   c.FlashModelOrDefault(),
	}
}
