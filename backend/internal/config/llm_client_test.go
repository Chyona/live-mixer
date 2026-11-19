package config

import (
	"testing"
	"time"

	"live-mixer/internal/pkg/llm"
)

func TestLoad_LLMEnvOverride(t *testing.T) {
	t.Setenv("APP_LLM_API_KEY", "llm-key")
	t.Setenv("APP_LLM_BASE_URL", "https://llm.example.com/v1")
	t.Setenv("APP_LLM_MODEL", "custom-model")
	t.Setenv("APP_LLM_FLASH_MODEL", "custom-flash")
	t.Setenv("APP_LLM_TIMEOUT_SEC", "1200")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLM.APIKey != "llm-key" {
		t.Errorf("APIKey = %q", cfg.LLM.APIKey)
	}
	if cfg.LLM.BaseURL != "https://llm.example.com/v1" {
		t.Errorf("BaseURL = %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.Model != "custom-model" {
		t.Errorf("Model = %q", cfg.LLM.Model)
	}
	if cfg.LLM.FlashModel != "custom-flash" {
		t.Errorf("FlashModel = %q", cfg.LLM.FlashModel)
	}
	if got := cfg.LLM.Timeout(); got != 20*time.Minute {
		t.Errorf("Timeout() = %v, want 20m", got)
	}
}

func TestLLMConfig_LLMClientConfig(t *testing.T) {
	cfg := LLMConfig{
		APIKey:  "k",
		BaseURL: "https://example.com/v3",
		Model:   "m1",
	}
	clientCfg := cfg.LLMClientConfig()
	if clientCfg.APIKey != "k" || clientCfg.BaseURL != "https://example.com/v3" || clientCfg.Model != "m1" {
		t.Errorf("clientCfg = %#v", clientCfg)
	}
	client := llm.NewClient(clientCfg)
	if client.Model() != "m1" {
		t.Errorf("Model() = %q", client.Model())
	}
}

func TestLLMConfig_LLMClientConfigForASR(t *testing.T) {
	cfg := LLMConfig{
		APIKey:     "k",
		BaseURL:    "https://example.com/v3",
		Model:      "qwen3.7-plus",
		FlashModel: "qwen3.7-flash",
	}
	asrCfg := cfg.LLMClientConfigForASR()
	if asrCfg.Model != "qwen3.7-flash" {
		t.Fatalf("ASR Model = %q, want qwen3.7-flash", asrCfg.Model)
	}
	if asrCfg.APIKey != "k" || asrCfg.BaseURL != "https://example.com/v3" {
		t.Errorf("asrCfg = %#v", asrCfg)
	}
	defaultCfg := cfg.LLMClientConfig()
	if defaultCfg.Model != "qwen3.7-plus" {
		t.Fatalf("default Model = %q, want qwen3.7-plus", defaultCfg.Model)
	}
}

func TestLLMConfig_FlashModelOrDefault(t *testing.T) {
	if got := (LLMConfig{FlashModel: "x"}).FlashModelOrDefault(); got != "x" {
		t.Fatalf("got %q, want x", got)
	}
	if got := (LLMConfig{}).FlashModelOrDefault(); got != defaultLLMFlashModel {
		t.Fatalf("got %q, want %q", got, defaultLLMFlashModel)
	}
}

// TestLLMConfig_Timeout 验证单次调用超时的回落与换算。
func TestLLMConfig_Timeout(t *testing.T) {
	tests := []struct {
		name string
		sec  int
		want time.Duration
	}{
		{name: "未配置回落默认", sec: 0, want: DefaultLLMTimeout},
		{name: "非正值回落默认", sec: -1, want: DefaultLLMTimeout},
		{name: "显式配置生效", sec: 60, want: time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (LLMConfig{TimeoutSec: tt.sec}).Timeout(); got != tt.want {
				t.Fatalf("Timeout() = %v, want %v", got, tt.want)
			}
		})
	}
	// 默认值必须能容纳线上最慢的一次生成（AI 切片实测 6.5~8.8 分钟）。
	if DefaultLLMTimeout < 15*time.Minute {
		t.Fatalf("DefaultLLMTimeout = %v, 小于实测最慢生成的 2 倍，AI 切片会被客户端超时掐死", DefaultLLMTimeout)
	}
}

// TestLLMConfig_ClientConfigsCarryTimeout 验证两个客户端配置都把超时透传到 llm.Config。
func TestLLMConfig_ClientConfigsCarryTimeout(t *testing.T) {
	cfg := LLMConfig{APIKey: "k", Model: "m", FlashModel: "f", TimeoutSec: 900}
	if got := cfg.LLMClientConfig().Timeout; got != 900*time.Second {
		t.Errorf("LLMClientConfig().Timeout = %v, want 900s", got)
	}
	if got := cfg.LLMClientConfigForASR().Timeout; got != 900*time.Second {
		t.Errorf("LLMClientConfigForASR().Timeout = %v, want 900s", got)
	}
	if got := (LLMConfig{}).LLMClientConfig().Timeout; got != DefaultLLMTimeout {
		t.Errorf("未配置时 LLMClientConfig().Timeout = %v, want %v", got, DefaultLLMTimeout)
	}
}
