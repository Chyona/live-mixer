package service

import (
	"strings"
	"testing"
	"time"

	"live-mixer/internal/model"
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
	if len(got.Paragraphs) == 0 {
		t.Fatal("expected local paragraphs")
	}
	if len(got.Summaries) != 0 {
		t.Fatalf("summaries = %d, want 0", len(got.Summaries))
	}
	joined := got.Paragraphs[0].Text
	if !strings.Contains(joined, "你好") {
		t.Fatalf("paragraph text = %q", joined)
	}
}

func TestLocalASRPostprocessFallback_EmptyASR(t *testing.T) {
	got := localASRPostprocessFallback("{}", 0)
	if len(got.Paragraphs) != 0 || len(got.Summaries) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}

func TestMaxWindowASRSegments(t *testing.T) {
	// 单次转写上限 2 分钟 / 6 秒分片 = 20，再 +1 重叠余量。
	got := maxWindowASRSegments()
	want := int(model.MaxASRTranscribeDuration/time.Second)/model.LiveSegmentDurationSec + 1
	if got != want {
		t.Fatalf("maxWindowASRSegments() = %d, want %d", got, want)
	}
	if got > 40 || got < 10 {
		t.Fatalf("maxWindowASRSegments() = %d, out of expected short-chunk range", got)
	}
}
