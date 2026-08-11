package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"

	"go.uber.org/zap"
)

func TestSortAndMergeOverlappingClipRanges(t *testing.T) {
	tests := []struct {
		name string
		in   []model.ClipRange
		want []model.ClipRange
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: []model.ClipRange{}, want: nil},
		{
			name: "single",
			in:   []model.ClipRange{{StartTime: 10, EndTime: 20}},
			want: []model.ClipRange{{StartTime: 10, EndTime: 20}},
		},
		{
			name: "sort_only",
			in: []model.ClipRange{
				{StartTime: 5000, EndTime: 6000},
				{StartTime: 1000, EndTime: 2000},
			},
			want: []model.ClipRange{
				{StartTime: 1000, EndTime: 2000},
				{StartTime: 5000, EndTime: 6000},
			},
		},
		{
			name: "overlap_merge",
			in: []model.ClipRange{
				{StartTime: 5000, EndTime: 8000},
				{StartTime: 1000, EndTime: 3000},
				{StartTime: 2500, EndTime: 6000},
			},
			want: []model.ClipRange{{StartTime: 1000, EndTime: 8000}},
		},
		{
			name: "abut_merge",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 100},
				{StartTime: 100, EndTime: 200},
			},
			want: []model.ClipRange{{StartTime: 0, EndTime: 200}},
		},
		{
			name: "gap_keep",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 100},
				{StartTime: 101, EndTime: 200},
			},
			want: []model.ClipRange{
				{StartTime: 0, EndTime: 100},
				{StartTime: 101, EndTime: 200},
			},
		},
		{
			name: "contained",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 100, EndTime: 200},
			},
			want: []model.ClipRange{{StartTime: 0, EndTime: 1000}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortAndMergeOverlappingClipRanges(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSortAndMergeOverlappingClipRanges_DoesNotMutateInput(t *testing.T) {
	in := []model.ClipRange{
		{StartTime: 5000, EndTime: 8000},
		{StartTime: 1000, EndTime: 3000},
	}
	_ = sortAndMergeOverlappingClipRanges(in)
	if in[0].StartTime != 5000 || in[1].StartTime != 1000 {
		t.Fatalf("input mutated: %#v", in)
	}
}

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

func TestBuildClips1FromIndices_MergeAdjacent(t *testing.T) {
	segments := []asr.Utterance{
		{
			StartTime: 0, EndTime: 1000, Text: "第一段",
			Words: []asr.Word{{StartTime: 0, EndTime: 1000, Text: "第一段"}},
		},
		{
			StartTime: 1400, EndTime: 2000, Text: "第二段",
			Words: []asr.Word{{StartTime: 1400, EndTime: 2000, Text: "第二段"}},
		},
		{StartTime: 3000, EndTime: 4000, Text: "远隔段"},
	}
	// gap=400ms ≤ 500 → 前两段合并；第三段保留。
	clips := buildClips1FromIndices(segments, []int{0, 1, 2})
	if len(clips) != 2 {
		t.Fatalf("len = %d, want 2, clips=%#v", len(clips), clips)
	}
	if clips[0].Text != "第一段第二段" || clips[0].StartTime != 0 || clips[0].EndTime != 2000 {
		t.Errorf("merged = %#v", clips[0])
	}
	if len(clips[0].Words) != 2 {
		t.Errorf("merged words len = %d, want 2", len(clips[0].Words))
	}
	if clips[1].Text != "远隔段" {
		t.Errorf("clips[1] = %#v", clips[1])
	}
}

func TestMergeAdjacentClips1_GapExactly500(t *testing.T) {
	in := []model.ClipWithText{
		{Text: "a", StartTime: 0, EndTime: 1000},
		{Text: "b", StartTime: 1500, EndTime: 2000},
	}
	got := mergeAdjacentClips1(in, prepare.ClipMergeGapMS)
	if len(got) != 1 || got[0].Text != "ab" || got[0].StartTime != 0 || got[0].EndTime != 2000 {
		t.Fatalf("got = %#v", got)
	}
}

func TestMergeAdjacentClips1_Gap501KeepsTwo(t *testing.T) {
	in := []model.ClipWithText{
		{Text: "a", StartTime: 0, EndTime: 1000},
		{Text: "b", StartTime: 1501, EndTime: 2000},
	}
	got := mergeAdjacentClips1(in, prepare.ClipMergeGapMS)
	if len(got) != 2 {
		t.Fatalf("got = %#v, want 2", got)
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

func TestWriteAISliceClips0Debug(t *testing.T) {
	dir := t.TempDir()
	clips := []model.ClipRange{{StartTime: 1, EndTime: 2}, {StartTime: 10, EndTime: 20}}
	writeAISliceClips0Debug(dir, "clips0_before.json", "task-1", 9, "before", clips, zap.NewNop())

	raw, err := os.ReadFile(filepath.Join(dir, "clips0_before.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got aiSliceClips0DebugRecord
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Phase != "before" || got.TaskID != "task-1" || got.VideoProjectID != 9 || got.Count != 2 {
		t.Fatalf("record = %#v", got)
	}
	if !reflect.DeepEqual(got.Clips, clips) {
		t.Fatalf("clips = %#v", got.Clips)
	}
}

func TestWriteAISliceClips0Debug_EmptyDirNoop(t *testing.T) {
	// 不应 panic。
	writeAISliceClips0Debug("", "x.json", "t", 1, "before", nil, zap.NewNop())
}
