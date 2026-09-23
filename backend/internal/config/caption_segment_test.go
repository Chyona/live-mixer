package config

import (
	"testing"
	"time"

	"live-mixer/internal/pkg/asr"
)

func TestCaptionSegmentConfig_Defaults(t *testing.T) {
	var c CaptionSegmentConfig
	if c.Enabled != nil {
		t.Error("未配置时 Enabled 应为 nil（缺省即开启，只有显式 false 才是关闭）")
	}
	if !c.EnabledOrDefault() {
		t.Error("缺省应启用 LLM 断句")
	}
	if got := c.Timeout(); got != DefaultCaptionSegmentTimeout {
		t.Errorf("Timeout() = %v, want %v", got, DefaultCaptionSegmentTimeout)
	}
	if got := c.ConcurrencyOrDefault(); got != DefaultCaptionSegmentConcurrency {
		t.Errorf("ConcurrencyOrDefault() = %d, want %d", got, DefaultCaptionSegmentConcurrency)
	}
	if got := c.CacheSizeOrDefault(); got != DefaultCaptionSegmentCacheSize {
		t.Errorf("CacheSizeOrDefault() = %d, want %d", got, DefaultCaptionSegmentCacheSize)
	}
}

func TestCaptionSegmentConfig_CustomValues(t *testing.T) {
	c := CaptionSegmentConfig{
		Enabled:     boolPtr(false),
		TimeoutMS:   3000,
		Concurrency: 2,
		CacheSize:   10,
	}
	if c.EnabledOrDefault() {
		t.Error("显式 false 应关闭 LLM 断句")
	}
	if got := c.Timeout(); got != 3*time.Second {
		t.Errorf("Timeout() = %v, want 3s", got)
	}
	if got := c.ConcurrencyOrDefault(); got != 2 {
		t.Errorf("ConcurrencyOrDefault() = %d, want 2", got)
	}
	if got := c.CacheSizeOrDefault(); got != 10 {
		t.Errorf("CacheSizeOrDefault() = %d, want 10", got)
	}
}

func TestCaptionSegmentConfig_NegativeValuesFallBack(t *testing.T) {
	c := CaptionSegmentConfig{TimeoutMS: -1, Concurrency: -1, CacheSize: -1}
	if got := c.Timeout(); got != DefaultCaptionSegmentTimeout {
		t.Errorf("Timeout() = %v, want %v", got, DefaultCaptionSegmentTimeout)
	}
	if got := c.ConcurrencyOrDefault(); got != DefaultCaptionSegmentConcurrency {
		t.Errorf("ConcurrencyOrDefault() = %d, want %d", got, DefaultCaptionSegmentConcurrency)
	}
	if got := c.CacheSizeOrDefault(); got != DefaultCaptionSegmentCacheSize {
		t.Errorf("CacheSizeOrDefault() = %d, want %d", got, DefaultCaptionSegmentCacheSize)
	}
}

// TestCaptionSegmentConfig_RunesNotConfigurable 行宽是共享常量、不是配置项：
// LLM 断句与规则折行必须同一上限，否则同一支成片里两种行宽并存；改动该常量需同步提示词与竖屏样式。
func TestCaptionSegmentConfig_RunesNotConfigurable(t *testing.T) {
	if asr.MaxCaptionRunes != 12 {
		t.Fatalf("MaxCaptionRunes = %d, want 12——改这个常量要同步提示词文案与竖屏样式", asr.MaxCaptionRunes)
	}
}

func TestLoad_CaptionSegmentEnvOverride(t *testing.T) {
	t.Setenv("APP_CAPTION_SEGMENT_ENABLED", "true")
	t.Setenv("APP_CAPTION_SEGMENT_TIMEOUT_MS", "3000")
	t.Setenv("APP_CAPTION_SEGMENT_CONCURRENCY", "2")
	t.Setenv("APP_CAPTION_SEGMENT_CACHE_SIZE", "16")
	// 已删除的配置项不应再被读取（写了也不生效）。
	t.Setenv("APP_CAPTION_SEGMENT_MAX_RUNES", "10")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.CaptionSegment.EnabledOrDefault() {
		t.Error("CaptionSegment.EnabledOrDefault() = false, want true")
	}
	if got := cfg.CaptionSegment.Timeout(); got != 3*time.Second {
		t.Errorf("Timeout() = %v, want 3s", got)
	}
	if got := cfg.CaptionSegment.ConcurrencyOrDefault(); got != 2 {
		t.Errorf("ConcurrencyOrDefault() = %d, want 2", got)
	}
	if got := cfg.CaptionSegment.CacheSizeOrDefault(); got != 16 {
		t.Errorf("CacheSizeOrDefault() = %d, want 16", got)
	}
	if asr.MaxCaptionRunes != 12 {
		t.Errorf("行宽应恒为 asr.MaxCaptionRunes(12)，实际 %d", asr.MaxCaptionRunes)
	}
}

// TestLoad_CaptionSegmentEnabledByDefault 出厂即开启：配置里不写 caption_segment 也算开启。
func TestLoad_CaptionSegmentEnabledByDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CaptionSegment.Enabled != nil {
		t.Errorf("Enabled = %v, want nil（未配置）", *cfg.CaptionSegment.Enabled)
	}
	if !cfg.CaptionSegment.EnabledOrDefault() {
		t.Error("未显式配置时应启用 LLM 断句（缺省即开启）")
	}
}

func TestLoad_CaptionSegmentExplicitOffByEnv(t *testing.T) {
	t.Setenv("APP_CAPTION_SEGMENT_ENABLED", "false")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CaptionSegment.EnabledOrDefault() {
		t.Error("APP_CAPTION_SEGMENT_ENABLED=false 应关闭 LLM 断句")
	}
}

func TestLoad_CaptionSegmentInvalidEnvIgnored(t *testing.T) {
	t.Setenv("APP_CAPTION_SEGMENT_ENABLED", "maybe")
	t.Setenv("APP_CAPTION_SEGMENT_CONCURRENCY", "abc")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CaptionSegment.Enabled != nil {
		t.Errorf("非法布尔值应被忽略（保持未配置），实际 = %v", *cfg.CaptionSegment.Enabled)
	}
	if !cfg.CaptionSegment.EnabledOrDefault() {
		t.Error("非法布尔值被忽略后应回到缺省：开启")
	}
	if got := cfg.CaptionSegment.ConcurrencyOrDefault(); got != DefaultCaptionSegmentConcurrency {
		t.Errorf("ConcurrencyOrDefault() = %d, want %d", got, DefaultCaptionSegmentConcurrency)
	}
}

func boolPtr(b bool) *bool { return &b }
