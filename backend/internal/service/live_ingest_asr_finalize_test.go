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
