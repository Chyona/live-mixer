package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestLocalASRPostprocessFallback_BuildsParagraphs(t *testing.T) {
	liveASR := `{
		"audio_info":{"duration":5000},
		"result":{"utterances":[
			{"start_time":0,"end_time":2000,"text":"你好世界","additions":{"speaker":"1"},"words":[]},
			{"start_time":2100,"end_time":4000,"text":"欢迎收看","additions":{"speaker":"1"},"words":[]}
		]}
	}`
	got := localASRPostprocessFallback(liveASR, 5000)
	if len(got.Paragraphs) != 1 {
		t.Fatalf("expected 1 merged paragraph, got %d", len(got.Paragraphs))
	}
	if len(got.Summaries) != 0 {
		t.Fatalf("summaries = %d, want 0", len(got.Summaries))
	}
	joined := got.Paragraphs[0].Text
	if !strings.Contains(joined, "你好") || !strings.Contains(joined, "欢迎") {
		t.Fatalf("paragraph text = %q", joined)
	}
}

func TestLocalASRPostprocessFallback_EmptyASR(t *testing.T) {
	got := localASRPostprocessFallback("{}", 0)
	if len(got.Paragraphs) != 0 || len(got.Summaries) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}

// TestFinishASRPostprocess_StillFullRebuild 关播收尾仍全量重算 paragraphs（+ summaries LLM），不依赖跟播中途结果。
func TestFinishASRPostprocess_StillFullRebuild(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	liveASR := `{
		"result":{"utterances":[
			{"start_time":0,"end_time":100,"text":"甲","additions":{"speaker":"1"},
				"words":[{"text":"甲","start_time":0,"end_time":100}]},
			{"start_time":120,"end_time":200,"text":"乙","additions":{"speaker":"1"},
				"words":[{"text":"乙","start_time":120,"end_time":200}]}
		]}
	}`
	// 中途已有「错误/过时」段落，关播应覆盖为正确合并结果。
	mat := &model.LiveMaterial{
		Name:          "finalize-rebuild",
		LiveURL:       "https://example.com/x.mp4",
		RecordUUID:    "fin1",
		M3U8URL:       "https://example.com/x.m3u8",
		SourceMode:    model.SourceModeLive,
		LiveStatus:    model.LiveStatusEnded,
		IngestEpoch:   5,
		LiveASR:       liveASR,
		ASRStatus:     model.ASRStatusProcessing,
		ASRParagraphs: []model.ASRParagraph{{Speaker: "1", Text: "过时段", StartTime: 0, EndTime: 1}},
		Duration:      200,
		CreatedBy:     1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	llmClient := &workerMockLLM{
		chatFn: func(ctx context.Context, messages []llm.ChatMessage) (string, error) {
			return `{"items":[]}`, nil
		},
	}
	w := &liveIngestWorker{repo: repo, llmClient: llmClient, logger: zap.NewNop()}
	if err := w.finishASRPostprocess(context.Background(), mat, 200); err != nil {
		t.Fatalf("finishASRPostprocess: %v", err)
	}
	got, err := repo.GetByID(context.Background(), mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRStatus != model.ASRStatusCompleted {
		t.Fatalf("asr_status=%q", got.ASRStatus)
	}
	if len(got.ASRParagraphs) != 1 || got.ASRParagraphs[0].Text != "甲乙" {
		t.Fatalf("final ASRParagraphs=%+v, want merged 甲乙", got.ASRParagraphs)
	}
	if got.ASRSummaries == nil {
		t.Fatal("ASRSummaries should be non-nil empty after LLM")
	}
}

func TestLiveMediaWindowDuration(t *testing.T) {
	if model.LiveMediaWindowDuration != 10*time.Minute {
		t.Fatalf("LiveMediaWindowDuration = %v, want 10m", model.LiveMediaWindowDuration)
	}
	if model.MaxASRTranscribeDuration != 2*time.Minute {
		t.Fatalf("MaxASRTranscribeDuration = %v, want 2m (短 chunk，避免整窗缩放)", model.MaxASRTranscribeDuration)
	}
	if model.MaxASRTranscribeDuration >= model.LiveMediaWindowDuration {
		t.Fatalf("ASR chunk (%v) must be shorter than media window (%v)", model.MaxASRTranscribeDuration, model.LiveMediaWindowDuration)
	}
}
