package config

import "live-mixer/internal/pkg/llm"

// LLMClientConfig 将应用 LLM 配置转换为 OpenAI 兼容客户端配置。
func (c LLMConfig) LLMClientConfig() llm.Config {
	return llm.Config{
		APIKey:  c.APIKey,
		BaseURL: c.BaseURL,
		Model:   c.Model,
	}
}
