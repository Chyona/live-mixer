package asr

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidateCaptionLines_AcceptsLosslessSplit(t *testing.T) {
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	lines, err := ValidateCaptionLines(text, []string{"今天我们来聊一聊", "人工智能在医疗领域的应用"}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("ValidateCaptionLines err = %v, want nil", err)
	}
	if strings.Join(lines, "") != text {
		t.Fatalf("lines 拼接 %q != 原文 %q", strings.Join(lines, ""), text)
	}
}

func TestValidateCaptionLines_AcceptsDroppedBoundaryPunctuation(t *testing.T) {
	// 真实模型会把行边界的标点/空白直接丢掉（产品规则本来就要剥掉它们），这类结果必须采纳：
	// 内容字符一字不能少、顺序不能变，但「行边界丢标点与空白」不算改动原文。
	tests := []struct {
		name  string
		text  string
		lines []string
		want  []string
	}{
		{
			name:  "丢掉行边界逗号与句号",
			text:  "这款产品原价是￥199，现在直播间下单只要98折，相当于省了小两百块钱。",
			lines: []string{"这款产品原价是￥199", "现在直播间下单只要98折", "相当于省了小两百块钱"},
			want:  []string{"这款产品原价是￥199", "现在直播间下单只要98折", "相当于省了小两百块钱"},
		},
		{
			name:  "丢掉行首空白",
			text:  "我们聊 ChatGPT 可以吗",
			lines: []string{"我们聊 ChatGPT", "可以吗"},
			want:  []string{"我们聊 ChatGPT", "可以吗"},
		},
		{
			name:  "丢掉结尾省略号",
			text:  "这个事情我们后面再聊……",
			lines: []string{"这个事情我们后面再聊"},
			want:  []string{"这个事情我们后面再聊"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateCaptionLines(tt.text, tt.lines, MaxCaptionRunes)
			if err != nil {
				t.Fatalf("ValidateCaptionLines err = %v, want nil", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("lines = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("lines[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValidateCaptionLines_RejectsUnusableLines(t *testing.T) {
	text := "今天我们来聊一聊人工智能"
	tests := []struct {
		name   string
		lines  []string
		reason string
	}{
		{"空行", []string{}, CaptionLinesReasonEmpty},
		{"空白行", []string{"今天我们来聊一聊", "  "}, CaptionLinesReasonBlankLine},
		{"漏字", []string{"今天我们来聊一聊", "人工智"}, CaptionLinesReasonMismatch},
		{"改写", []string{"今天我们来聊一聊", "人工只能"}, CaptionLinesReasonMismatch},
		{"多字", []string{"今天我们来聊一聊", "人工智能啊"}, CaptionLinesReasonMismatch},
		{"顺序错", []string{"人工智能", "今天我们来聊一聊"}, CaptionLinesReasonMismatch},
		{"切碎", []string{"今", "天", "我", "们", "来", "聊", "一", "聊", "人", "工", "智", "能"}, CaptionLinesReasonTooFine},
		{"切开英文词", []string{"我们聊 ChatGP", "T 可以吗"}, CaptionLinesReasonTokenCut},
		// 行尾的 '.' 落在数字原子「3.14」内部：它不是可以丢的行边界标点，所以第二行内容对不上原文。
		{"切开小数", []string{"今天的涨幅是 3.", "14 个百分点"}, CaptionLinesReasonMismatch},
		{"切开货币", []string{"原价是 ￥", "199 元"}, CaptionLinesReasonTokenCut},
		{"切开词表词", []string{"美联", "储主席"}, CaptionLinesReasonTokenCut},
		{"纯标点行", []string{"今天我们来聊一聊，", "，", "人工智能。"}, CaptionLinesReasonMismatch},
		{"行内丢标点", []string{"今天我们来聊一聊人工智能"}, CaptionLinesReasonMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := text
			switch tt.name {
			case "切开英文词":
				input = "我们聊 ChatGPT 可以吗"
			case "切开小数":
				input = "今天的涨幅是 3.14 个百分点"
			case "切开货币":
				input = "原价是 ￥199 元"
			case "切开词表词":
				input = "美联储主席"
			case "切碎":
				input = "今天我们来聊一聊人工智能"
			case "纯标点行":
				input = "今天我们来聊一聊，人工智能。"
			case "行内丢标点":
				input = "今天我们来聊一聊，人工智能"
			}
			got, err := ValidateCaptionLines(input, tt.lines, MaxCaptionRunes)
			if err == nil {
				t.Fatalf("ValidateCaptionLines err = nil, want %s (got %v)", tt.reason, got)
			}
			if !errors.Is(err, ErrCaptionLines) {
				t.Errorf("errors.Is(err, ErrCaptionLines) = false, err = %v", err)
			}
			var ce *CaptionLinesError
			if !errors.As(err, &ce) || ce.Reason != tt.reason {
				t.Errorf("reason = %v, want %s", err, tt.reason)
			}
		})
	}
}

func TestValidateCaptionLines_ResplitsOverLongLine(t *testing.T) {
	// 单行 20 字，超过 MaxCaptionRunes：就地按规则再切，且不丢字。
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	lines, err := ValidateCaptionLines(text, []string{text}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("ValidateCaptionLines err = %v, want nil", err)
	}
	if len(lines) < 2 {
		t.Fatalf("lines = %v, want 至少 2 行", lines)
	}
	for i, line := range lines {
		if n := utf8.RuneCountInString(line); n > MaxCaptionRunes {
			t.Errorf("lines[%d] = %q 长度 %d > %d", i, line, n, MaxCaptionRunes)
		}
	}
	if joined := strings.Join(lines, ""); joined != text {
		t.Errorf("拼接 %q != 原文 %q", joined, text)
	}
}

func TestValidateCaptionLines_StripsEdgePunctuation(t *testing.T) {
	// 标点在原文里必须原样保留（无损校验的前置条件），但成片行按产品规则剥掉行首尾标点。
	text := "今天我们来聊一聊，人工智能。"
	lines, err := ValidateCaptionLines(text, []string{"今天我们来聊一聊，", "人工智能。"}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("ValidateCaptionLines err = %v, want nil", err)
	}
	want := []string{"今天我们来聊一聊", "人工智能"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("lines[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
	// 去掉边缘标点后应与原文（同样去标点）一致：只丢标点，不丢字。
	if stripEdge := strings.Join(lines, ""); stripEdge != stripBreakPunct(text) {
		t.Errorf("去标点后 %q != %q", stripEdge, stripBreakPunct(text))
	}
}

func TestSplitLinesByRule_MatchesUtterancePath(t *testing.T) {
	// 无词级时间时，规则折行与 SplitUtteranceForCaptions 的行文本一致（比例分配路径）。
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	lines := SplitLinesByRule(text)
	segs := SplitUtteranceForCaptions(Utterance{Text: text, StartTime: 0, EndTime: 1000})
	if len(lines) != len(segs) {
		t.Fatalf("lines = %v, segs = %v", lines, segs)
	}
	for i := range lines {
		if lines[i] != segs[i].Text {
			t.Errorf("lines[%d] = %q, segs[%d].Text = %q", i, lines[i], i, segs[i].Text)
		}
	}
}

func TestTimedSegmentsForLines_UsesWordTimes(t *testing.T) {
	u := Utterance{
		Text:      "跳舞吗",
		StartTime: 0,
		EndTime:   400,
		Words: []Word{
			{Text: "跳", StartTime: 40, EndTime: 160},
			{Text: "舞", StartTime: 160, EndTime: 280},
			{Text: "吗", StartTime: 280, EndTime: 400},
		},
	}
	segs := TimedSegmentsForLines(u, []string{"跳舞", "吗"})
	if len(segs) != 2 {
		t.Fatalf("segs = %v, want 2 段", segs)
	}
	if segs[0].StartTime != 40 || segs[0].EndTime != 280 {
		t.Errorf("segs[0] = %+v, want 40-280", segs[0])
	}
	if segs[1].StartTime != 280 || segs[1].EndTime != 400 {
		t.Errorf("segs[1] = %+v, want 280-400", segs[1])
	}
}

func TestTimedSegmentsForLines_FallsBackProportional(t *testing.T) {
	// 行文本与词流不匹配（模拟外部断句与词流不一致）→ 整条比例分配，不报错、不产生负长度。
	u := Utterance{
		Text:      "跳舞吗",
		StartTime: 0,
		EndTime:   400,
		Words:     []Word{{Text: "跳", StartTime: 40, EndTime: 160}},
	}
	segs := TimedSegmentsForLines(u, []string{"跳舞", "吗"})
	if len(segs) != 2 {
		t.Fatalf("segs = %v, want 2 段", segs)
	}
	for i, s := range segs {
		if s.EndTime <= s.StartTime {
			t.Errorf("segs[%d] 非正长度: %+v", i, s)
		}
	}
	if segs[0].StartTime != 0 || segs[len(segs)-1].EndTime != 400 {
		t.Errorf("比例分配未覆盖整句: %+v", segs)
	}
}
