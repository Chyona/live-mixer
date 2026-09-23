package config

import (
	"testing"
	"time"

	"live-mixer/internal/pkg/asr"
)

func TestCaptionSegmentConfig_Defaults(t *testing.T) {
	var c CaptionSegmentConfig
	if c.Enabled {
		t.Error("默认应关闭 LLM 断句")
	}
	if got := c.Timeout(); got != DefaultCaptionSegmentTimeout {
		t.Errorf("Timeout() = %v, want %v", got, DefaultCaptionSegmentTimeout)
	}
	if got := c.ConcurrencyOrDefault(); got != DefaultCaptionSegmentConcurrency {
		t.Errorf("ConcurrencyOrDefault() = %d, want %d", got, DefaultCaptionSegmentConcurrency)
	}
	if got := c.MaxRunesOrDefault(); got != asr.MaxCaptionRunes {
		t.Errorf("MaxRunesOrDefault() = %d, want %d（与规则折行同一上限）", got, asr.MaxCaptionRunes)
	}
	if got := c.CacheSizeOrDefault(); got != DefaultCaptionSegmentCacheSize {
		t.Errorf("CacheSizeOrDefault() = %d, want %d", got, DefaultCaptionSegmentCacheSize)
	}
}

func TestCaptionSegmentConfig_CustomValues(t *testing.T) {
	c := CaptionSegmentConfig{
		Enabled:     true,
		TimeoutMS:   3000,
		Concurrency: 2,
		MaxRunes:    8,
		CacheSize:   10,
	}
	if got := c.Timeout(); got != 3*time.Second {
		t.Errorf("Timeout() = %v, want 3s", got)
	}
	if got := c.ConcurrencyOrDefault(); got != 2 {
		t.Errorf("ConcurrencyOrDefault() = %d, want 2", got)
	}
	if got := c.MaxRunesOrDefault(); got != 8 {
		t.Errorf("MaxRunesOrDefault() = %d, want 8", got)
	}
	if got := c.CacheSizeOrDefault(); got != 10 {
		t.Errorf("CacheSizeOrDefault() = %d, want 10", got)
	}
}

func TestCaptionSegmentConfig_NegativeValuesFallBack(t *testing.T) {
	c := CaptionSegmentConfig{TimeoutMS: -1, Concurrency: -1, MaxRunes: -1, CacheSize: -1}
	if got := c.Timeout(); got != DefaultCaptionSegmentTimeout {
		t.Errorf("Timeout() = %v, want %v", got, DefaultCaptionSegmentTimeout)
	}
	if got := c.ConcurrencyOrDefault(); got != DefaultCaptionSegmentConcurrency {
		t.Errorf("ConcurrencyOrDefault() = %d, want %d", got, DefaultCaptionSegmentConcurrency)
	}
	if got := c.MaxRunesOrDefault(); got != asr.MaxCaptionRunes {
		t.Errorf("MaxRunesOrDefault() = %d, want %d", got, asr.MaxCaptionRunes)
	}
	if got := c.CacheSizeOrDefault(); got != DefaultCaptionSegmentCacheSize {
		t.Errorf("CacheSizeOrDefault() = %d, want %d", got, DefaultCaptionSegmentCacheSize)
	}
}

func TestLoad_CaptionSegmentEnvOverride(t *testing.T) {
	t.Setenv("APP_CAPTION_SEGMENT_ENABLED", "true")
	t.Setenv("APP_CAPTION_SEGMENT_TIMEOUT_MS", "3000")
	t.Setenv("APP_CAPTION_SEGMENT_CONCURRENCY", "2")
	t.Setenv("APP_CAPTION_SEGMENT_MAX_RUNES", "10")
	t.Setenv("APP_CAPTION_SEGMENT_CACHE_SIZE", "16")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.CaptionSegment.Enabled {
		t.Error("CaptionSegment.Enabled = false, want true")
	}
	if got := cfg.CaptionSegment.Timeout(); got != 3*time.Second {
		t.Errorf("Timeout() = %v, want 3s", got)
	}
	if got := cfg.CaptionSegment.ConcurrencyOrDefault(); got != 2 {
		t.Errorf("ConcurrencyOrDefault() = %d, want 2", got)
	}
	if got := cfg.CaptionSegment.MaxRunesOrDefault(); got != 10 {
		t.Errorf("MaxRunesOrDefault() = %d, want 10", got)
	}
	if got := cfg.CaptionSegment.CacheSizeOrDefault(); got != 16 {
		t.Errorf("CacheSizeOrDefault() = %d, want 16", got)
	}
}

func TestLoad_CaptionSegmentDisabledByDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CaptionSegment.Enabled {
		t.Error("未显式配置时不应启用 LLM 断句")
	}
}

func TestLoad_CaptionSegmentInvalidEnvIgnored(t *testing.T) {
	t.Setenv("APP_CAPTION_SEGMENT_ENABLED", "maybe")
	t.Setenv("APP_CAPTION_SEGMENT_CONCURRENCY", "abc")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CaptionSegment.Enabled {
		t.Error("非法布尔值不应启用 LLM 断句")
	}
	if got := cfg.CaptionSegment.ConcurrencyOrDefault(); got != DefaultCaptionSegmentConcurrency {
		t.Errorf("ConcurrencyOrDefault() = %d, want %d", got, DefaultCaptionSegmentConcurrency)
	}
}
