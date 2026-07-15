package config

import (
	"testing"

	"live-mixer/internal/pkg/llm"
)

func TestLoad_LLMEnvOverride(t *testing.T) {
	t.Setenv("APP_LLM_API_KEY", "llm-key")
	t.Setenv("APP_LLM_BASE_URL", "https://llm.example.com/v1")
	t.Setenv("APP_LLM_MODEL", "custom-model")

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
