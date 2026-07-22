package asr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitBalancedPreferLatin_Table(t *testing.T) {
	mk := func(n int) string {
		b := make([]rune, n)
		for i := range b {
			b[i] = rune('一' + i%10)
		}
		return string(b)
	}
	tests := []struct {
		n    int
		want []int
	}{
		{15, []int{15}},
		{16, []int{8, 8}},
		{17, []int{9, 8}},
		{30, []int{15, 15}},
		{31, []int{11, 10, 10}},
		{46, []int{12, 12, 11, 11}},
	}
	for _, tt := range tests {
		got := splitBalancedPreferLatin(mk(tt.n), MaxCaptionRunes)
		if len(got) != len(tt.want) {
			t.Fatalf("n=%d len=%d want %d lines: %v", tt.n, len(got), len(tt.want), got)
		}
		for i, line := range got {
			n := utf8.RuneCountInString(line)
			if n != tt.want[i] {
				t.Errorf("n=%d line[%d] len=%d want %d (%q)", tt.n, i, n, tt.want[i], line)
			}
			if n > MaxCaptionRunes {
				t.Errorf("n=%d line[%d] exceeds max: %d", tt.n, i, n)
			}
		}
	}
}

func TestSplitBalancedPreferLatin_KeepsEnglishWordIntact(t *testing.T) {
	// 7 汉字 + ChatGPT(7) + 3 汉字 = 17，理想切点落在 ChatGPT 中部。
	text := "一二三四五六七ChatGPT八九十"
	got := splitBalancedPreferLatin(text, MaxCaptionRunes)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	found := false
	for _, line := range got {
		if strings.Contains(line, "ChatGPT") {
			found = true
		}
		if strings.Contains(line, "Chat") && !strings.Contains(line, "ChatGPT") {
			t.Errorf("word split across lines: %v", got)
		}
		if strings.Contains(line, "GPT") && !strings.Contains(line, "ChatGPT") {
			t.Errorf("word split across lines: %v", got)
		}
		if utf8.RuneCountInString(line) > MaxCaptionRunes {
			t.Errorf("line too long: %q (%d)", line, utf8.RuneCountInString(line))
		}
	}
	if !found {
		t.Fatalf("ChatGPT not kept intact in any line: %v", got)
	}
}

func TestSplitBalancedPreferLatin_LongEnglishWord(t *testing.T) {
	word := "ABCDEFGHIJKLMNOP" // 16 letters > 15
	got := splitBalancedPreferLatin(word, MaxCaptionRunes)
	if len(got) != 1 || got[0] != word {
		t.Fatalf("got %v, want whole word on one line", got)
	}
}

func TestSplitByPunctuation(t *testing.T) {
	got := splitByPunctuation("好，我里面给你们去搭个这个嗯蕾丝美学的米色")
	if len(got) != 2 || got[0] != "好，" {
		t.Fatalf("got %v", got)
	}
	if !strings.HasPrefix(got[1], "我里面") {
		t.Fatalf("second clause = %q", got[1])
	}
}

func TestSplitUtteranceForCaptions_PunctAndBalance(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   10000,
		Text:      "好，我里面给你们去搭个这个嗯蕾丝美学的米色",
	}
	got := SplitUtteranceForCaptions(u)
	if len(got) < 2 {
		t.Fatalf("expected multiple segments, got %v", got)
	}
	if got[0].Text != "好，" {
		t.Errorf("first = %q", got[0].Text)
	}
	var total int
	for _, seg := range got {
		n := utf8.RuneCountInString(seg.Text)
		total += n
		if n > MaxCaptionRunes {
			t.Errorf("seg %q len=%d > 15", seg.Text, n)
		}
		if seg.EndTime <= seg.StartTime {
			t.Errorf("bad time %#v", seg)
		}
	}
	if total != utf8.RuneCountInString(u.Text) {
		t.Errorf("rune total %d want %d", total, utf8.RuneCountInString(u.Text))
	}
	if got[0].StartTime != 0 || got[len(got)-1].EndTime != 10000 {
		t.Errorf("time span %d-%d", got[0].StartTime, got[len(got)-1].EndTime)
	}
}

func TestSplitUtteranceForCaptions_WithWords(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   4000,
		Text:      "今天很好，明天更好",
		Words: []Word{
			{Text: "今天", StartTime: 0, EndTime: 500},
			{Text: "很好", StartTime: 500, EndTime: 1200},
			{Text: "明天", StartTime: 2000, EndTime: 2800},
			{Text: "更好", StartTime: 2800, EndTime: 4000},
		},
	}
	got := SplitUtteranceForCaptions(u)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %#v", len(got), got)
	}
	if got[0].Text != "今天很好，" || got[1].Text != "明天更好" {
		t.Errorf("texts = %q / %q", got[0].Text, got[1].Text)
	}
	if got[0].StartTime != 0 || got[0].EndTime != 1200 {
		t.Errorf("seg0 time = %d-%d", got[0].StartTime, got[0].EndTime)
	}
	if got[1].StartTime != 2000 || got[1].EndTime != 4000 {
		t.Errorf("seg1 time = %d-%d", got[1].StartTime, got[1].EndTime)
	}
}

func TestSplitUtteranceForCaptions_ShortUnchanged(t *testing.T) {
	u := Utterance{StartTime: 100, EndTime: 800, Text: "第一段话"}
	got := SplitUtteranceForCaptions(u)
	if len(got) != 1 || got[0].Text != "第一段话" || got[0].StartTime != 100 || got[0].EndTime != 800 {
		t.Fatalf("got %#v", got)
	}
}
