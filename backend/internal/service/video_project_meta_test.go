package service

import (
	"strings"
	"testing"

	"live-mixer/internal/model"
)

func TestNormalizeVideoTitle(t *testing.T) {
	got, err := normalizeVideoTitle("  利率上行的真相  ")
	if err != nil || got != "利率上行的真相" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := normalizeVideoTitle("一"); err == nil {
		t.Fatal("expected too-short error")
	}
	long := strings.Repeat("字", model.VideoProjectTitleMaxRunes+1)
	if _, err := normalizeVideoTitle(long); err == nil {
		t.Fatal("expected too-long error")
	}
	empty, err := normalizeVideoTitle("   ")
	if err != nil || empty != "" {
		t.Fatalf("empty = %q err=%v", empty, err)
	}
}

func TestNormalizeVideoDescription(t *testing.T) {
	got, err := normalizeVideoDescription("  介绍  ")
	if err != nil || got != "介绍" {
		t.Fatalf("got %q err=%v", got, err)
	}
	long := strings.Repeat("字", model.VideoProjectDescriptionMaxRunes+1)
	if _, err := normalizeVideoDescription(long); err == nil {
		t.Fatal("expected too-long error")
	}
}

func TestNormalizeVideoTopics(t *testing.T) {
	got, err := normalizeVideoTopics([]string{" 宏观经济 ", "#利率周期", "资产配置"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 3 || got[0] != "宏观经济" || got[1] != "利率周期" {
		t.Fatalf("got = %#v", got)
	}

	empty, err := normalizeVideoTopics(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("nil => %#v err=%v", empty, err)
	}

	if _, err := normalizeVideoTopics([]string{"宏观经济"}); err == nil {
		t.Fatal("expected count error")
	}
	if _, err := normalizeVideoTopics([]string{"一", "宏观经济"}); err == nil {
		t.Fatal("expected item length error")
	}
}

func TestSanitizeVideoMetaFromLLM(t *testing.T) {
	if got := sanitizeVideoTitleFromLLM("  利率上行的真相再加几个字啊  "); got != "利率上行的真相再加几个字" {
		t.Errorf("truncated title = %q", got)
	}
	if got := sanitizeVideoTitleFromLLM("一"); got != "" {
		t.Errorf("short title = %q, want empty", got)
	}
	longDesc := strings.Repeat("字", 200)
	if got := sanitizeVideoDescriptionFromLLM(longDesc); len([]rune(got)) != model.VideoProjectDescriptionMaxRunes {
		t.Errorf("desc runes = %d", len([]rune(got)))
	}
	got := sanitizeVideoTopicsFromLLM([]string{"#宏观经济", "利率周期", "一", "宏观经济", "资产配置", "投资理念", "行业研究", "多余话题"})
	if len(got) != 6 {
		t.Fatalf("topics len = %d, want 6: %#v", len(got), got)
	}
	if got[0] != "宏观经济" || got[1] != "利率周期" {
		t.Errorf("topics = %#v", got)
	}
}
