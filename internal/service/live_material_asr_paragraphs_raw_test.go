package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
)

// TestGenerateASRParagraphs_FromASRRawJSONFiles 分别以仓库根目录下多个 ASR 样例为 live_asr，
// 走与 worker 相同的 paragraphs 管线（LLM 不可用时窗内本地合并），严格校验：
// 1) 每段 join(words)==text；2) 全文拼接一致；3) 时间线合法；4) 单段 ≤200 字。
func TestGenerateASRParagraphs_FromASRRawJSONFiles(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{name: "asr_raw", file: "asr_raw.json"},
		{name: "001_asr_raw", file: "001_asr_raw.json"},
		{name: "002_asr_raw", file: "002_asr_raw.json"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			liveASR, durationMs := loadRepoLiveASRFixture(t, tc.file)
			assertASRParagraphsFromLiveASR(t, tc.file, liveASR, durationMs)
		})
	}
}

// TestGenerateASRParagraphs_FromASRRawJSON 保留旧名，覆盖 asr_raw.json。
func TestGenerateASRParagraphs_FromASRRawJSON(t *testing.T) {
	liveASR, durationMs := loadRepoLiveASRFixture(t, "asr_raw.json")
	assertASRParagraphsFromLiveASR(t, "asr_raw.json", liveASR, durationMs)
}

func assertASRParagraphsFromLiveASR(t *testing.T, fixtureName, liveASR string, durationMs int64) {
	t.Helper()
	utterances := asr.FormatUtterancesForAPI(liveASR)
	if len(utterances) == 0 {
		t.Fatalf("%s 未解析出 utterances", fixtureName)
	}
	if durationMs <= 0 {
		durationMs = utterances[len(utterances)-1].EndTime
	}
	if durationMs <= 0 {
		t.Fatalf("%s duration_ms 无效", fixtureName)
	}

	// 不依赖真实 LLM：返回非法 JSON，触发窗内本地相邻合并（与生产兜底一致）。
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "not-json", nil
		},
	}

	paras, err := generateASRParagraphs(context.Background(), llmClient, utterances, durationMs, nil, nil)
	if err != nil {
		t.Fatalf("generateASRParagraphs(%s): %v", fixtureName, err)
	}
	if len(paras) == 0 {
		t.Fatalf("%s paragraphs 为空", fixtureName)
	}

	var fullUT strings.Builder
	for _, u := range utterances {
		fullUT.WriteString(u.Text)
	}
	var fullPara strings.Builder
	for i, p := range paras {
		if utf8.RuneCountInString(p.Text) > asrParagraphMaxRunes {
			t.Fatalf("%s paras[%d] runes=%d > %d", fixtureName, i, utf8.RuneCountInString(p.Text), asrParagraphMaxRunes)
		}
		got := joinedClipWordText(p.Words)
		if got != p.Text {
			t.Fatalf("%s paras[%d] join(words)!=text\ntext=%q\nwords=%q",
				fixtureName, i, truncateRunes(p.Text, 80), truncateRunes(got, 80))
		}
		if strings.TrimSpace(stripASRAlignPunct(p.Text)) != "" && len(p.Words) == 0 {
			t.Fatalf("%s paras[%d] 有正文但 words 为空: %q", fixtureName, i, truncateRunes(p.Text, 40))
		}
		if strings.TrimSpace(stripASRAlignPunct(p.Text)) != "" && p.EndTime <= p.StartTime {
			t.Fatalf("%s paras[%d] 时间非法: [%d,%d] text=%q", fixtureName, i, p.StartTime, p.EndTime, truncateRunes(p.Text, 40))
		}
		// 有正文的段不应被挤压成 1ms 幽灵段（finalize 在 words 错位时的典型产物）。
		if strings.TrimSpace(stripASRAlignPunct(p.Text)) != "" && p.EndTime-p.StartTime <= 1 {
			t.Fatalf("%s paras[%d] 疑似幽灵时间线 [%d,%d] text=%q", fixtureName, i, p.StartTime, p.EndTime, truncateRunes(p.Text, 40))
		}
		fullPara.WriteString(p.Text)
	}
	if fullPara.String() != fullUT.String() {
		t.Fatalf("%s 段落拼接与 live_asr 全文不一致: para_len=%d ut_len=%d", fixtureName, fullPara.Len(), fullUT.Len())
	}
	if err := validateASRParagraphWords(paras); err != nil {
		t.Fatalf("%s validateASRParagraphWords: %v", fixtureName, err)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("%s validateASRParagraphTimeline: %v", fixtureName, err)
	}

	t.Logf("%s: utterances=%d paragraphs=%d duration_ms=%d", fixtureName, len(utterances), len(paras), durationMs)
}

func TestTakeWordsForText_ASRPunctInTextOnly(t *testing.T) {
	// 模拟豆包：text 含标点，words 无标点。
	words := []model.ClipWord{
		{Text: "知", StartTime: 1, EndTime: 2},
		{Text: "道", StartTime: 2, EndTime: 3},
		{Text: "哪", StartTime: 3, EndTime: 4},
		{Text: "个", StartTime: 4, EndTime: 5},
		{Text: "关", StartTime: 5, EndTime: 6},
		{Text: "吗", StartTime: 6, EndTime: 7},
		{Text: "知", StartTime: 8, EndTime: 9},
		{Text: "道", StartTime: 9, EndTime: 10},
		{Text: "啊", StartTime: 10, EndTime: 11},
		{Text: "这", StartTime: 11, EndTime: 12},
		{Text: "首", StartTime: 12, EndTime: 13},
		{Text: "就", StartTime: 13, EndTime: 14},
		{Text: "停", StartTime: 14, EndTime: 15},
		{Text: "了", StartTime: 15, EndTime: 16},
	}
	c1 := "知道哪个关吗？"
	c2 := "知道啊这首就停了。"
	taken1, rest := takeWordsForText(words, c1)
	if joinedClipWordText(taken1) != "知道哪个关吗" {
		t.Fatalf("taken1=%q", joinedClipWordText(taken1))
	}
	taken2, rest2 := takeWordsForText(rest, c2)
	if joinedClipWordText(taken2) != "知道啊这首就停了" {
		t.Fatalf("taken2=%q", joinedClipWordText(taken2))
	}
	if len(rest2) != 0 {
		t.Fatalf("rest2=%v", rest2)
	}
	s1 := syncWordsToText(taken1, c1, 1, 7)
	s2 := syncWordsToText(taken2, c2, 8, 16)
	if joinedClipWordText(s1) != c1 {
		t.Fatalf("sync1=%q want %q", joinedClipWordText(s1), c1)
	}
	if joinedClipWordText(s2) != c2 {
		t.Fatalf("sync2=%q want %q", joinedClipWordText(s2), c2)
	}
}

func TestSyncWordsToText_KeepsDecimalPoint(t *testing.T) {
	text := "增长了2.4，跌了0.6%。"
	words := []model.ClipWord{
		{Text: "增", StartTime: 0, EndTime: 1},
		{Text: "长", StartTime: 1, EndTime: 2},
		{Text: "了", StartTime: 2, EndTime: 3},
		{Text: "2.4", StartTime: 3, EndTime: 4},
		{Text: "跌", StartTime: 5, EndTime: 6},
		{Text: "了", StartTime: 6, EndTime: 7},
		{Text: "0.6%", StartTime: 7, EndTime: 8},
	}
	got := syncWordsToText(words, text, 0, 10)
	if joinedClipWordText(got) != text {
		t.Fatalf("got=%q want=%q", joinedClipWordText(got), text)
	}
}

func TestStitchASRParagraphs_WordsEqualTextWithPunct(t *testing.T) {
	utterances := []asr.Utterance{
		{
			Speaker: "1", StartTime: 100, EndTime: 200,
			Text: "你好，世界。",
			Words: []asr.Word{
				{Text: "你", StartTime: 100, EndTime: 120},
				{Text: "好", StartTime: 120, EndTime: 140},
				{Text: "世", StartTime: 150, EndTime: 170},
				{Text: "界", StartTime: 170, EndTime: 200},
			},
		},
	}
	paras, err := stitchASRParagraphs(utterances, []asrParagraphRange{{StartIndex: 0, EndIndex: 0}})
	if err != nil {
		t.Fatalf("stitch: %v", err)
	}
	if err := validateASRParagraphWords(paras); err != nil {
		t.Fatalf("words: %v", err)
	}
	if paras[0].StartTime != 100 || paras[0].EndTime != 200 {
		t.Fatalf("timeline=[%d,%d], want utterance bounds", paras[0].StartTime, paras[0].EndTime)
	}
}

func TestSplitASRParagraphBySentences_RealASRStyleNoWordSteal(t *testing.T) {
	// 超长段：text 有句号，words 无句号；切分后不得互吞 words，且不得出现空 words。
	s1 := strings.Repeat("甲", 100) + "。啊，" + strings.Repeat("乙", 50) + "。"
	s2 := strings.Repeat("丙", 80) + "。"
	text := s1 + s2
	var words []model.ClipWord
	t0 := int64(1000)
	for _, r := range []rune(stripASRAlignPunct(text)) {
		words = append(words, model.ClipWord{Text: string(r), StartTime: t0, EndTime: t0 + 5})
		t0 += 5
	}
	p := model.ASRParagraph{
		Speaker:   "1",
		Text:      text,
		StartTime: 1000,
		EndTime:   t0,
		Words:     syncWordsToText(words, text, 1000, t0),
	}
	got := splitASRParagraphBySentences(p, asrParagraphMaxRunes)
	finalizeASRParagraphTimeline(got, t0+100)
	if err := validateASRParagraphWords(got); err != nil {
		t.Fatalf("words: %v", err)
	}
	if err := validateASRParagraphTimeline(got); err != nil {
		t.Fatalf("timeline: %v", err)
	}
	var joined strings.Builder
	for i, seg := range got {
		if utf8.RuneCountInString(seg.Text) > asrParagraphMaxRunes {
			t.Fatalf("seg[%d] too long", i)
		}
		if strings.TrimSpace(stripASRAlignPunct(seg.Text)) != "" && seg.EndTime-seg.StartTime <= 1 {
			t.Fatalf("seg[%d] ghost timeline: %+v", i, seg)
		}
		joined.WriteString(seg.Text)
	}
	if joined.String() != text {
		t.Fatal("joined text mismatch")
	}
}

// loadRepoLiveASRFixture 读取仓库根目录 ASR 样例。
// 支持两种格式：
//  1. 直接 live_asr（含 result.utterances），如 asr_raw.json；
//  2. 调试包装 {"duration_ms":...,"live_asr":{...}}，如 001/002_asr_raw.json。
func loadRepoLiveASRFixture(t *testing.T, name string) (liveASR string, durationMs int64) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	path := filepath.Join(root, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s 为空", name)
	}

	var wrapped struct {
		DurationMs int64           `json:"duration_ms"`
		LiveASR    json.RawMessage `json:"live_asr"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.LiveASR) > 0 && wrapped.LiveASR[0] == '{' {
		return string(wrapped.LiveASR), wrapped.DurationMs
	}
	return string(raw), 0
}

// loadRepoASRRawJSON 保留兼容：返回 asr_raw.json 原始 live_asr 文本。
func loadRepoASRRawJSON(t *testing.T) string {
	t.Helper()
	liveASR, _ := loadRepoLiveASRFixture(t, "asr_raw.json")
	return liveASR
}
