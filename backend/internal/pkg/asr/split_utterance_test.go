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
		{12, []int{12}},
		{13, []int{7, 6}},
		{24, []int{12, 12}},
		{25, []int{9, 8, 8}},
		{31, []int{11, 10, 10}},
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
	word := "ABCDEFGHIJKLM" // 13 letters > 12
	got := splitBalancedPreferLatin(word, MaxCaptionRunes)
	if len(got) != 1 || got[0] != word {
		t.Fatalf("got %v, want whole word on one line", got)
	}
}

func assertTokenIntactAcrossLines(t *testing.T, lines []string, token string) {
	t.Helper()
	found := false
	for _, line := range lines {
		if strings.Contains(line, token) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("token %q not kept intact in any line: %v", token, lines)
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, token) {
		t.Fatalf("joined text missing token %q: %q", token, joined)
	}
	tok := []rune(token)
	for i := 0; i+1 < len(lines); i++ {
		left, right := lines[i], lines[i+1]
		for k := 1; k < len(tok); k++ {
			prefix, suffix := string(tok[:k]), string(tok[k:])
			if strings.HasSuffix(left, prefix) && strings.HasPrefix(right, suffix) {
				t.Errorf("token %q split across lines %q | %q: %v", token, left, right, lines)
			}
		}
	}
}

func TestSplitBalancedPreferIntact_KeepsNumberIntact(t *testing.T) {
	// 优惠力度达到(6) + 100(3) + 真的很香(4) = 13 → 目标 7+6，切点易落在 100 中
	text := "优惠力度达到100真的很香"
	got := splitBalancedPreferIntact(text, MaxCaptionRunes)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "100")
}

func TestSplitBalancedPreferIntact_KeepsPercentIntact(t *testing.T) {
	text := "优惠力度达到100%真的很香" // 14 runes → 7+7
	got := splitBalancedPreferIntact(text, MaxCaptionRunes)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "100%")
}

func TestSplitBalancedPreferIntact_LongNumber(t *testing.T) {
	num := "1234567890123" // 13 digits > 12
	got := splitBalancedPreferIntact(num, MaxCaptionRunes)
	if len(got) != 1 || got[0] != num {
		t.Fatalf("got %v, want whole number on one line", got)
	}
}

func TestSplitByPunctuation_KeepsDecimalIntact(t *testing.T) {
	got := splitByPunctuation("价格是3.14元。真的")
	joined := strings.Join(got, "")
	if joined != "价格是3.14元。真的" {
		t.Fatalf("joined %q", joined)
	}
	found := false
	for _, clause := range got {
		if strings.Contains(clause, "3.14") {
			found = true
		}
		if strings.Contains(clause, "3.") && !strings.Contains(clause, "3.14") {
			t.Errorf("decimal split in clause %q: %v", clause, got)
		}
	}
	if !found {
		t.Fatalf("3.14 not intact: %v", got)
	}
}

func TestSplitBalancedPreferIntact_KeepsDecimalIntact(t *testing.T) {
	text := "今天价格只要3.14元起"
	got := splitBalancedPreferIntact(text, MaxCaptionRunes)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "3.14")
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

func TestTrimCaptionEdgePunct(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"好，", "好"},
		{"，然后", "然后"},
		{"今天很好，", "今天很好"},
		{"…开场", "开场"},
		{"结尾...", "结尾"},
		{"  ，。  ", ""},
	}
	for _, tt := range tests {
		if got := trimCaptionEdgePunct(tt.in); got != tt.want {
			t.Errorf("trimCaptionEdgePunct(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func assertNoEdgePunct(t *testing.T, text string) {
	t.Helper()
	runes := []rune(text)
	if len(runes) == 0 {
		return
	}
	if runes[0] == '…' || isBreakPunctRune(runes[0]) {
		t.Errorf("leading punct: %q", text)
	}
	if len(runes) >= 3 && runes[0] == '.' && runes[1] == '.' && runes[2] == '.' {
		t.Errorf("leading ellipsis: %q", text)
	}
	last := len(runes) - 1
	if runes[last] == '…' || isBreakPunctRune(runes[last]) {
		t.Errorf("trailing punct: %q", text)
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
	if got[0].Text != "好" {
		t.Errorf("first = %q", got[0].Text)
	}
	for _, seg := range got {
		n := utf8.RuneCountInString(seg.Text)
		if n > MaxCaptionRunes {
			t.Errorf("seg %q len=%d > %d", seg.Text, n, MaxCaptionRunes)
		}
		assertNoEdgePunct(t, seg.Text)
		if seg.EndTime <= seg.StartTime {
			t.Errorf("bad time %#v", seg)
		}
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
	if got[0].Text != "今天很好" || got[1].Text != "明天更好" {
		t.Errorf("texts = %q / %q", got[0].Text, got[1].Text)
	}
	assertNoEdgePunct(t, got[0].Text)
	assertNoEdgePunct(t, got[1].Text)
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
