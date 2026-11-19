package asr

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSnapCaptionLines_KeepsLegalCutsUntouched(t *testing.T) {
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	lines := []string{"今天我们来聊一聊", "人工智能在医疗领域的应用"}
	got, stats, err := SnapCaptionLines(text, lines, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SnapCaptionLines err = %v, want nil", err)
	}
	if strings.Join(got, "|") != strings.Join(lines, "|") {
		t.Fatalf("合法切点不该被挪动: got %q, want %q", got, lines)
	}
	want := CaptionSnapStats{CutCount: 1}
	if stats != want {
		t.Errorf("stats = %+v, want %+v", stats, want)
	}
}

func TestSnapCaptionLines_SnapsCutInsideWord(t *testing.T) {
	// 模型把「中产阶级」切成「中产阶|级」：不整条判死，而是把切点挪到词边界（前移 1 个字）。
	text := "中产阶级的消费观念正在发生改变"
	got, stats, err := SnapCaptionLines(text, []string{"中产阶", "级的消费观念正在发生改变"}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SnapCaptionLines err = %v, want nil", err)
	}
	want := []string{"中产阶级", "的消费观念正在发生改变"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got = %q, want %q", got, want)
	}
	if wantStats := (CaptionSnapStats{CutCount: 1, MovedCuts: 1, Displaced: 1}); stats != wantStats {
		t.Errorf("stats = %+v, want %+v", stats, wantStats)
	}
}

func TestSnapCaptionLines_PreservesLineCountAndContent(t *testing.T) {
	// 吸附只挪行边界：行数不变，内容字符（除行边界可丢的标点/空格外）不变。
	tests := []struct {
		name  string
		text  string
		lines []string
	}{
		{
			name:  "中文词内部切点",
			text:  "中产阶级的消费观念正在发生改变",
			lines: []string{"中产阶", "级的消费观念正在发生改变"},
		},
		{
			name:  "英文词内部切点",
			text:  "我们聊 ChatGPT 可以吗",
			lines: []string{"我们聊 ChatGP", "T 可以吗"},
		},
		{
			name:  "丢掉行边界标点",
			text:  "这款产品原价是￥199，现在直播间下单只要98折，相当于省了小两百块钱。",
			lines: []string{"这款产品原价是￥199", "现在直播间下单只要98折", "相当于省了小两百块钱"},
		},
		{
			name:  "比行宽上限还长的英文原子独占一行",
			text:  "Supercalifragilistics 这个词",
			lines: []string{"Supercalifragilistics", " 这个词"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := SnapCaptionLines(tt.text, tt.lines, MaxCaptionRunes)
			if err != nil {
				t.Fatalf("SnapCaptionLines err = %v, want nil", err)
			}
			if len(got) != len(tt.lines) {
				t.Fatalf("行数 %d != %d: got %q", len(got), len(tt.lines), got)
			}
			// 行边界允许丢标点与空白，所以比较前先去掉它们（与校验口径一致）。
			if a, b := stripBreakPunct(strings.Join(got, "")), stripBreakPunct(strings.Join(tt.lines, "")); a != b {
				t.Errorf("内容被改动: got %q, want %q", a, b)
			}
			for i, line := range got {
				if strings.TrimSpace(line) == "" {
					t.Errorf("lines[%d] 为空白", i)
				}
			}
		})
	}
}

func TestSnapCaptionLines_IsIdempotent(t *testing.T) {
	// 断句器吸附一次、成片链路再走一次：第二次必须是恒等操作（否则两次校验会得到不同的行）。
	text := "中产阶级的消费观念正在发生改变"
	first, _, err := SnapCaptionLines(text, []string{"中产阶", "级的消费观念正在发生改变"}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("首次吸附 err = %v, want nil", err)
	}
	second, stats, err := SnapCaptionLines(text, first, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("再次吸附 err = %v, want nil", err)
	}
	if strings.Join(second, "|") != strings.Join(first, "|") {
		t.Fatalf("吸附不幂等: 第一次 %q，第二次 %q", first, second)
	}
	if want := (CaptionSnapStats{CutCount: len(first) - 1}); stats != want {
		t.Errorf("再次吸附 stats = %+v, want %+v（切点不该再挪）", stats, want)
	}
}

func TestSnapCaptionLines_KeepsOverlongAtomIntact(t *testing.T) {
	// 比行宽上限还长的单原子（超长英文词）只能整段独占一行，不能因为超宽就判整条非法。
	text := "Supercalifragilistics 这个词"
	got, _, err := SnapCaptionLines(text, []string{"Supercalifragilistics", " 这个词"}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SnapCaptionLines err = %v, want nil", err)
	}
	want := []string{"Supercalifragilistics", "这个词"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got = %q, want %q", got, want)
	}
}

func TestSnapCaptionLines_RejectsUnsnappableLayout(t *testing.T) {
	// 三个 12 字英文原子共 38 字，模型给 3 行排不下（每行都要塞下中间那个空格），只能回退规则折行。
	text := "AAAAAAAAAAAA BBBBBBBBBBBB CCCCCCCCCCCC"
	lines := []string{"AAAAAAAAAAAA", " BBBBBBBBBBBB", " CCCCCCCCCCCC"}
	_, _, err := SnapCaptionLines(text, lines, MaxCaptionRunes)
	if err == nil {
		t.Fatalf("unsnappable 应报错")
	}
	if !errors.Is(err, ErrCaptionLines) {
		t.Errorf("errors.Is(err, ErrCaptionLines) = false, err = %v", err)
	}
	var ce *CaptionLinesError
	if !errors.As(err, &ce) || ce.Reason != CaptionLinesReasonUnsnappable {
		t.Errorf("reason = %v, want %s", err, CaptionLinesReasonUnsnappable)
	}
}

func TestSnapCaptionLines_RejectsUnusableLines(t *testing.T) {
	// 内容侧的硬约束与 ValidateCaptionLines 一致：吸附只放宽「切点位置」，不放宽内容。
	text := "今天我们来聊一聊人工智能"
	tests := []struct {
		name   string
		lines  []string
		reason string
	}{
		{"无行", nil, CaptionLinesReasonEmpty},
		{"空白行", []string{"今天我们来聊一聊", "  "}, CaptionLinesReasonBlankLine},
		{"漏字", []string{"今天我们来聊一聊", "人工智"}, CaptionLinesReasonMismatch},
		{"改写", []string{"今天我们来聊一聊", "人工只能"}, CaptionLinesReasonMismatch},
		{"切碎", []string{"今", "天", "我", "们", "来", "聊", "一", "聊", "人", "工", "智", "能"}, CaptionLinesReasonTooFine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := SnapCaptionLines(text, tt.lines, MaxCaptionRunes)
			if err == nil {
				t.Fatalf("err = nil, want %s (got %v)", tt.reason, got)
			}
			var ce *CaptionLinesError
			if !errors.As(err, &ce) || ce.Reason != tt.reason {
				t.Errorf("reason = %v, want %s", err, tt.reason)
			}
		})
	}
}

func TestSnapCaptionLines_ResplitsSingleLineReply(t *testing.T) {
	// 模型只给一行（或超长行）：没有切点可吸附，交给 normalizeCaptionLines 按规则再切——与旧行为一致。
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	got, stats, err := SnapCaptionLines(text, []string{text}, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("SnapCaptionLines err = %v, want nil", err)
	}
	if len(got) < 2 {
		t.Fatalf("超长单行应被再切: %q", got)
	}
	for i, line := range got {
		if n := utf8.RuneCountInString(line); n > MaxCaptionRunes {
			t.Errorf("lines[%d] = %q 长度 %d > %d", i, line, n, MaxCaptionRunes)
		}
	}
	if want := (CaptionSnapStats{}); stats != want {
		t.Errorf("stats = %+v, want %+v", stats, want)
	}
}

func TestSnapCuts_PenalizesOrphanLine(t *testing.T) {
	// 原子宽度 1 / 2 / 5 / 12，理想切点在 1（离它最近的合法切点）：切在 1 会留下 1 字孤行，
	// 挪到 3 只多花 2 个字的位移，但省掉孤行惩罚（3）——所以该往后挪。
	cuts := []int{0, 1, 3, 8, 20}
	got, ok := snapCuts(cuts, []int{1, 16}, 20, MaxCaptionRunes)
	if !ok {
		t.Fatalf("snapCuts ok = false, want true")
	}
	want := []int{0, 3, 8, 20}
	if len(got) != len(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got = %v, want %v", got, want)
		}
	}
}

func TestSnapCuts_OrphanIsSoftPenalty(t *testing.T) {
	// 孤行只是软惩罚：这里避开 1 字孤行要多挪 11 个字，代价远大于惩罚，于是保留孤行。
	cuts := []int{0, 1, 12, 13}
	got, ok := snapCuts(cuts, []int{1}, 13, MaxCaptionRunes)
	if !ok {
		t.Fatalf("snapCuts ok = false, want true")
	}
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 13 {
		t.Fatalf("got = %v, want [0 1 13]", got)
	}
}

func TestSnapCuts_ReportsInfeasible(t *testing.T) {
	cuts := []int{0, 12, 13, 25, 26, 38}
	if _, ok := snapCuts(cuts, []int{12, 25}, 38, MaxCaptionRunes); ok {
		t.Fatal("snapCuts ok = true, want false（38 字排 3 行放不下）")
	}
	// 切点比原子还多：同样无解。
	if _, ok := snapCuts([]int{0, 20}, []int{4, 12}, 20, MaxCaptionRunes); ok {
		t.Fatal("snapCuts ok = true, want false（只有一个原子）")
	}
}
