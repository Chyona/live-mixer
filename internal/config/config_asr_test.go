package config

import "testing"

func TestLoad_ASREnvOverride(t *testing.T) {
	t.Setenv("APP_ASR_API_KEY", "override-key")
	t.Setenv("APP_ASR_BASE_URL", "https://custom.example.com/asr")
	t.Setenv("APP_ASR_RESOURCE_ID", "custom.resource")
	t.Setenv("APP_ASR_POLL_INTERVAL_SEC", "5")
	t.Setenv("APP_ASR_MAX_POLLS", "120")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ASR.APIKey != "override-key" {
		t.Errorf("ASR.APIKey = %q, want override-key", cfg.ASR.APIKey)
	}
	if cfg.ASR.BaseURL != "https://custom.example.com/asr" {
		t.Errorf("ASR.BaseURL = %q, want custom base url", cfg.ASR.BaseURL)
	}
	if cfg.ASR.ResourceID != "custom.resource" {
		t.Errorf("ASR.ResourceID = %q, want custom.resource", cfg.ASR.ResourceID)
	}
	if cfg.ASR.PollIntervalSec != 5 {
		t.Errorf("ASR.PollIntervalSec = %d, want 5", cfg.ASR.PollIntervalSec)
	}
	if cfg.ASR.MaxPolls != 120 {
		t.Errorf("ASR.MaxPolls = %d, want 120", cfg.ASR.MaxPolls)
	}
}
