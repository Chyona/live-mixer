package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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

func TestValidateASRSummaries_ShortDuration(t *testing.T) {
	err := validateASRSummaries([]model.ASRSummarySegment{
		{Title: "开场", Summary: "简短内容", StartTime: 0, EndTime: 1000},
	}, 1000)
	if err != nil {
		t.Fatalf("validateASRSummaries() error = %v", err)
	}
}

func TestValidateASRSummaries_TitleTooLong(t *testing.T) {
	err := validateASRSummaries([]model.ASRSummarySegment{
		{Title: "一二三四五六七", Summary: "摘要", StartTime: 0, EndTime: 300000},
	}, 600000)
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("error = %v, want title too long", err)
	}
}

func TestParseASRSummaries_FromMarkdown(t *testing.T) {
	segs, err := parseASRSummaries("```json\n[{\"title\":\"主题\",\"summary\":\"核心观点提炼\",\"start_time\":0,\"end_time\":300000}]\n```")
	if err != nil {
		t.Fatalf("parseASRSummaries() error = %v", err)
	}
	if len(segs) != 1 || segs[0].Title != "主题" {
		t.Fatalf("segs = %+v", segs)
	}
}

func TestRunASRPostprocess_Success(t *testing.T) {
	liveASR := string(sampleLiveASRJSON(1200, "你好世界"))
	out, err := runASRPostprocess(context.Background(), defaultWorkerLLM(), liveASR, 1200, nil)
	if err != nil {
		t.Fatalf("runASRPostprocess() error = %v", err)
	}
	if len(out.Summaries) == 0 || len(out.Paragraphs) == 0 {
		t.Fatalf("empty result: %+v", out)
	}
	if out.Paragraphs[0].Text != "你好世界" {
		t.Errorf("paragraph text = %q", out.Paragraphs[0].Text)
	}
}

func TestRunASRPostprocess_LLMErrorFails(t *testing.T) {
	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	_, err := runASRPostprocess(context.Background(), llmClient, string(sampleLiveASRJSON(100, "hi")), 100, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLiveMaterialASRWorker_Process_LLMFailedMarksFailed(t *testing.T) {
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

	if err := worker.Process(context.Background(), repo.materials[9]); err == nil {
		t.Fatal("Process() error = nil, want LLM failure")
	}
	if repo.materials[9].ASRStatus != model.ASRStatusFailed {
		t.Errorf("ASRStatus = %q, want failed", repo.materials[9].ASRStatus)
	}
	if repo.materials[9].ASRErrorMsg == "" {
		t.Error("ASRErrorMsg should not be empty")
	}
}
