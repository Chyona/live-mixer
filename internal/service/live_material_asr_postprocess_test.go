package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/webroot"
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
	got := filterASRSummariesByDuration(segs)
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
	})
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
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
	out, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRMultiUtteranceJSON(uts)), dur, nil)
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
		{Speaker: "1", StartTime: 0, EndTime: 1000, Text: "甲"},
		{Speaker: "1", StartTime: asrParagraphWindowMs + 1, EndTime: asrParagraphWindowMs + 2000, Text: "乙"},
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
	paras, err := generateASRParagraphs(context.Background(), llmClient, uts, dur, nil)
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
		t.Fatalf("timeline = %d-%d, want 0-%d", paras[0].StartTime, paras[len(paras)-1].EndTime, dur)
	}
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
	out, err := runASRPostprocess(context.Background(), defaultWorkerLLM(), liveASR, 1200, nil)
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
	out, err := runASRPostprocess(context.Background(), llmClient, liveASR, dur, nil)
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

func TestRunASRPostprocess_BadJSONFallsBackWithoutRepair(t *testing.T) {
	calls := 0
	const dur = int64(10 * 60 * 1000)
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls++
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
	out, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRJSON(dur, "你好")), dur, nil)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if len(out.Summaries) != 0 {
		t.Fatalf("summaries = %+v, want empty fallback", out.Summaries)
	}
	if len(out.Paragraphs) == 0 || out.Paragraphs[0].Text != "你好" {
		t.Fatalf("paragraphs = %+v, want local fallback", out.Paragraphs)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (no content repair)", calls)
	}
}

func TestRunASRPostprocess_LLMTransportRetriesThenFails(t *testing.T) {
	calls := 0
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls++
			return "", context.DeadlineExceeded
		},
	}
	_, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRJSON(100, "hi")), 100, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != asrLLMTransportMaxAttempts {
		t.Fatalf("calls = %d, want %d transport retries", calls, asrLLMTransportMaxAttempts)
	}
}

func TestASRChatStructured_TransportRetryThenSuccess(t *testing.T) {
	calls := 0
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			calls++
			if calls < 3 {
				return "", fmt.Errorf("请求 LLM 失败: connection reset")
			}
			return `{"items":[]}`, nil
		},
	}
	got, err := asrChatStructured(context.Background(), llmClient, []llm.ChatMessage{{Role: "user", Content: "x"}})
	if err != nil {
		t.Fatalf("asrChatStructured() error = %v", err)
	}
	if got != `{"items":[]}` {
		t.Fatalf("got = %q", got)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
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
	paras := []model.ASRParagraph{
		{StartTime: 40, EndTime: 200, Text: "a"},
		{StartTime: 150, EndTime: 300, Text: "b"}, // overlap
	}
	normalizeASRParagraphTimeline(paras, 1000)
	if paras[0].StartTime != 0 {
		t.Fatalf("first start = %d, want 0", paras[0].StartTime)
	}
	if paras[1].StartTime < paras[0].EndTime {
		t.Fatalf("overlap remains: %+v", paras)
	}
	if paras[1].EndTime != 1000 {
		t.Fatalf("last end = %d, want 1000", paras[1].EndTime)
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
	ranges, _, err := generateASRParagraphsWindow(context.Background(), llmClient, win)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(ranges) != 2 || ranges[0].EndIndex != 0 || ranges[1].StartIndex != 1 {
		t.Fatalf("ranges = %+v, want local by speaker", ranges)
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
