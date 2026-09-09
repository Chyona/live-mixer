package service

import (
	"strings"
	"testing"
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
