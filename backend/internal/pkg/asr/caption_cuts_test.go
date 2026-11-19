package asr

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitCaptionClauses(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "标点断句并剥掉边缘标点",
			text: "好的，今天我们来聊一聊人工智能在医疗领域的应用。",
			want: []string{"好的", "今天我们来聊一聊人工智能在医疗领域的应用"},
		},
		{
			name: "省略号也是断句标点",
			text: "嗯…这个的话",
			want: []string{"嗯", "这个的话"},
		},
		{
			name: "没有标点则整段一个子句",
			text: "这个颜色特别好看",
			want: []string{"这个颜色特别好看"},
		},
		{
			name: "首尾空白与标点被剥掉",
			text: "  今天天气不错！ ",
			want: []string{"今天天气不错"},
		},
		{
			name: "空文本没有子句",
			text: "   ",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitCaptionClauses(tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("子句数 = %d, want %d (%q)", len(got), len(tt.want), got)
			}
			for i, clause := range got {
				if clause.Text != tt.want[i] {
					t.Errorf("子句[%d] = %q, want %q", i, clause.Text, tt.want[i])
				}
				if want := utf8.RuneCountInString(clause.Text); clause.Runes != want {
					t.Errorf("子句[%d] Runes = %d, want %d", i, clause.Runes, want)
				}
			}
		})
	}
}

func TestSplitLinesByCuts_AfterTheCharAtPosition(t *testing.T) {
	// 位置口径：cuts[0]=8 表示第 8 个字（0 起）是「是」，切在「是」后面。
	text := "最直接的解决方法是升级到修复该问题的版本"
	got, stats, err := SplitLinesByCuts(text, []int{8}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	want := []string{"最直接的解决方法是", "升级到修复该问题的版本"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %q, want %q", got, want)
	}
	if stats.CutCount != 1 {
		t.Errorf("CutCount = %d, want 1", stats.CutCount)
	}
	if strings.Join(got, "") != text {
		t.Errorf("行拼接被改动: %q != %q", strings.Join(got, ""), text)
	}
}

func TestSplitLinesByCuts_TwoCuts(t *testing.T) {
	text := "这个颜色特别好看而且它的面料非常柔软穿起来很舒服"
	got, _, err := SplitLinesByCuts(text, []int{7, 17}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	want := []string{"这个颜色特别好看", "而且它的面料非常柔软", "穿起来很舒服"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %q, want %q", got, want)
	}
	if strings.Join(got, "") != text {
		t.Errorf("行拼接被改动: %q != %q", strings.Join(got, ""), text)
	}
}

func TestSplitLinesByCuts_SortsAndDedupesPositions(t *testing.T) {
	// 模型输出的顺序与重复不承载语义：去重排序后与规范输入等价。
	text := "这个颜色特别好看而且它的面料非常柔软穿起来很舒服"
	want, _, err := SplitLinesByCuts(text, []int{7, 17}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	got, _, err := SplitLinesByCuts(text, []int{17, 7, 7}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("乱序/重复位置 got = %q, want %q", got, want)
	}
}

func TestSplitLinesByCuts_SnapsCutInsideTokenToBoundary(t *testing.T) {
	// 位置落在英文词中间：不判死，吸附到最近的合法词边界（正文一字不动）。
	text := "我们聊 ChatGPT 可以吗"
	got, stats, err := SplitLinesByCuts(text, []int{6}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	want := []string{"我们聊", "ChatGPT 可以吗"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %q, want %q", got, want)
	}
	if stats.MovedCuts == 0 {
		t.Errorf("stats = %+v, 期望切点被挪动", stats)
	}
}

func TestSplitLinesByCuts_KeepsLinesWithinMaxRunes(t *testing.T) {
	text := "这款产品原价是一九九元现在直播间下单只要九八折相当于省了小两百块钱"
	got, _, err := SplitLinesByCuts(text, []int{6, 12, 18}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	if len(got) != 4 {
		t.Fatalf("行数 = %d, want 4 (%q)", len(got), got)
	}
	for i, line := range got {
		if n := utf8.RuneCountInString(line); n > MaxCaptionRunes {
			t.Errorf("lines[%d] = %q 长度 %d > %d", i, line, n, MaxCaptionRunes)
		}
	}
	if strings.Join(got, "") != text {
		t.Errorf("行拼接被改动: %q != %q", strings.Join(got, ""), text)
	}
}

func TestSplitLinesByCuts_RejectsUnusablePositions(t *testing.T) {
	text := "最直接的解决方法是升级到修复该问题的版本"
	n := utf8.RuneCountInString(text)
	tests := []struct {
		name   string
		cuts   []int
		reason string
	}{
		{name: "负数位置", cuts: []int{-1}, reason: CaptionLinesReasonCutOutOfRange},
		{name: "切在最后一个字之后", cuts: []int{n - 1}, reason: CaptionLinesReasonCutOutOfRange},
		{name: "位置越界", cuts: []int{999}, reason: CaptionLinesReasonCutOutOfRange},
		{name: "没有位置", cuts: nil, reason: CaptionLinesReasonUnsnappable},
		{name: "词边界排不下", cuts: []int{12, 25}, reason: CaptionLinesReasonUnsnappable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := text
			if tt.name == "词边界排不下" {
				target = "AAAAAAAAAAAA BBBBBBBBBBBB CCCCCCCCCCCC"
			}
			got, _, err := SplitLinesByCuts(target, tt.cuts, MaxCaptionRunes)
			if err == nil {
				t.Fatalf("期望报错，实际得到 %q", got)
			}
			if !errors.Is(err, ErrCaptionLines) {
				t.Errorf("errors.Is(err, ErrCaptionLines) = false, err = %v", err)
			}
			var linesErr *CaptionLinesError
			if !errors.As(err, &linesErr) || linesErr.Reason != tt.reason {
				t.Errorf("Reason = %v, want %s", err, tt.reason)
			}
		})
	}
}

func TestSplitLinesByCuts_MatchesRuleShape(t *testing.T) {
	// 切点来源不影响行的形态：与规则折行一样剥掉行首尾标点、行长不超上限。
	text := "最直接的解决方法是升级到修复该问题的版本。"
	got, _, err := SplitLinesByCuts(text, []int{8}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SplitLinesByCuts err = %v, want nil", err)
	}
	want := SplitLinesByRuleMax(text, MaxCaptionRunes)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got = %q, want %q（与规则折行同形态）", got, want)
	}
}
