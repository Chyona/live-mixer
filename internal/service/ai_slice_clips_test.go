package service

import (
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

func TestFilterUtterancesByClips0(t *testing.T) {
	utterances := []asr.Utterance{
		{StartTime: 0, EndTime: 1000, Text: "开场"},
		{StartTime: 5000, EndTime: 6000, Text: "中间"},
		{StartTime: 9000, EndTime: 10000, Text: "结尾"},
	}
	clips0 := []model.ClipRange{
		{StartTime: 4500, EndTime: 6500},
	}
	got := filterUtterancesByClips0(utterances, clips0)
	if len(got) != 1 || got[0].Text != "中间" {
		t.Fatalf("got = %#v, want 中间", got)
	}
}

func TestFilterUtterancesByClips0_MultipleRangesAndDedupe(t *testing.T) {
	utterances := []asr.Utterance{
		{StartTime: 100, EndTime: 200, Text: "A"},
		{StartTime: 300, EndTime: 400, Text: "B"},
		{StartTime: 500, EndTime: 600, Text: "C"},
	}
	// 两个区间都覆盖 B，结果仍只保留一次。
	clips0 := []model.ClipRange{
		{StartTime: 0, EndTime: 350},
		{StartTime: 280, EndTime: 450},
	}
	got := filterUtterancesByClips0(utterances, clips0)
	if len(got) != 2 || got[0].Text != "A" || got[1].Text != "B" {
		t.Fatalf("got = %#v", got)
	}
}

func TestFilterUtterancesByClips0_Empty(t *testing.T) {
	if got := filterUtterancesByClips0(nil, []model.ClipRange{{StartTime: 0, EndTime: 1}}); got != nil {
		t.Errorf("nil utterances => %#v", got)
	}
	if got := filterUtterancesByClips0([]asr.Utterance{{Text: "x"}}, nil); got != nil {
		t.Errorf("nil clips0 => %#v", got)
	}
}

func TestBuildClips1FromIndices(t *testing.T) {
	segments := []asr.Utterance{
		{
			StartTime: 0, EndTime: 1000, Text: "你好世界",
			Words: []asr.Word{
				{StartTime: 0, EndTime: 400, Text: "你好"},
				{StartTime: 400, EndTime: 1000, Text: "世界"},
			},
		},
		{StartTime: 2000, EndTime: 3000, Text: "继续"},
		{StartTime: 4000, EndTime: 5000, Text: "结束"},
	}

	clips := buildClips1FromIndices(segments, []int{0, 2})
	if len(clips) != 2 {
		t.Fatalf("len = %d, want 2", len(clips))
	}
	if clips[0].Text != "你好世界" || clips[0].StartTime != 0 || clips[0].EndTime != 1000 {
		t.Errorf("clips[0] = %#v", clips[0])
	}
	if len(clips[0].Words) != 2 {
		t.Errorf("words len = %d, want 2", len(clips[0].Words))
	}
	if clips[1].Text != "结束" {
		t.Errorf("clips[1].Text = %q", clips[1].Text)
	}
}

func TestBuildClips1FromIndices_SkipOutOfRange(t *testing.T) {
	segments := []asr.Utterance{
		{StartTime: 0, EndTime: 100, Text: "仅有"},
	}
	// -1、1、99 均越界，只保留下标 0。
	clips := buildClips1FromIndices(segments, []int{-1, 0, 1, 99})
	if len(clips) != 1 || clips[0].Text != "仅有" {
		t.Fatalf("clips = %#v", clips)
	}
}

func TestBuildClips1FromIndices_AllOutOfRange(t *testing.T) {
	segments := []asr.Utterance{{Text: "a"}}
	clips := buildClips1FromIndices(segments, []int{5, 6})
	if len(clips) != 0 {
		t.Fatalf("clips = %#v, want empty", clips)
	}
}

func TestFormatASRSegmentLine(t *testing.T) {
	line := formatASRSegmentLine(0, asr.Utterance{
		StartTime: 4919050,
		EndTime:   4927810,
		Text:      "好，我里面给你们去搭个这个嗯蕾丝美学的米色",
	})
	want := "[0] (4919.05 - 4927.81) 好，我里面给你们去搭个这个嗯蕾丝美学的米色"
	if line != want {
		t.Errorf("line = %q\nwant = %q", line, want)
	}
}
