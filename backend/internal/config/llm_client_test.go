package config

import (
	"testing"

	"live-mixer/internal/pkg/llm"
)

func TestLoad_LLMEnvOverride(t *testing.T) {
	t.Setenv("APP_LLM_API_KEY", "llm-key")
	t.Setenv("APP_LLM_BASE_URL", "https://llm.example.com/v1")
	t.Setenv("APP_LLM_MODEL", "custom-model")
	t.Setenv("APP_LLM_FLASH_MODEL", "custom-flash")

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
