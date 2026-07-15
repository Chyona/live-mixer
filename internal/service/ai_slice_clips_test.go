package service

import (
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

func TestBuildClips1(t *testing.T) {
	utterances := []asr.Utterance{
		{
			StartTime: 0,
			EndTime:   1000,
			Text:      "你好世界",
			Words: []asr.Word{
				{StartTime: 0, EndTime: 400, Text: "你好"},
				{StartTime: 400, EndTime: 1000, Text: "世界"},
			},
		},
		{
			StartTime: 2000,
			EndTime:   3000,
			Text:      "继续",
			Words: []asr.Word{
				{StartTime: 2000, EndTime: 3000, Text: "继续"},
			},
		},
	}
	ranges := []model.ClipRange{{StartTime: 0, EndTime: 1000}}
	clips := buildClips1(utterances, ranges)
	if len(clips) != 1 {
		t.Fatalf("len = %d, want 1", len(clips))
	}
	if clips[0].Text != "你好世界" {
		t.Errorf("Text = %q, want 你好世界", clips[0].Text)
	}
	if len(clips[0].Words) != 2 {
		t.Errorf("words len = %d, want 2", len(clips[0].Words))
	}

	json0, err := marshalClips0JSON(ranges)
	if err != nil || json0 == "" {
		t.Fatalf("marshalClips0JSON = %q, err=%v", json0, err)
	}
	json1, err := marshalClips1JSON(clips)
	if err != nil || json1 == "" {
		t.Fatalf("marshalClips1JSON = %q, err=%v", json1, err)
	}
}

func TestBuildClips1_FallbackUtteranceText(t *testing.T) {
	utterances := []asr.Utterance{
		{StartTime: 100, EndTime: 500, Text: "只有分句没有词"},
	}
	clips := buildClips1(utterances, []model.ClipRange{{StartTime: 0, EndTime: 600}})
	if len(clips) != 1 || clips[0].Text != "只有分句没有词" {
		t.Fatalf("clips = %#v", clips)
	}
}
