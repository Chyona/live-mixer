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
// 1) words 与 live_asr 总数相等且按序 1:1（text/start/end）；
// 2) 每段 strip(text)==strip(join(words))；
// 3) 全文 text 拼接一致；4) 段级时间线合法；5) 单段 ≤200 字。
func TestGenerateASRParagraphs_FromASRRawJSONFiles(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{name: "asr_raw", file: "asr_raw.json"},
		{name: "live_asr01", file: "live_asr01.json"},
		{name: "live_asr02", file: "live_asr02.json"},
		{name: "live_asr03", file: "live_asr03.json"},
		{name: "live_asr04", file: "live_asr04.json"},
		{name: "live_asr05", file: "live_asr05.json"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			liveASR, durationMs := loadRepoLiveASRFixture(t, tc.file)
			assertASRParagraphsFromLiveASR(t, tc.file, liveASR, durationMs)
		})
	}
}

// TestGenerateASRParagraphs_FromLiveASR0NFiles 专门覆盖 live_asr01~05 作为 live_asr 输入。
func TestGenerateASRParagraphs_FromLiveASR0NFiles(t *testing.T) {
	cases := []string{
		"live_asr01.json",
		"live_asr02.json",
		"live_asr03.json",
		"live_asr04.json",
		"live_asr05.json",
	}
	for _, file := range cases {
		file := file
		t.Run(file, func(t *testing.T) {
			liveASR, durationMs := loadRepoLiveASRFixture(t, file)
			assertASRParagraphsFromLiveASR(t, file, liveASR, durationMs)
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

	wantWords := flattenUtteranceWords(utterances)
	gotWords := flattenParagraphWords(paras)
	if len(gotWords) != len(wantWords) {
		t.Fatalf("%s words 总数不一致: paragraphs=%d live_asr=%d", fixtureName, len(gotWords), len(wantWords))
	}
	for i := range wantWords {
		if gotWords[i] != wantWords[i] {
			t.Fatalf("%s words[%d] 非 1:1: got=%+v want=%+v", fixtureName, i, gotWords[i], wantWords[i])
		}
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
		if !asrParagraphContentAligned(p.Text, p.Words) {
			t.Fatalf("%s paras[%d] strip(text)!=strip(join(words))\ntext=%q\nwords=%q\nstripped_text=%q\nstripped_words=%q",
				fixtureName, i,
				truncateRunes(p.Text, 80),
				truncateRunes(joinedClipWordText(p.Words), 80),
				truncateRunes(stripASRAlignPunct(p.Text), 80),
				truncateRunes(stripASRAlignPunct(joinedClipWordText(p.Words)), 80),
			)
		}
		if strings.TrimSpace(stripASRAlignPunct(p.Text)) != "" && p.EndTime <= p.StartTime {
			t.Fatalf("%s paras[%d] 时间非法: [%d,%d] text=%q", fixtureName, i, p.StartTime, p.EndTime, truncateRunes(p.Text, 40))
		}
		if strings.TrimSpace(stripASRAlignPunct(p.Text)) != "" && p.EndTime-p.StartTime <= 1 {
			t.Fatalf("%s paras[%d] 疑似幽灵时间线 [%d,%d] text=%q", fixtureName, i, p.StartTime, p.EndTime, truncateRunes(p.Text, 40))
		}
		fullPara.WriteString(p.Text)
	}
	if fullPara.String() != fullUT.String() {
		t.Fatalf("%s 段落拼接与 live_asr 全文不一致: para_len=%d ut_len=%d", fixtureName, fullPara.Len(), fullUT.Len())
	}
	if err := validateASRParagraphWordIdentity(utterances, paras); err != nil {
		t.Fatalf("%s validateASRParagraphWordIdentity: %v", fixtureName, err)
	}
	if err := validateASRParagraphContentAlign(paras); err != nil {
		t.Fatalf("%s validateASRParagraphContentAlign: %v", fixtureName, err)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("%s validateASRParagraphTimeline: %v", fixtureName, err)
	}

	t.Logf("%s: utterances=%d paragraphs=%d words=%d duration_ms=%d",
		fixtureName, len(utterances), len(paras), len(gotWords), durationMs)
}

func TestTakeWordsForText_ASRPunctInTextOnly(t *testing.T) {
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
	// 不插入标点：join(words) 可以不等于 text。
	if joinedClipWordText(taken1) == c1 {
		t.Fatal("unexpected: taken should not include punctuation from text")
	}
}

func TestStitchASRParagraphs_PreservesSourceWordsWithPunctText(t *testing.T) {
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
	if err := validateASRParagraphWordIdentity(utterances, paras); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := validateASRParagraphContentAlign(paras); err != nil {
		t.Fatalf("content: %v", err)
	}
	if joinedClipWordText(paras[0].Words) == paras[0].Text {
		t.Fatal("expected join(words)!=text when text has punctuation")
	}
	if stripASRAlignPunct(paras[0].Text) != joinedClipWordText(paras[0].Words) {
		t.Fatalf("stripped text != join(words): %q vs %q",
			stripASRAlignPunct(paras[0].Text), joinedClipWordText(paras[0].Words))
	}
	if paras[0].StartTime != 100 || paras[0].EndTime != 200 {
		t.Fatalf("timeline=[%d,%d], want utterance bounds", paras[0].StartTime, paras[0].EndTime)
	}
}

func TestSplitASRParagraphBySentences_PreservesWordIdentity(t *testing.T) {
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
		Words:     append([]model.ClipWord(nil), words...),
	}
	got := splitASRParagraphBySentences(p, asrParagraphMaxRunes)
	finalizeASRParagraphTimeline(got, t0+100)
	flat := flattenParagraphWords(got)
	if len(flat) != len(words) {
		t.Fatalf("word count %d != %d", len(flat), len(words))
	}
	for i := range words {
		if flat[i] != words[i] {
			t.Fatalf("words[%d] changed: got=%+v want=%+v", i, flat[i], words[i])
		}
	}
	if err := validateASRParagraphContentAlign(got); err != nil {
		t.Fatalf("content: %v", err)
	}
	if err := validateASRParagraphTimeline(got); err != nil {
		t.Fatalf("timeline: %v", err)
	}
	var joined strings.Builder
	for i, seg := range got {
		if utf8.RuneCountInString(seg.Text) > asrParagraphMaxRunes {
			t.Fatalf("seg[%d] too long", i)
		}
		joined.WriteString(seg.Text)
	}
	if joined.String() != text {
		t.Fatal("joined text mismatch")
	}
}

// loadRepoLiveASRFixture 读取仓库根目录 ASR 样例。
// 支持两种格式：
//  1. 直接 live_asr（含 result.utterances），如 asr_raw.json / live_asr0N.json；
//  2. 调试包装 {"duration_ms":...,"live_asr":{...}}。
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

func loadRepoASRRawJSON(t *testing.T) string {
	t.Helper()
	liveASR, _ := loadRepoLiveASRFixture(t, "asr_raw.json")
	return liveASR
}
