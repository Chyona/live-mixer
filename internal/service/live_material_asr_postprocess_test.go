package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/webroot"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestFormatASRTranscriptLines(t *testing.T) {
	lines := formatASRTranscriptLines([]asr.Utterance{
		{Speaker: "1", StartTime: 40, EndTime: 400, Text: "你好"},
		{Speaker: "2", StartTime: 500, EndTime: 900, Text: "世界"},
	}, 0)
	if !strings.Contains(lines, "[0] speaker=1 t=40-400 你好") {
		t.Fatalf("unexpected lines: %s", lines)
	}
	if !strings.Contains(lines, "[1] speaker=2") {
		t.Fatalf("missing second line: %s", lines)
	}
}

func TestStitchASRParagraphs_OK(t *testing.T) {
	utterances := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 100, Text: "甲", Words: []asr.Word{{Text: "甲", StartTime: 0, EndTime: 100}}},
		{Speaker: "1", StartTime: 120, EndTime: 200, Text: "乙", Words: []asr.Word{{Text: "乙", StartTime: 120, EndTime: 200}}},
		{Speaker: "2", StartTime: 300, EndTime: 400, Text: "丙", Words: []asr.Word{{Text: "丙", StartTime: 300, EndTime: 400}}},
	}
	paras, err := stitchASRParagraphs(utterances, []asrParagraphRange{
		{StartIndex: 0, EndIndex: 1},
		{StartIndex: 2, EndIndex: 2},
	})
	if err != nil {
		t.Fatalf("stitchASRParagraphs() error = %v", err)
	}
	if len(paras) != 2 {
		t.Fatalf("len = %d, want 2", len(paras))
	}
	if paras[0].Text != "甲乙" || paras[0].Speaker != "1" {
		t.Errorf("para0 = %+v", paras[0])
	}
	if len(paras[0].Words) != 2 {
		t.Errorf("para0 words = %d, want 2", len(paras[0].Words))
	}
	if paras[1].Text != "丙" || paras[1].Speaker != "2" {
		t.Errorf("para1 = %+v", paras[1])
	}
}

func TestStitchASRParagraphs_MultiSpeakerRejected(t *testing.T) {
	utterances := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 100, Text: "甲"},
		{Speaker: "2", StartTime: 120, EndTime: 200, Text: "乙"},
	}
	_, err := stitchASRParagraphs(utterances, []asrParagraphRange{{StartIndex: 0, EndIndex: 1}})
	if err == nil || !strings.Contains(err.Error(), "多个说话人") {
		t.Fatalf("error = %v, want multi speaker", err)
	}
}

func TestValidateASRSummaries_EmptyOK(t *testing.T) {
	if err := validateASRSummaries(nil); err != nil {
		t.Fatalf("validateASRSummaries(nil) error = %v", err)
	}
	if err := validateASRSummaries([]model.ASRSummarySegment{}); err != nil {
		t.Fatalf("validateASRSummaries([]) error = %v", err)
	}
}

func TestValidateASRSummaries_TitleTooLongTruncated(t *testing.T) {
	segs := []model.ASRSummarySegment{
		{Title: "一二三四五六七", StartTime: 0, EndTime: 300000},
	}
	normalizeASRSummaries(segs, 600000)
	if err := validateASRSummaries(segs); err != nil {
		t.Fatalf("validateASRSummaries() error = %v", err)
	}
	if got := segs[0].Title; got != "一二三四五六" {
		t.Fatalf("Title = %q, want truncated to 6 runes", got)
	}
}

func TestFilterASRSummariesByDuration(t *testing.T) {
	segs := []model.ASRSummarySegment{
		{Title: "过短", StartTime: 0, EndTime: 60_000},                         // 1 分钟
		{Title: "合法", StartTime: 0, EndTime: 300_000},                        // 5 分钟
		{Title: "过长", StartTime: 0, EndTime: 65 * 60 * 1000},                 // 65 分钟
		{Title: "上限", StartTime: 0, EndTime: asrSummaryMaxDurationMs},        // 60 分钟
	}
	got := filterASRSummariesByDuration(segs, nil)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Title != "合法" || got[1].Title != "上限" {
		t.Fatalf("got = %+v", got)
	}
}

func TestFilterASRSummariesByDuration_AllDropped(t *testing.T) {
	got := filterASRSummariesByDuration([]model.ASRSummarySegment{
		{Title: "短", StartTime: 0, EndTime: 1000},
	}, nil)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestMergeASRSummaries_SameTitleAcrossGap(t *testing.T) {
	// 两段同主题，间隙 90s < 2min → 合并后时长满足 5 分钟。
	segs := []model.ASRSummarySegment{
		{Title: "产品讲解", StartTime: 0, EndTime: 180_000},             // 3 分钟
		{Title: "产品讲解", StartTime: 270_000, EndTime: 420_000},         // 间隙 90s，再 2.5 分钟
		{Title: "福利促销", StartTime: 500_000, EndTime: 800_000},
	}
	got := mergeASRSummaries(segs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 after merge", len(got))
	}
	if got[0].Title != "产品讲解" || got[0].StartTime != 0 || got[0].EndTime != 420_000 {
		t.Fatalf("merged = %+v", got[0])
	}
	if got[1].Title != "福利促销" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestMergeASRSummaries_GapTooLargeNotMerged(t *testing.T) {
	segs := []model.ASRSummarySegment{
		{Title: "开场", StartTime: 0, EndTime: 300_000},
		{Title: "开场", StartTime: 300_000 + asrSummaryMergeGapMs + 1, EndTime: 700_000},
	}
	got := mergeASRSummaries(segs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (gap too large)", len(got))
	}
}

func TestMergeASRSummaries_ThenFilterKeepsMerged(t *testing.T) {
	// 合并前各 <5 分钟，合并后 >=5 分钟，应保留。
	segs := []model.ASRSummarySegment{
		{Title: "讲解", StartTime: 0, EndTime: 180_000},
		{Title: "讲解", StartTime: 180_000, EndTime: 300_000},
	}
	got := filterASRSummariesByDuration(mergeASRSummaries(segs), nil)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].EndTime-got[0].StartTime != 300_000 {
		t.Fatalf("duration = %d", got[0].EndTime-got[0].StartTime)
	}
}

func TestDedupeContainedASRSummaries(t *testing.T) {
	segs := []model.ASRSummarySegment{
		{Title: "主题", StartTime: 0, EndTime: 600_000},
		{Title: "主题", StartTime: 100_000, EndTime: 200_000}, // 被包含
		{Title: "其它", StartTime: 0, EndTime: 300_000},
	}
	got := dedupeContainedASRSummaries(segs)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	titles := map[string]bool{}
	for _, s := range got {
		titles[s.Title] = true
		if s.Title == "主题" && (s.StartTime != 0 || s.EndTime != 600_000) {
			t.Fatalf("kept wrong theme segment: %+v", s)
		}
	}
	if !titles["主题"] || !titles["其它"] {
		t.Fatalf("titles = %v", titles)
	}
}

func TestReduceASRSummaries_PipelineOrder(t *testing.T) {
	// 模拟跨窗碎片：先合并再过滤，避免短段被提前丢掉。
	all := []model.ASRSummarySegment{
		{Title: " 产品讲解 ", StartTime: 0, EndTime: 200_000},
		{Title: "产品讲解", StartTime: 210_000, EndTime: 400_000}, // 间隙 10s
	}
	normalizeASRSummaries(all, 1_000_000)
	all = mergeASRSummaries(all)
	all = dedupeContainedASRSummaries(all)
	all = filterASRSummariesByDuration(all, nil)
	if err := validateASRSummaries(all); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(all) != 1 || all[0].Title != "产品讲解" {
		t.Fatalf("got = %+v", all)
	}
	if all[0].StartTime != 0 || all[0].EndTime != 400_000 {
		t.Fatalf("times = %d-%d", all[0].StartTime, all[0].EndTime)
	}
}

func TestParseASRSummaryItems_FromObject(t *testing.T) {
	items, err := parseASRSummaryItems("```json\n{\"items\":[{\"title\":\"主题\",\"start_index\":0,\"end_index\":0}]}\n```")
	if err != nil {
		t.Fatalf("parseASRSummaryItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Title != "主题" {
		t.Fatalf("items = %+v", items)
	}
}

func TestResolveASRSummaries_IndexToTime(t *testing.T) {
	win := utteranceWindow{
		Offset: 0,
		Utterances: []asr.Utterance{
			{StartTime: 0, EndTime: 1000, Text: "a"},
			{StartTime: 2000, EndTime: 5000, Text: "b"},
		},
	}
	segs, err := resolveASRSummaries([]asrSummaryLLMItem{
		{Title: "开场", StartIndex: 0, EndIndex: 1},
	}, win, 5000)
	if err != nil {
		t.Fatalf("resolveASRSummaries() error = %v", err)
	}
	if segs[0].StartTime != 0 || segs[0].EndTime != 5000 {
		t.Fatalf("times = %d-%d, want 0-5000", segs[0].StartTime, segs[0].EndTime)
	}
}

func TestResolveASRSummaries_EmptyItemsOK(t *testing.T) {
	segs, err := resolveASRSummaries(nil, utteranceWindow{Utterances: []asr.Utterance{{}}}, 1000)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(segs) != 0 {
		t.Fatalf("len = %d, want 0", len(segs))
	}
}

func TestSplitUtterancesByDuration_AlwaysWindows(t *testing.T) {
	uts := []asr.Utterance{
		{StartTime: 0, EndTime: 1000, Text: "a"},
		{StartTime: asrSummaryWindowMs + 1, EndTime: asrSummaryWindowMs + 2000, Text: "b"},
	}
	wins := splitUtterancesByDuration(uts, asrSummaryWindowMs, asrLLMWindowMaxRunes)
	if len(wins) != 2 {
		t.Fatalf("windows = %d, want 2", len(wins))
	}
}

func TestSplitUtterancesByDuration_RuneBudgetCreatesMultipleWindows(t *testing.T) {
	// 构造远超 asrLLMWindowMaxRunes 的句段列表（时长仍在单窗内），应被 rune 预算拆成多窗。
	const n = 80
	longText := strings.Repeat("测", 250) // 每行约 250+ 元数据 rune
	uts := make([]asr.Utterance, n)
	for i := 0; i < n; i++ {
		start := int64(i * 1000)
		uts[i] = asr.Utterance{
			Speaker:   "1",
			StartTime: start,
			EndTime:   start + 900,
			Text:      longText,
		}
	}
	totalRunes := utf8.RuneCountInString(formatASRTranscriptLines(uts, 0))
	if totalRunes <= asrLLMWindowMaxRunes {
		t.Fatalf("fixture too small: totalRunes=%d, budget=%d", totalRunes, asrLLMWindowMaxRunes)
	}

	wins := splitUtterancesByDuration(uts, asrSummaryWindowMs, asrLLMWindowMaxRunes)
	if len(wins) < 2 {
		t.Fatalf("windows = %d, want >= 2 (rune budget map)", len(wins))
	}
	covered := 0
	for i, win := range wins {
		lines := formatASRTranscriptLines(win.Utterances, 0)
		runes := utf8.RuneCountInString(lines)
		if len(win.Utterances) > 1 && runes > asrLLMWindowMaxRunes {
			t.Fatalf("window[%d] runes=%d > budget %d", i, runes, asrLLMWindowMaxRunes)
		}
		if win.Offset != covered {
			t.Fatalf("window[%d] offset=%d, want %d", i, win.Offset, covered)
		}
		covered += len(win.Utterances)
	}
	if covered != n {
		t.Fatalf("covered %d utterances, want %d", covered, n)
	}
}

func TestGenerateASRSummaries_MultiWindowReduce(t *testing.T) {
	// 两窗：第一窗 0..0，第二窗因时长切到下一小时；各返回一段，Reduce 后合并为 2。
	uts := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 300_000, Text: "开场内容足够长用于主题"},
		{Speaker: "1", StartTime: asrSummaryWindowMs, EndTime: asrSummaryWindowMs + 300_000, Text: "后半场内容足够长用于主题"},
	}
	var calls atomic.Int32
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls.Add(1)
			sys := ""
			user := ""
			for _, msg := range messages {
				switch msg.Role {
				case "system":
					sys = msg.Content
				case "user":
					user = msg.Content
				}
			}
			if strings.Contains(sys, "主题提炼") {
				if strings.Contains(user, "本窗句段编号范围：0~0") {
					return `{"items":[{"title":"开场","start_index":0,"end_index":0}]}`, nil
				}
				return `{"items":[{"title":"后半","start_index":1,"end_index":1}]}`, nil
			}
			// paragraphs：覆盖本窗
			if strings.Contains(user, "[0]") && !strings.Contains(user, "[1]") {
				return `{"items":[{"start_index":0,"end_index":0}]}`, nil
			}
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}
	dur := asrSummaryWindowMs + 300_000
	out, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRMultiUtteranceJSON(uts)), dur, nil, nil)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if len(out.Summaries) != 2 {
		t.Fatalf("summaries = %+v, want 2 after reduce", out.Summaries)
	}
	if out.Summaries[0].Title != "开场" || out.Summaries[1].Title != "后半" {
		t.Fatalf("summaries titles = %+v", out.Summaries)
	}
	if n := calls.Load(); n < 4 { // 2 summary windows + 2 paragraph windows
		t.Fatalf("calls = %d, want >= 4 for multi-window map", n)
	}
}

func TestGenerateASRParagraphs_MultiWindowOffsetStitch(t *testing.T) {
	uts := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 1000, Text: "甲", Words: asrWordsFromContentText("甲", 0, 1000)},
		{Speaker: "1", StartTime: asrParagraphWindowMs + 1, EndTime: asrParagraphWindowMs + 2000, Text: "乙", Words: asrWordsFromContentText("乙", asrParagraphWindowMs+1, asrParagraphWindowMs+2000)},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			sys := ""
			for _, msg := range messages {
				if msg.Role == "system" {
					sys = msg.Content
				}
			}
			if strings.Contains(sys, "主题提炼") {
				return `{"items":[]}`, nil
			}
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}
	dur := asrParagraphWindowMs + 2000
	paras, err := generateASRParagraphs(context.Background(), llmClient, uts, dur, nil, nil)
	if err != nil {
		t.Fatalf("generateASRParagraphs() error = %v", err)
	}
	if len(paras) != 2 {
		t.Fatalf("paragraphs = %+v, want 2", paras)
	}
	joined := paras[0].Text + paras[1].Text
	if joined != "甲乙" {
		t.Fatalf("joined text = %q, want 甲乙", joined)
	}
	if paras[0].StartTime != 0 || paras[len(paras)-1].EndTime != dur {
		t.Fatalf("timeline = %d-%d, want words bounds 0-%d", paras[0].StartTime, paras[len(paras)-1].EndTime, dur)
	}
}

// asrWordsFromContentText 按去句读正文逐字构造源 ASR words（不含标点）。
func asrWordsFromContentText(text string, start, end int64) []asr.Word {
	runes := []rune(stripASRAlignPunct(text))
	if len(runes) == 0 {
		return nil
	}
	span := end - start
	if span < int64(len(runes)) {
		span = int64(len(runes))
	}
	step := span / int64(len(runes))
	if step < 1 {
		step = 1
	}
	out := make([]asr.Word, 0, len(runes))
	t0 := start
	for i, r := range runes {
		t1 := t0 + step
		if i == len(runes)-1 {
			t1 = end
			if t1 < t0 {
				t1 = t0 + step
			}
		}
		out = append(out, asr.Word{Text: string(r), StartTime: t0, EndTime: t1})
		t0 = t1
	}
	return out
}

// clipWordsFromContentText 同 asrWordsFromContentText，输出 ClipWord。
func clipWordsFromContentText(text string, start, end int64) []model.ClipWord {
	ws := asrWordsFromContentText(text, start, end)
	out := make([]model.ClipWord, len(ws))
	for i, w := range ws {
		out[i] = model.ClipWord{Text: w.Text, StartTime: w.StartTime, EndTime: w.EndTime}
	}
	return out
}

// sampleLiveASRMultiUtteranceJSON 构造含多句段的豆包 ASR JSON，供后处理测试。
func sampleLiveASRMultiUtteranceJSON(uts []asr.Utterance) []byte {
	type word struct {
		Text      string `json:"text"`
		StartTime int64  `json:"start_time"`
		EndTime   int64  `json:"end_time"`
	}
	type utterance struct {
		Additions struct {
			Speaker string `json:"speaker"`
		} `json:"additions"`
		StartTime int64  `json:"start_time"`
		EndTime   int64  `json:"end_time"`
		Text      string `json:"text"`
		Words     []word `json:"words"`
	}
	payload := struct {
		AudioInfo struct {
			Duration int64 `json:"duration"`
		} `json:"audio_info"`
		Result struct {
			Utterances []utterance `json:"utterances"`
		} `json:"result"`
	}{}
	var maxEnd int64
	for _, u := range uts {
		if u.EndTime > maxEnd {
			maxEnd = u.EndTime
		}
		item := utterance{
			StartTime: u.StartTime,
			EndTime:   u.EndTime,
			Text:      u.Text,
		}
		item.Additions.Speaker = u.Speaker
		for _, w := range u.Words {
			item.Words = append(item.Words, word{Text: w.Text, StartTime: w.StartTime, EndTime: w.EndTime})
		}
		if len(item.Words) == 0 {
			item.Words = []word{{Text: u.Text, StartTime: u.StartTime, EndTime: u.EndTime}}
		}
		payload.Result.Utterances = append(payload.Result.Utterances, item)
	}
	payload.AudioInfo.Duration = maxEnd
	raw, _ := json.Marshal(payload)
	return raw
}

func TestRunASRPostprocess_EmptySummariesOK(t *testing.T) {
	// 短素材：summary 段会被时长过滤掉，paragraphs 仍成功，整体不失败。
	liveASR := string(sampleLiveASRJSON(1200, "你好世界"))
	out, err := runASRPostprocess(context.Background(), defaultWorkerLLM(), liveASR, 1200, nil, nil)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if out.Summaries == nil || len(out.Summaries) != 0 {
		t.Fatalf("Summaries = %#v, want empty non-nil", out.Summaries)
	}
	if len(out.Paragraphs) == 0 || out.Paragraphs[0].Text != "你好世界" {
		t.Fatalf("paragraphs = %+v", out.Paragraphs)
	}
}

func TestRunASRParagraphs_OnlyParagraphs(t *testing.T) {
	const dur = int64(10 * 60 * 1000)
	var summaryCalls atomic.Int32
	liveASR := string(sampleLiveASRJSON(dur, "你好世界"))
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			for _, msg := range messages {
				if msg.Role == "system" && strings.Contains(msg.Content, "主题提炼") {
					summaryCalls.Add(1)
					return `{"items":[]}`, nil
				}
			}
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}
	paras, err := RunASRParagraphs(context.Background(), llmClient, liveASR, dur, nil)
	if err != nil {
		t.Fatalf("RunASRParagraphs() error = %v", err)
	}
	if summaryCalls.Load() != 0 {
		t.Fatalf("summary LLM calls = %d, want 0", summaryCalls.Load())
	}
	if len(paras) == 0 || paras[0].Text != "你好世界" {
		t.Fatalf("paragraphs = %+v", paras)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("timeline: %v", err)
	}
}

func TestRunASRPostprocess_KeepsInRangeSummary(t *testing.T) {
	const dur = int64(10 * 60 * 1000) // 10 分钟
	liveASR := string(sampleLiveASRJSON(dur, "你好世界"))
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			sys := ""
			for _, msg := range messages {
				if msg.Role == "system" {
					sys = msg.Content
				}
			}
			if strings.Contains(sys, "主题提炼") {
				return `{"items":[{"title":"讲解","start_index":0,"end_index":0}]}`, nil
			}
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}
	out, err := runASRPostprocess(context.Background(), llmClient, liveASR, dur, nil, nil)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if len(out.Summaries) != 1 || out.Summaries[0].Title != "讲解" {
		t.Fatalf("Summaries = %+v", out.Summaries)
	}
	if out.Summaries[0].EndTime-out.Summaries[0].StartTime != dur {
		t.Fatalf("duration = %d, want %d", out.Summaries[0].EndTime-out.Summaries[0].StartTime, dur)
	}
}

func TestRunASRPostprocess_SummariesAndParagraphsRunInParallel(t *testing.T) {
	const dur = int64(10 * 60 * 1000)
	var (
		summaryEntered   atomic.Bool
		paragraphEntered atomic.Bool
		sawOverlap       atomic.Bool
		closeGate        sync.Once
	)
	gate := make(chan struct{})

	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			sys := ""
			for _, msg := range messages {
				if msg.Role == "system" {
					sys = msg.Content
				}
			}
			isSummary := strings.Contains(sys, "主题提炼")
			if isSummary {
				summaryEntered.Store(true)
				if paragraphEntered.Load() {
					sawOverlap.Store(true)
				}
				// 等待 paragraphs 侧也进入，证明两路重叠。
				select {
				case <-gate:
				case <-time.After(2 * time.Second):
					return "", errors.New("timed out waiting for paragraph phase")
				case <-ctx.Done():
					return "", ctx.Err()
				}
				return `{"items":[{"title":"讲解","start_index":0,"end_index":0}]}`, nil
			}
			paragraphEntered.Store(true)
			if summaryEntered.Load() {
				sawOverlap.Store(true)
			}
			closeGate.Do(func() { close(gate) })
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}

	out, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRJSON(dur, "你好世界")), dur, nil, nil)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if !sawOverlap.Load() {
		t.Fatal("summaries and paragraphs did not overlap in time")
	}
	if len(out.Summaries) != 1 || out.Summaries[0].Title != "讲解" {
		t.Fatalf("Summaries = %+v", out.Summaries)
	}
	if len(out.Paragraphs) == 0 || out.Paragraphs[0].Text != "你好世界" {
		t.Fatalf("Paragraphs = %+v", out.Paragraphs)
	}
}

func TestRunASRPostprocess_BadJSONFallsBackWithoutRepair(t *testing.T) {
	var calls atomic.Int32
	const dur = int64(10 * 60 * 1000)
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls.Add(1)
			sys := ""
			for _, msg := range messages {
				if msg.Role == "system" {
					sys = msg.Content
				}
			}
			if strings.Contains(sys, "主题提炼") {
				return "not-json", nil
			}
			return "also-not-json", nil
		},
	}
	out, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRJSON(dur, "你好")), dur, nil, logger)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if len(out.Summaries) != 0 {
		t.Fatalf("summaries = %+v, want empty fallback", out.Summaries)
	}
	if len(out.Paragraphs) == 0 || out.Paragraphs[0].Text != "你好" {
		t.Fatalf("paragraphs = %+v, want local fallback", out.Paragraphs)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("calls = %d, want 2 (no content repair)", n)
	}
	summaryWarns := logs.FilterMessageSnippet("ASR summaries 窗结果不可用").Len()
	paraWarns := logs.FilterMessageSnippet("ASR paragraphs 窗不可用").Len()
	if summaryWarns < 1 || paraWarns < 1 {
		t.Fatalf("fallback warn logs: summary=%d paragraphs=%d, want both >= 1", summaryWarns, paraWarns)
	}
}

func TestRunASRPostprocess_LLMTransportRetriesThenFails(t *testing.T) {
	var calls atomic.Int32
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls.Add(1)
			return "", context.DeadlineExceeded
		},
	}
	_, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRJSON(100, "hi")), 100, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// 两路并行：每路最多 asrLLMTransportMaxAttempts 次；一路失败后另一路可能被取消，故区间为 [N, 2N]。
	n := int(calls.Load())
	if n < asrLLMTransportMaxAttempts || n > 2*asrLLMTransportMaxAttempts {
		t.Fatalf("calls = %d, want between %d and %d", n, asrLLMTransportMaxAttempts, 2*asrLLMTransportMaxAttempts)
	}
}

func TestASRChatStructured_TransportRetryThenSuccess(t *testing.T) {
	calls := 0
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls++
			if calls < 3 {
				return "", fmt.Errorf("请求 LLM 失败: connection reset")
			}
			return `{"items":[]}`, nil
		},
	}
	got, err := asrChatStructured(context.Background(), llmClient, []llm.ChatMessage{{Role: "user", Content: "x"}}, logger)
	if err != nil {
		t.Fatalf("asrChatStructured() error = %v", err)
	}
	if got != `{"items":[]}` {
		t.Fatalf("got = %q", got)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if n := logs.FilterMessageSnippet("准备重试").Len(); n != 2 {
		t.Fatalf("retry warn logs = %d, want 2", n)
	}
}

func TestIsASRLLMTransportError(t *testing.T) {
	if !isASRLLMTransportError(context.DeadlineExceeded) {
		t.Fatal("DeadlineExceeded should be transport")
	}
	if !isASRLLMTransportError(errors.New("LLM HTTP 503: busy")) {
		t.Fatal("HTTP error should be transport")
	}
	if isASRLLMTransportError(errors.New("LLM API Key 未配置")) {
		t.Fatal("API key missing should not retry")
	}
	if isASRLLMTransportError(errors.New("解析 asr_summaries JSON 失败")) {
		t.Fatal("content parse error should not be transport")
	}
}

func TestStitchASRParagraphs_AllowsLongText(t *testing.T) {
	long := strings.Repeat("字", 400)
	utterances := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 1000, Text: long},
	}
	paras, err := stitchASRParagraphs(utterances, []asrParagraphRange{{StartIndex: 0, EndIndex: 0}})
	if err != nil {
		t.Fatalf("stitchASRParagraphs() error = %v", err)
	}
	if utf8.RuneCountInString(paras[0].Text) != 400 {
		t.Fatalf("runes = %d, want 400", utf8.RuneCountInString(paras[0].Text))
	}
}

func TestBuildParagraphRangesLocally_BySpeaker(t *testing.T) {
	uts := []asr.Utterance{
		{Speaker: "1", Text: "a"},
		{Speaker: "1", Text: "b"},
		{Speaker: "2", Text: "c"},
		{Speaker: "1", Text: "d"},
	}
	got := buildParagraphRangesLocally(uts)
	want := []asrParagraphRange{
		{StartIndex: 0, EndIndex: 1},
		{StartIndex: 2, EndIndex: 2},
		{StartIndex: 3, EndIndex: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("got = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNormalizeASRParagraphTimeline(t *testing.T) {
	// 无 words 时不再为消重叠改写时间；有 words 时以字级首尾为准。
	paras := []model.ASRParagraph{
		{
			StartTime: 40, EndTime: 200, Text: "a",
			Words: []model.ClipWord{{Text: "a", StartTime: 40, EndTime: 200}},
		},
		{
			StartTime: 150, EndTime: 300, Text: "b",
			Words: []model.ClipWord{{Text: "b", StartTime: 150, EndTime: 300}},
		},
	}
	normalizeASRParagraphTimeline(paras, 1000)
	if paras[0].StartTime != 40 || paras[0].EndTime != 200 {
		t.Fatalf("para0=%d-%d, want words bounds 40-200", paras[0].StartTime, paras[0].EndTime)
	}
	if paras[1].StartTime != 150 || paras[1].EndTime != 300 {
		t.Fatalf("para1=%d-%d, want words bounds 150-300（允许与上一段时间重叠）", paras[1].StartTime, paras[1].EndTime)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestNormalizeASRParagraphTimeline_PreservesLeadingSilence(t *testing.T) {
	paras := []model.ASRParagraph{
		{StartTime: 2_218_590, EndTime: 2_224_350, Text: "属 AI 股票顾问"},
		{StartTime: 2_241_080, EndTime: 2_246_090, Text: "知道哪个关吗？"},
	}
	normalizeASRParagraphTimeline(paras, 18_231_192)
	if paras[0].StartTime != 2_218_590 {
		t.Fatalf("first start = %d, want 2218590", paras[0].StartTime)
	}
	if paras[1].EndTime != 2_246_090 {
		t.Fatalf("last end = %d, want 2246090", paras[1].EndTime)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestNormalizeASRParagraphTimeline_DerivesSegmentBoundsKeepsWordTimes(t *testing.T) {
	paras := []model.ASRParagraph{
		{
			StartTime: -1,
			EndTime:   -1,
			Text:      "属 AI",
			Words: []model.ClipWord{
				{Text: "属", StartTime: 100, EndTime: 120},
				{Text: " ", StartTime: -1, EndTime: -1},
				{Text: "AI", StartTime: 140, EndTime: 180},
			},
		},
		{
			StartTime: 0,
			EndTime:   0,
			Text:      "你好",
			Words: []model.ClipWord{
				{Text: "你", StartTime: 200, EndTime: 220},
				{Text: "好", StartTime: 220, EndTime: 250},
			},
		},
	}
	finalizeASRParagraphTimeline(paras, 10_000)
	if paras[0].StartTime != 100 || paras[0].EndTime != 180 {
		t.Fatalf("para0 timeline = [%d,%d], want words[0].start=100 words[last].end=180", paras[0].StartTime, paras[0].EndTime)
	}
	if paras[1].StartTime != 200 || paras[1].EndTime != 250 {
		t.Fatalf("para1 timeline = [%d,%d], want 200-250", paras[1].StartTime, paras[1].EndTime)
	}
	// 词级时间与源一致：空格保持 -1，不做 repair。
	space := paras[0].Words[1]
	if space.StartTime != -1 || space.EndTime != -1 {
		t.Fatalf("space word should keep source -1: %+v", space)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestFinalizeASRParagraphTimeline_UndersizedDurationCoversLastWord(t *testing.T) {
	// 复现错误结果.json：duration 偏短时不得把段 end 压到末字之前。
	paras := []model.ASRParagraph{
		{
			Speaker:   "1",
			Text:      "把生命浪费在美好的事物上",
			StartTime: 91850,
			EndTime:   136460,
			Words: []model.ClipWord{
				{Text: "物", StartTime: 136100, EndTime: 136180},
				{Text: "上", StartTime: 136220, EndTime: 136460},
			},
		},
	}
	finalizeASRParagraphTimeline(paras, 135430)
	if paras[0].Words[1].EndTime != 136460 {
		t.Fatalf("words mutated: %+v", paras[0].Words[1])
	}
	if paras[0].StartTime != paras[0].Words[0].StartTime {
		t.Fatalf("start_time=%d != words[0].start=%d", paras[0].StartTime, paras[0].Words[0].StartTime)
	}
	if paras[0].EndTime != 136460 {
		t.Fatalf("end_time=%d, want words[last].end=136460", paras[0].EndTime)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestNormalizeASRParagraphTimeline_NoOverlapWhenPrevZeroEnd(t *testing.T) {
	// 无 words：仅修复零长度区间；不再为消重叠改写后段 start。
	paras := []model.ASRParagraph{
		{StartTime: 0, EndTime: 0, Text: "a"},
		{StartTime: 0, EndTime: 7_065_780, Text: "b"},
	}
	finalizeASRParagraphTimeline(paras, 18_000_000)
	if paras[0].StartTime >= paras[0].EndTime {
		t.Fatalf("para0 still zero-span: %+v", paras[0])
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestStitchASRParagraphs_PreservesSpaceWordTimes(t *testing.T) {
	utterances := []asr.Utterance{
		{
			Speaker: "1", StartTime: 100, EndTime: 200, Text: "属 AI",
			Words: []asr.Word{
				{Text: "属", StartTime: 100, EndTime: 120},
				{Text: " ", StartTime: -1, EndTime: -1},
				{Text: "AI", StartTime: 140, EndTime: 200},
			},
		},
	}
	paras, err := stitchASRParagraphs(utterances, []asrParagraphRange{{StartIndex: 0, EndIndex: 0}})
	if err != nil {
		t.Fatalf("stitch: %v", err)
	}
	finalizeASRParagraphTimeline(paras, 1000)
	if err := validateASRParagraphWordIdentity(utterances, paras); err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if paras[0].StartTime != 100 || paras[0].EndTime != 200 {
		t.Fatalf("timeline = [%d,%d], want [100,200]", paras[0].StartTime, paras[0].EndTime)
	}
	if paras[0].Words[1].StartTime != -1 || paras[0].Words[1].EndTime != -1 {
		t.Fatalf("space word mutated: %+v", paras[0].Words[1])
	}
}

func TestSplitASRParagraphBySentences_IgnoresInvalidLeadingWordTime(t *testing.T) {
	s1 := strings.Repeat("甲", 119) + "。"
	s2 := strings.Repeat("乙", 119) + "。"
	text := s1 + s2
	words := []model.ClipWord{{Text: " ", StartTime: -1, EndTime: -1}}
	t0 := int64(1000)
	for _, r := range []rune(text) {
		words = append(words, model.ClipWord{Text: string(r), StartTime: t0, EndTime: t0 + 5})
		t0 += 5
	}
	// 让 words 文本与 paragraph 对齐：去掉前导空格 word 对应的文本外的错位
	// 实际 ASR 空格在正文中，这里构造「正文含空格」场景：
	textWithSpace := " " + text
	p := model.ASRParagraph{
		Speaker:   "1",
		Text:      textWithSpace,
		StartTime: 1000,
		EndTime:   t0,
		Words:     words,
	}
	got := splitASRParagraphBySentences(p, asrParagraphMaxRunes)
	finalizeASRParagraphTimeline(got, t0+100)
	if err := validateASRParagraphTimeline(got); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got[0].StartTime == 0 && got[0].EndTime == 0 {
		t.Fatalf("first segment collapsed to [0,0]: %+v", got[0])
	}
	if got[0].StartTime < 0 {
		t.Fatalf("first start still negative: %d", got[0].StartTime)
	}
}

func TestEnforceASRParagraphMaxRunes_SplitByPeriod(t *testing.T) {
	// 两句各 120 字，合计 242 > 200，应按句号拆成两段。
	s1 := strings.Repeat("甲", 119) + "。"
	s2 := strings.Repeat("乙", 119) + "。"
	text := s1 + s2
	paras, splitCount := enforceASRParagraphMaxRunes([]model.ASRParagraph{{
		Speaker:   "1",
		Text:      text,
		StartTime: 0,
		EndTime:   1000,
		Words:     clipWordsFromContentText(text, 0, 1000),
	}})
	if splitCount != 1 {
		t.Fatalf("splitCount = %d, want 1", splitCount)
	}
	if len(paras) < 2 {
		t.Fatalf("len = %d, want >= 2", len(paras))
	}
	var joined strings.Builder
	for _, p := range paras {
		if utf8.RuneCountInString(p.Text) > asrParagraphMaxRunes {
			t.Fatalf("segment runes = %d > %d: %q", utf8.RuneCountInString(p.Text), asrParagraphMaxRunes, truncateRunes(p.Text, 32))
		}
		joined.WriteString(p.Text)
	}
	if joined.String() != text {
		t.Fatalf("joined text mismatch")
	}
}

func TestEnforceASRParagraphMaxRunes_HardSplitNoPeriod(t *testing.T) {
	text := strings.Repeat("字", 450)
	paras, splitCount := enforceASRParagraphMaxRunes([]model.ASRParagraph{{
		Speaker:   "1",
		Text:      text,
		StartTime: 0,
		EndTime:   900,
		Words:     clipWordsFromContentText(text, 0, 900),
	}})
	if splitCount != 1 {
		t.Fatalf("splitCount = %d, want 1", splitCount)
	}
	if len(paras) != 3 { // 200+200+50
		t.Fatalf("len = %d, want 3", len(paras))
	}
	var joined strings.Builder
	for _, p := range paras {
		if utf8.RuneCountInString(p.Text) > asrParagraphMaxRunes {
			t.Fatalf("runes = %d", utf8.RuneCountInString(p.Text))
		}
		joined.WriteString(p.Text)
	}
	if joined.String() != text {
		t.Fatal("joined mismatch")
	}
}

func TestSplitASRParagraphBySentences_WordsAlign(t *testing.T) {
	s1 := strings.Repeat("A", 100) + "。"
	s2 := strings.Repeat("B", 100) + "。"
	text := s1 + s2
	// 模拟源 ASR：words 不含句读，仅内容字符。
	var words []model.ClipWord
	t0 := int64(0)
	for _, r := range []rune(stripASRAlignPunct(text)) {
		words = append(words, model.ClipWord{
			Text:      string(r),
			StartTime: t0,
			EndTime:   t0 + 10,
		})
		t0 += 10
	}
	orig := append([]model.ClipWord(nil), words...)
	p := model.ASRParagraph{
		Speaker:   "1",
		Text:      text,
		StartTime: 0,
		EndTime:   t0,
		Words:     words,
	}
	got := splitASRParagraphBySentences(p, asrParagraphMaxRunes)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	finalizeASRParagraphTimeline(got, t0+100)
	flat := flattenParagraphWords(got)
	if len(flat) != len(orig) {
		t.Fatalf("word count %d != %d", len(flat), len(orig))
	}
	for i := range orig {
		if flat[i] != orig[i] {
			t.Fatalf("words[%d] mutated: got=%+v want=%+v", i, flat[i], orig[i])
		}
	}
	var wordText strings.Builder
	for i, seg := range got {
		if utf8.RuneCountInString(seg.Text) > asrParagraphMaxRunes {
			t.Fatalf("seg[%d] too long", i)
		}
		wordText.WriteString(seg.Text)
		if i > 0 && seg.StartTime < got[i-1].EndTime {
			t.Fatalf("time not monotonic: %+v", got)
		}
	}
	if wordText.String() != text {
		t.Fatal("full text mismatch")
	}
	if err := validateASRParagraphTimeline(got); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestBuildParagraphRangesLocally_PacksByMaxRunes(t *testing.T) {
	longA := strings.Repeat("甲", 120)
	longB := strings.Repeat("乙", 120) // 120+120 > 200
	uts := []asr.Utterance{
		{Speaker: "1", Text: longA},
		{Speaker: "1", Text: longB},
	}
	got := buildParagraphRangesLocally(uts)
	if len(got) != 2 || got[0].EndIndex != 0 || got[1].StartIndex != 1 {
		t.Fatalf("got = %+v, want two ranges", got)
	}
}

func TestGenerateASRParagraphs_EnforcesMaxRunes(t *testing.T) {
	s1 := strings.Repeat("开", 110) + "。"
	s2 := strings.Repeat("场", 110) + "。"
	text := s1 + s2
	uts := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 500, Text: text, Words: asrWordsFromContentText(text, 0, 500)},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			sys := ""
			for _, msg := range messages {
				if msg.Role == "system" {
					sys = msg.Content
				}
			}
			if strings.Contains(sys, "主题提炼") {
				return `{"items":[]}`, nil
			}
			// 模型故意返回整段一个区间（超 200）
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}
	paras, err := generateASRParagraphs(context.Background(), llmClient, uts, 1000, nil, nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(paras) < 2 {
		t.Fatalf("paragraphs = %d, want split >= 2", len(paras))
	}
	var joined strings.Builder
	for _, p := range paras {
		if utf8.RuneCountInString(p.Text) > asrParagraphMaxRunes {
			t.Fatalf("paragraph exceeds max: %d", utf8.RuneCountInString(p.Text))
		}
		joined.WriteString(p.Text)
	}
	if joined.String() != text {
		t.Fatal("joined text mismatch")
	}
	if paras[0].StartTime != 0 || paras[len(paras)-1].EndTime != 500 {
		t.Fatalf("timeline = %d-%d, want words bounds 0-500", paras[0].StartTime, paras[len(paras)-1].EndTime)
	}
}

func TestGenerateASRParagraphsWindow_LocalFallbackOnOverlap(t *testing.T) {
	win := utteranceWindow{
		Utterances: []asr.Utterance{
			{Speaker: "1", StartTime: 0, EndTime: 100, Text: "甲"},
			{Speaker: "2", StartTime: 120, EndTime: 200, Text: "乙"},
		},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			// 漏盖：只覆盖一句
			return `{"items":[{"start_index":0,"end_index":0}]}`, nil
		},
	}
	ranges, _, err := generateASRParagraphsWindow(context.Background(), llmClient, win, nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(ranges) != 2 || ranges[0].EndIndex != 0 || ranges[1].StartIndex != 1 {
		t.Fatalf("ranges = %+v, want local by speaker", ranges)
	}
}

func TestGenerateASRParagraphsWindow_LLMTransportFallsBackLocally(t *testing.T) {
	win := utteranceWindow{
		Utterances: []asr.Utterance{
			{Speaker: "1", StartTime: 0, EndTime: 100, Text: "甲"},
			{Speaker: "1", StartTime: 120, EndTime: 200, Text: "乙"},
			{Speaker: "2", StartTime: 300, EndTime: 400, Text: "丙"},
		},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	ranges, _, err := generateASRParagraphsWindow(context.Background(), llmClient, win, nil)
	if err != nil {
		t.Fatalf("error = %v, want local fallback without error", err)
	}
	want := buildParagraphRangesLocally(win.Utterances)
	if len(ranges) != len(want) {
		t.Fatalf("ranges = %+v, want %+v", ranges, want)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("ranges[%d] = %+v, want %+v", i, ranges[i], want[i])
		}
	}
}

func TestGenerateASRParagraphs_LLMTransportFallsBackLocally(t *testing.T) {
	uts := []asr.Utterance{
		{Speaker: "1", StartTime: 0, EndTime: 100, Text: "甲", Words: asrWordsFromContentText("甲", 0, 100)},
		{Speaker: "1", StartTime: 120, EndTime: 200, Text: "乙", Words: asrWordsFromContentText("乙", 120, 200)},
		{Speaker: "2", StartTime: 300, EndTime: 400, Text: "丙", Words: asrWordsFromContentText("丙", 300, 400)},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "", fmt.Errorf("请求 LLM 失败: connection reset")
		},
	}
	paras, err := generateASRParagraphs(context.Background(), llmClient, uts, 1000, nil, nil)
	if err != nil {
		t.Fatalf("generateASRParagraphs() error = %v", err)
	}
	if len(paras) != 2 {
		t.Fatalf("paragraphs = %+v, want 2 (speaker-adjacent merge)", paras)
	}
	if paras[0].Text != "甲乙" || paras[1].Text != "丙" {
		t.Fatalf("texts = %q / %q", paras[0].Text, paras[1].Text)
	}
	if err := validateASRParagraphTimeline(paras); err != nil {
		t.Fatalf("timeline: %v", err)
	}
}

func TestRunASRParagraphs_LLMTransportStillSucceeds(t *testing.T) {
	const dur = int64(10 * 60 * 1000)
	liveASR := string(sampleLiveASRJSON(dur, "你好世界"))
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	paras, err := RunASRParagraphs(context.Background(), llmClient, liveASR, dur, nil)
	if err != nil {
		t.Fatalf("RunASRParagraphs() error = %v", err)
	}
	if len(paras) == 0 || paras[0].Text != "你好世界" {
		t.Fatalf("paragraphs = %+v", paras)
	}
}

func TestLiveMaterialASRWorker_Process_BadLLMJSONSucceedsWithFallback(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			9: {ID: 9, LiveURL: "https://example.com/x.mp4", ASRStatus: model.ASRStatusProcessing},
		},
	}
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			return sampleLiveASRJSON(100, "hi"), nil
		},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "not-json", nil
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/a.mp3", Cleanup: func() {}}, nil
		},
	}, llmClient, nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[9]); err != nil {
		t.Fatalf("Process() error = %v, want success via fallback", err)
	}
	if repo.materials[9].ASRStatus != model.ASRStatusCompleted {
		t.Errorf("ASRStatus = %q, want completed", repo.materials[9].ASRStatus)
	}
}

func TestLiveMaterialASRWorker_Process_LLMTransportFailedMarksFailed(t *testing.T) {
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			9: {ID: 9, LiveURL: "https://example.com/x.mp4", ASRStatus: model.ASRStatusProcessing},
		},
	}
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			return sampleLiveASRJSON(100, "hi"), nil
		},
	}
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "", fmt.Errorf("请求 LLM 失败: timeout")
		},
	}
	worker := NewLiveMaterialASRWorker(repo, asrSvc, &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{AudioURL: "https://bucket.example.com/a.mp3", Cleanup: func() {}}, nil
		},
	}, llmClient, nil, 0, 0, webroot.Config{})

	if err := worker.Process(context.Background(), repo.materials[9]); err == nil {
		t.Fatal("Process() error = nil, want transport failure")
	}
	if repo.materials[9].ASRStatus != model.ASRStatusFailed {
		t.Errorf("ASRStatus = %q, want failed", repo.materials[9].ASRStatus)
	}
}
