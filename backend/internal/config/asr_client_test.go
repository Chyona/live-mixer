package config

import (
	"testing"
	"time"

	"live-mixer/internal/pkg/asr"
)

func TestASRConfig_ASRClientConfig(t *testing.T) {
	cfg := ASRConfig{
		APIKey:          "test-key",
		BaseURL:         "https://example.com/asr",
		ResourceID:      "volc.seedasr.auc",
		PollIntervalSec: 5,
		MaxPolls:        100,
	}

	clientCfg := cfg.ASRClientConfig()
	if clientCfg.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want test-key", clientCfg.APIKey)
	}
	if clientCfg.BaseURL != "https://example.com/asr" {
		t.Errorf("BaseURL = %q, want custom url", clientCfg.BaseURL)
	}
	if clientCfg.ResourceID != "volc.seedasr.auc" {
		t.Errorf("ResourceID = %q, want volc.seedasr.auc", clientCfg.ResourceID)
	}
	if clientCfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s", clientCfg.PollInterval)
	}
	if clientCfg.MaxPolls != 100 {
		t.Errorf("MaxPolls = %d, want 100", clientCfg.MaxPolls)
	}

	// 零值轮询间隔由 asr.NewClient 补默认值
	empty := ASRConfig{APIKey: "k"}.ASRClientConfig()
	client := asr.NewClient(empty)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}
