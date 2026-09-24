package asr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSplitBalancedPreferLatin_Table 只断言与词表无关的性质：行数、不超宽、不丢字、无孤行。
// 精确到每行长度的断言留给 balancedCut 的单测——本用例的语料是十连汉字循环，
// 里面会撞上真实词（如「万丈」「三一」），切点被词边界挡住后长度分布会平移，不是缺陷。
func TestSplitBalancedPreferLatin_Table(t *testing.T) {
	mk := func(n int) string {
		b := make([]rune, n)
		for i := range b {
			b[i] = rune('一' + i%10)
		}
		return string(b)
	}
	tests := []struct {
		n     int
		parts int
	}{
		{12, 1},
		{13, 2},
		{24, 2},
		{25, 3},
		{31, 3},
	}
	for _, tt := range tests {
		text := mk(tt.n)
		got := splitBalancedPreferLatin(text, MaxCaptionRunes)
		if len(got) != tt.parts {
			t.Fatalf("n=%d lines=%d want %d: %v", tt.n, len(got), tt.parts, got)
		}
		if joined := strings.Join(got, ""); joined != text {
			t.Fatalf("n=%d joined %q != original", tt.n, joined)
		}
		for i, line := range got {
			n := utf8.RuneCountInString(line)
			if n > MaxCaptionRunes {
				t.Errorf("n=%d line[%d] len=%d exceeds max", tt.n, i, n)
			}
			if n < 3 {
				t.Errorf("n=%d line[%d] is an orphan: %q", tt.n, i, line)
			}
		}
	}
}

func TestBalancedCut_Ideals(t *testing.T) {
	tests := []struct {
		n, parts, idx, want int
	}{
		{12, 1, 1, 12},
		{13, 2, 1, 7},
		{13, 2, 2, 13},
		{25, 3, 1, 9},
		{25, 3, 2, 17},
		{31, 3, 1, 11},
		{31, 3, 2, 21},
		{31, 3, 3, 31},
	}
	for _, tt := range tests {
		if got := balancedCut(tt.n, tt.parts, tt.idx); got != tt.want {
			t.Errorf("balancedCut(%d,%d,%d)=%d want %d", tt.n, tt.parts, tt.idx, got, tt.want)
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
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "100")
}

func TestSplitBalancedPreferIntact_KeepsPercentIntact(t *testing.T) {
	text := "优惠力度达到100%真的很香" // 14 runes → 7+7
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "100%")
}

func TestSplitBalancedPreferIntact_LongNumber(t *testing.T) {
	num := "1234567890123" // 13 digits > 12
	got := splitBalancedPreferIntact(num, MaxCaptionRunes, nil)
	if len(got) != 1 || got[0] != num {
		t.Fatalf("got %v, want whole number on one line", got)
	}
}

// TestSplitBalancedPreferIntact_CutsNearTarget 覆盖切点选择：切点应落在离均分目标最近的
// 原子边界上，而不是「累加到超过目标就封口」。后者的封口位置由原子大小决定，
// 会在离目标很远的地方断行（下面两个用例在改动前分别产出 4 行/3 行且带 1~2 字的孤行）。
func TestSplitBalancedPreferIntact_CutsNearTarget(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
		// token 非空时额外断言该词没被行边界切开。
		token string
	}{
		{
			// 「人工智能」= 人工 + 智能 两个词表词，切在两者之间会把它拦腰截断
			name:  "词表词交界不被当作理想切点",
			text:  "但是因为你不在 AI 不在人工智能的那个聚光灯下面",
			want:  []string{"但是因为你不在", "AI 不在人工智能", "的那个聚光灯下面"},
			token: "人工智能",
		},
		{
			name: "凑不到目标位时不留下孤行",
			text: "这就是一个正在发生的一个房地产市场的 K 化",
			want: []string{"这就是一个正在发生的", "一个房地产市场的 K 化"},
		},
		{
			// 理想切点(8)落在「上市公司」内部，取靠前的 7：该词整体留给下一行
			name: "理想切点落在词内时往前让位",
			text: "这些行业所有的上市公司跌了20%",
			want: []string{"这些行业所有的", "上市公司跌了20%"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitLinesByRule(tt.text)
			// 行边界落在词间空格上时该空格会被 trimCaptionEdgePunct 剥掉（既有产品规则），
			// 因此比的是剥标点/空白后的内容：内容字符一律不许增删改。
			if joined := stripBreakPunct(strings.Join(got, "")); joined != stripBreakPunct(tt.text) {
				t.Fatalf("joined %q != original %q（剥标点后 %q vs %q）",
					strings.Join(got, ""), tt.text, joined, stripBreakPunct(tt.text))
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for i, line := range got {
				if line != tt.want[i] {
					t.Errorf("line[%d] = %q, want %q (all: %q)", i, line, tt.want[i], got)
				}
			}
			if tt.token != "" {
				assertTokenIntactAcrossLines(t, got, tt.token)
			}
		})
	}
}

// TestSplitBalancedPreferIntact_NoOverWideLine 覆盖行数：词/数字原子挡住目标位时应多切一行，
// 而不是把该行挤到 max 以上（超宽行在成片里会溢出安全区）。
func TestSplitBalancedPreferIntact_NoOverWideLine(t *testing.T) {
	// 24 字：硬按 ceil(24/12)=2 行时，理想切点 12 落在「中国」内部，第 2 行会变成 13 字
	text := "我描写了1978年以后中国经济发展的企业变革历史"
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
	if strings.Join(got, "") != text {
		t.Fatalf("joined %q != original", strings.Join(got, ""))
	}
	if len(got) != 3 {
		t.Fatalf("got %q, want 3 lines", got)
	}
	for i, line := range got {
		if n := utf8.RuneCountInString(line); n > MaxCaptionRunes {
			t.Errorf("line[%d] = %q 超宽 %d 字", i, line, n)
		}
		if n := utf8.RuneCountInString(line); n < 3 {
			t.Errorf("line[%d] = %q 孤行 %d 字", i, line, n)
		}
	}
}

// TestSplitBalancedPreferIntact_OrphanFree 覆盖孤行：目标位附近有可选边界时，
// 不应产出少于 3 个字的行——改动前「报告」这类尾行孤行占超长小句的近四分之一。
func TestSplitBalancedPreferIntact_OrphanFree(t *testing.T) {
	texts := []string{
		"我觉得我们直播间的同学可以把重新学习打在直播间里",
		"甚至我们的资产在全球化的过程中还会受到蒙代尔不可能三角的挑战",
		"这件事情是一个实实在在的在我们投资过程中给大家造成最大损失的一个主要原因",
		"那么我们就来送出这个价值2999块的 AI 悟空机器人",
	}
	for _, text := range texts {
		got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
		if strings.Join(got, "") != text {
			t.Fatalf("joined %q != original %q", strings.Join(got, ""), text)
		}
		for i, line := range got {
			if n := utf8.RuneCountInString(line); n < 3 {
				t.Errorf("%q: line[%d] = %q 只有 %d 字（孤行）", text, i, line, n)
			}
		}
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
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
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

// 厂商 words 会在拉丁/数字旁、标点后塞入空格填充词（start=end=-1）。它们不该打断词级对齐：
// 一个空格就让整条切片（可达数十秒）退化成按字数均分，字幕会随语速/停顿漂移数秒。
func TestTimedSegmentsForLines_SkipsFillerSpaceWords(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   8000,
		Text:      "今天我们聊 K 型时代，再见",
		Words: []Word{
			{Text: "今", StartTime: 0, EndTime: 200},
			{Text: "天", StartTime: 200, EndTime: 400},
			{Text: "我", StartTime: 400, EndTime: 600},
			{Text: "们", StartTime: 600, EndTime: 800},
			{Text: "聊", StartTime: 800, EndTime: 1000},
			{Text: " ", StartTime: -1, EndTime: -1},
			{Text: "K", StartTime: 3000, EndTime: 3600},
			{Text: " ", StartTime: -1, EndTime: -1},
			{Text: "型", StartTime: 3600, EndTime: 3800},
			{Text: "时", StartTime: 3800, EndTime: 4000},
			{Text: "代", StartTime: 4000, EndTime: 4200},
			{Text: "再", StartTime: 7000, EndTime: 7500},
			{Text: "见", StartTime: 7500, EndTime: 8000},
		},
	}
	segs, source := TimedSegmentsForLinesWithSource(u, []string{"今天我们聊 K 型时代", "再见"})
	if source != CaptionTimingWords {
		t.Fatalf("source = %q, want words（空格填充词不该打断对齐）", source)
	}
	if len(segs) != 2 {
		t.Fatalf("len = %d, want 2", len(segs))
	}
	if segs[0].StartTime != 0 || segs[0].EndTime != 4200 {
		t.Errorf("seg0 = %d-%d, want 0-4200（真实停顿不被均分抹平）", segs[0].StartTime, segs[0].EndTime)
	}
	if segs[1].StartTime != 7000 || segs[1].EndTime != 8000 {
		t.Errorf("seg1 = %d-%d, want 7000-8000", segs[1].StartTime, segs[1].EndTime)
	}
}

// 小数点 '.' 属于断句标点，会被 stripBreakPunct 从待对齐文本里去掉，但它在词流里是存在的：
// 同样要跳过，不能当作不匹配。
func TestTimedSegmentsForLines_KeepsDecimalPointWord(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   3000,
		Text:      "增长了2.4个百分点",
		Words: []Word{
			{Text: "增", StartTime: 0, EndTime: 300},
			{Text: "长", StartTime: 300, EndTime: 600},
			{Text: "了", StartTime: 600, EndTime: 900},
			{Text: "2.4", StartTime: 900, EndTime: 1300},
			{Text: "个", StartTime: 1300, EndTime: 1500},
			{Text: "百", StartTime: 1500, EndTime: 1700},
			{Text: "分", StartTime: 1700, EndTime: 1900},
			{Text: "点", StartTime: 1900, EndTime: 3000},
		},
	}
	segs, source := TimedSegmentsForLinesWithSource(u, []string{"增长了2.4", "个百分点"})
	if source != CaptionTimingWords {
		t.Fatalf("source = %q, want words（小数点不该打断对齐）", source)
	}
	if len(segs) != 2 || segs[0].EndTime != 1300 || segs[1].StartTime != 1300 {
		t.Fatalf("segs = %#v", segs)
	}
}

// 词流里没有有效时间的占位词不产生 -1 时间。
func TestTimedSegmentsForLines_NoTimeWordKeepsLineTime(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   2000,
		Text:      "今天",
		Words: []Word{
			{Text: "今", StartTime: -1, EndTime: -1},
			{Text: "天", StartTime: 1200, EndTime: 2000},
		},
	}
	segs, source := TimedSegmentsForLinesWithSource(u, []string{"今天"})
	if source != CaptionTimingWords {
		t.Fatalf("source = %q, want words", source)
	}
	if segs[0].StartTime != 1200 || segs[0].EndTime != 2000 {
		t.Fatalf("seg = %d-%d, want 1200-2000（不得出现 -1）", segs[0].StartTime, segs[0].EndTime)
	}
}

// 文本真的对不上词流时才降级为比例分配，并如实标注来源。
func TestTimedSegmentsForLines_RealMismatchFallsBack(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   4000,
		Text:      "明天更好",
		Words: []Word{
			{Text: "今", StartTime: 0, EndTime: 500},
			{Text: "天", StartTime: 500, EndTime: 1000},
			{Text: "很", StartTime: 1000, EndTime: 1500},
			{Text: "好", StartTime: 1500, EndTime: 4000},
		},
	}
	segs, source := TimedSegmentsForLinesWithSource(u, []string{"明天", "更好"})
	if source != CaptionTimingProportional {
		t.Fatalf("source = %q, want proportional", source)
	}
	if len(segs) != 2 || segs[0].StartTime != 0 || segs[0].EndTime != 2000 || segs[1].EndTime != 4000 {
		t.Fatalf("segs = %#v", segs)
	}
	for _, seg := range segs {
		if seg.TimingSource != CaptionTimingProportional {
			t.Errorf("seg %q TimingSource = %q", seg.Text, seg.TimingSource)
		}
	}
}

// 规则折行入口同样带上时间来源标注。
func TestSplitUtteranceForCaptions_ReportsTimingSource(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   6000,
		Text:      "好我们聊 K 型时代",
		Words: []Word{
			{Text: "好", StartTime: 0, EndTime: 300},
			{Text: "我", StartTime: 300, EndTime: 600},
			{Text: "们", StartTime: 600, EndTime: 900},
			{Text: "聊", StartTime: 900, EndTime: 1200},
			{Text: " ", StartTime: -1, EndTime: -1},
			{Text: "K", StartTime: 3000, EndTime: 3600},
			{Text: " ", StartTime: -1, EndTime: -1},
			{Text: "型", StartTime: 3600, EndTime: 3900},
			{Text: "时", StartTime: 3900, EndTime: 4200},
			{Text: "代", StartTime: 4200, EndTime: 6000},
		},
	}
	got := SplitUtteranceForCaptions(u)
	if len(got) == 0 {
		t.Fatal("expected segments")
	}
	for _, seg := range got {
		if seg.TimingSource != CaptionTimingWords {
			t.Errorf("seg %q TimingSource = %q, want words", seg.Text, seg.TimingSource)
		}
	}
}

func TestCaptionLexiconSize(t *testing.T) {
	n := CaptionLexiconSize()
	if n < 300 {
		t.Fatalf("lexicon size=%d, want at least ~300 finance terms", n)
	}
	ensureCaptionLexicon()
	for w := range captionLexiconSet {
		r := utf8.RuneCountInString(w)
		if r != 2 && r != 3 {
			t.Fatalf("word %q has %d runes, want 2 or 3", w, r)
		}
	}
	for _, w := range []string{"生产", "产生", "我们", "因为", "所以", "问题"} {
		if _, ok := captionLexiconSet[w]; !ok {
			t.Errorf("missing common word %q", w)
		}
	}
}

func TestSplitBalancedPreferIntact_KeepsChineseWordIntact(t *testing.T) {
	// 构造超长句，使均分切点容易落在「金融」「银行」中间。
	text := "今天我们来聊聊金融银行市场和经济投资风险问题"
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "金融")
	assertTokenIntactAcrossLines(t, got, "银行")
	assertTokenIntactAcrossLines(t, got, "市场")
	assertTokenIntactAcrossLines(t, got, "经济")
	assertTokenIntactAcrossLines(t, got, "投资")
	assertTokenIntactAcrossLines(t, got, "风险")
	for _, line := range got {
		if utf8.RuneCountInString(line) > MaxCaptionRunes {
			t.Errorf("line too long: %q (%d)", line, utf8.RuneCountInString(line))
		}
	}
}

func TestSplitBalancedPreferIntact_ChineseAndEnglishIntact(t *testing.T) {
	text := "一二三四五六七ChatGPT金融八九十"
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	assertTokenIntactAcrossLines(t, got, "ChatGPT")
	assertTokenIntactAcrossLines(t, got, "金融")
}

func TestSplitBalancedPreferIntact_ExtraASRWords(t *testing.T) {
	// 「蓝莓果酱」不在静态词表；通过 extra 注入后应整词保留。
	text := "一二三四五六七蓝莓果酱八九十十一"
	extra := map[string]struct{}{"蓝莓果酱": {}}
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, extra)
	assertTokenIntactAcrossLines(t, got, "蓝莓果酱")
}

func TestSplitBalancedPreferIntact_LongChineseWordExceedsMax(t *testing.T) {
	// 通过 extra 注入超长词（>12），应整词独占一行并允许超 max。
	word := "一二三四五六七八九十一二三"
	extra := map[string]struct{}{word: {}}
	got := splitBalancedPreferIntact(word, MaxCaptionRunes, extra)
	if len(got) != 1 || got[0] != word {
		t.Fatalf("got %v, want whole long word on one line", got)
	}
	if utf8.RuneCountInString(got[0]) <= MaxCaptionRunes {
		t.Fatalf("expected word longer than max, got %d", utf8.RuneCountInString(got[0]))
	}
}

func TestSplitUtteranceForCaptions_ChineseWordsIntact(t *testing.T) {
	u := Utterance{
		StartTime: 0,
		EndTime:   12000,
		Text:      "今天我们来聊聊金融银行市场和经济投资风险问题",
		Words: []Word{
			{Text: "金融", StartTime: 3000, EndTime: 4000},
			{Text: "银行", StartTime: 4000, EndTime: 5000},
		},
	}
	got := SplitUtteranceForCaptions(u)
	if len(got) < 2 {
		t.Fatalf("expected multiple segments, got %#v", got)
	}
	lines := make([]string, len(got))
	for i, seg := range got {
		lines[i] = seg.Text
		assertNoEdgePunct(t, seg.Text)
		if utf8.RuneCountInString(seg.Text) > MaxCaptionRunes {
			t.Errorf("seg too long: %q", seg.Text)
		}
	}
	assertTokenIntactAcrossLines(t, lines, "金融")
	assertTokenIntactAcrossLines(t, lines, "银行")
}

func TestSplitBalancedPreferIntact_KeepsCommonWordsIntact(t *testing.T) {
	text := "企业通过生产活动产生了大量的新产品和新需求"
	got := splitBalancedPreferIntact(text, MaxCaptionRunes, nil)
	joined := strings.Join(got, "")
	if joined != text {
		t.Fatalf("joined %q != original", joined)
	}
	for _, word := range []string{"通过", "生产", "活动", "产生", "大量", "产品", "需求"} {
		assertTokenIntactAcrossLines(t, got, word)
	}
	for _, line := range got {
		if utf8.RuneCountInString(line) > MaxCaptionRunes {
			t.Errorf("line too long: %q (%d)", line, utf8.RuneCountInString(line))
		}
	}
}

func TestMatchCJKWord_ForwardMax(t *testing.T) {
	runes := []rune("金融市场")
	end := matchCJKWord(runes, 0, nil)
	if end != 2 || string(runes[0:end]) != "金融" {
		t.Fatalf("got end=%d word=%q, want 金融", end, string(runes[0:end]))
	}
	end = matchCJKWord(runes, 2, nil)
	if end != 4 || string(runes[2:end]) != "市场" {
		t.Fatalf("got end=%d word=%q, want 市场", end, string(runes[2:end]))
	}
}

// —— L1：频率词表 + 最大概率分词 ——

func TestCaptionWordEnds_MaxProbability(t *testing.T) {
	tests := []struct {
		text string
		want string // 分词结果，用 / 连接
	}{
		// 4 字词：静态小词表只收 2–3 字词，给不出这个边界
		{"中产阶级崛起", "中产阶级/崛起"},
		// 最大概率分词优于正向最大匹配：研究生/命/起源 的总概率低于 研究/生命/起源
		{"研究生命起源", "研究/生命/起源"},
		{"消费者信心指数回升", "消费者/信心/指数/回升"},
		{"通货膨胀预期管理", "通货膨胀/预期/管理"},
		// 4 字机构名与后缀：市场/监督/管理局（不是 市场监督/管理局）
		{"市场监督管理局", "市场/监督/管理局"},
	}
	for _, tt := range tests {
		runes := []rune(tt.text)
		ends := captionWordEnds(runes, nil)
		var words []string
		for i := 0; i < len(runes); {
			j := ends[i]
			if j <= i || j > len(runes) {
				t.Fatalf("%s: 非法词结束位置 ends[%d]=%d", tt.text, i, j)
			}
			words = append(words, string(runes[i:j]))
			i = j
		}
		if got := strings.Join(words, "/"); got != tt.want {
			t.Errorf("%s: 分词 %s，期望 %s", tt.text, got, tt.want)
		}
	}
}

func TestCaptionWordEnds_ExtraWordsWin(t *testing.T) {
	// extra 是调用方强制的词，即使与词表更短的词冲突也整段成词。
	text := "蓝莓果酱真的好吃"
	runes := []rune(text)
	ends := captionWordEnds(runes, map[string]struct{}{"蓝莓果酱": {}})
	if got := string(runes[0:ends[0]]); got != "蓝莓果酱" {
		t.Fatalf("ends[0]=%d 得 %q，期望整段「蓝莓果酱」", ends[0], got)
	}
}

func TestTokenizeCaptionAtomsForSplit_CutsAtWordBoundaries(t *testing.T) {
	atoms := tokenizeCaptionAtomsForSplit("中产阶级崛起AI时代", nil)
	var texts []string
	for _, a := range atoms {
		texts = append(texts, a.text)
		if !a.keepIntact {
			t.Errorf("原子 %q 应为不可拆（keepIntact）", a.text)
		}
	}
	if got := strings.Join(texts, "|"); got != "中产阶级|崛起|AI|时代" {
		t.Fatalf("原子划分 = %s，期望 中产阶级|崛起|AI|时代", got)
	}
}

func TestSplitLinesByRule_KeepsLongWordsIntact(t *testing.T) {
	// 这些 4 字词在 L2（只有 2–3 字小词表）时代会被从中间切开：
	// 中产|阶级、消费观|念、信|心指数。词表换成频率词表后整词保留。
	tests := []struct {
		text string
		word string
	}{
		{"我们今天来聊一聊中产阶级的消费降级现象", "中产阶级"},
		{"中产阶级的消费观念正在发生改变", "消费观念"},
		{"我们看到消费者的信心指数在持续回升", "信心指数"},
	}
	for _, tt := range tests {
		lines := SplitLinesByRule(tt.text)
		if joined := strings.Join(lines, ""); joined != tt.text {
			t.Fatalf("%s: 折行 %q 丢了字", tt.text, joined)
		}
		assertTokenIntactAcrossLines(t, lines, tt.word)
		for _, line := range lines {
			if n := utf8.RuneCountInString(line); n > MaxCaptionRunes {
				t.Errorf("%s: 行 %q 超宽 %d", tt.text, line, n)
			}
		}
	}
}

// TestValidateCaptionLines_SmallLexiconStillLenient 钉住校验路径的现状：它只认 2–3 字小词表，
// 4 字词内部的切点会被放行。这是刻意的——校验一旦按大词表判「切在词内部」就整条拒绝，
// LLM 断句会大面积回退规则折行。要收紧得先把非法切点吸附到最近合法边界（captionWordEnds）。
func TestValidateCaptionLines_SmallLexiconStillLenient(t *testing.T) {
	text := "中产阶级的消费观念正在发生改变"
	lines := []string{"中产阶", "级的消费观念正在发生改变"} // 切在「中产阶级」内部
	valid, err := ValidateCaptionLines(text, lines, MaxCaptionRunes)
	if err != nil {
		t.Fatalf("校验不该拒绝：%v", err)
	}
	if joined := strings.Join(valid, ""); joined != text {
		t.Fatalf("校验后 %q != 原文", joined)
	}
}
