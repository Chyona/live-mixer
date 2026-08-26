package service

import (
	"reflect"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

func TestAutoSliceProjectNames(t *testing.T) {
	now := time.Date(2026, 8, 25, 22, 13, 0, 0, time.Local)
	got1 := autoSliceProjectNames("AI选片", 1, now)
	want1 := []string{"AI选片_2026-08-25_22:13:00"}
	if !reflect.DeepEqual(got1, want1) {
		t.Fatalf("n=1 got %#v want %#v", got1, want1)
	}
	got2 := autoSliceProjectNames("一键成片", 2, now)
	want2 := []string{"一键成片_2026-08-25_22:13:00_1", "一键成片_2026-08-25_22:13:00_2"}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("n=2 got %#v want %#v", got2, want2)
	}
}

func TestSplitClips0IntoProjects_WithinMaxKeepsOne(t *testing.T) {
	merged := []model.ClipRange{{StartTime: 0, EndTime: 20 * 60 * 1000}}
	got := splitClips0IntoProjects(merged, nil, nil)
	if len(got) != 1 {
		t.Fatalf("groups = %d, want 1", len(got))
	}
	if clipRangesDurationMS(got[0]) != 20*60*1000 {
		t.Fatalf("duration = %d", clipRangesDurationMS(got[0]))
	}
}

func TestSplitClips0IntoProjects_PreservesSixtyMinutes(t *testing.T) {
	const hourMS int64 = 60 * 60 * 1000
	const step int64 = 10 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: hourMS}}
	utterances := makeUtterances(0, hourMS, step)
	paragraphs := []model.ASRParagraph{
		{StartTime: 0, EndTime: 25 * 60 * 1000},
		{StartTime: 25 * 60 * 1000, EndTime: 50 * 60 * 1000},
		{StartTime: 50 * 60 * 1000, EndTime: hourMS},
	}

	got := splitClips0IntoProjects(merged, utterances, paragraphs)
	assertSplitCoversMerged(t, merged, got)

	if len(got) < 2 {
		t.Fatalf("expected multiple projects for 60min, got %d", len(got))
	}
	for i, g := range got {
		d := clipRangesDurationMS(g)
		if d > aiSliceProjectMaxDurationMS {
			t.Errorf("group[%d] duration %d exceeds 30min", i, d)
		}
		if d < aiSliceProjectMinDurationMS {
			t.Errorf("group[%d] duration %d below 5min", i, d)
		}
	}
}

func TestSplitClips0IntoProjects_NoDroppedGap(t *testing.T) {
	// 用户选 [0,60] 分钟：拆分后不得出现 [0,30]+[40,60] 这种中间丢 10 分钟。
	const hourMS int64 = 60 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: hourMS}}
	utterances := makeUtterances(0, hourMS, 60*1000)
	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
}

func TestSplitClips0IntoProjects_MergesOverlapThenSplits(t *testing.T) {
	in := []model.ClipRange{
		{StartTime: 10 * 60 * 1000, EndTime: 40 * 60 * 1000},
		{StartTime: 0, EndTime: 20 * 60 * 1000},
		{StartTime: 35 * 60 * 1000, EndTime: 50 * 60 * 1000},
	}
	merged := sortAndMergeOverlappingClipRanges(in)
	if len(merged) != 1 || merged[0].StartTime != 0 || merged[0].EndTime != 50*60*1000 {
		t.Fatalf("merged = %#v", merged)
	}
	utterances := makeUtterances(0, 50*60*1000, 30*1000)
	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
}

func TestSplitClips0IntoProjects_DoesNotSplitLongUtterance(t *testing.T) {
	const forty int64 = 40 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: forty}}
	utterances := []asr.Utterance{{StartTime: 0, EndTime: forty, Text: "超长一句"}}
	got := splitClips0IntoProjects(merged, utterances, nil)
	if len(got) != 1 {
		t.Fatalf("groups = %d, want 1 (cannot split a sentence)", len(got))
	}
	if clipRangesDurationMS(got[0]) != forty {
		t.Fatalf("duration = %d, want %d", clipRangesDurationMS(got[0]), forty)
	}
}

func TestSplitClips0IntoProjects_ScatteredClipsUnderMax(t *testing.T) {
	merged := []model.ClipRange{
		{StartTime: 0, EndTime: 10 * 60 * 1000},
		{StartTime: 40 * 60 * 1000, EndTime: 55 * 60 * 1000},
	}
	got := splitClips0IntoProjects(merged, nil, nil)
	if len(got) != 1 {
		t.Fatalf("groups = %d, want 1", len(got))
	}
	if clipRangesDurationMS(got[0]) != 25*60*1000 {
		t.Fatalf("duration = %d", clipRangesDurationMS(got[0]))
	}
}

func TestSplitClips0IntoProjects_ScatteredManySegmentsOver30Min(t *testing.T) {
	// 模拟时间轴上分散选中多个 AI 总结片段（总时长 >30 分钟）。
	const minMS int64 = 5 * 60 * 1000
	var merged []model.ClipRange
	var cursor int64
	for cursor < 90*60*1000 {
		end := cursor + minMS
		merged = append(merged, model.ClipRange{StartTime: cursor, EndTime: end})
		cursor += 10 * 60 * 1000 // 每段 5 分钟，间隔 5 分钟
	}
	if clipRangesDurationMS(merged) <= aiSliceProjectMaxDurationMS {
		t.Fatalf("fixture total = %d, want > 30min", clipRangesDurationMS(merged))
	}

	utterances := makeUtterances(0, 90*60*1000, 30*1000)
	paragraphs := []model.ASRParagraph{
		{StartTime: 0, EndTime: 30 * 60 * 1000},
		{StartTime: 30 * 60 * 1000, EndTime: 60 * 60 * 1000},
		{StartTime: 60 * 60 * 1000, EndTime: 90 * 60 * 1000},
	}
	got := splitClips0IntoProjects(merged, utterances, paragraphs)
	assertSplitCoversMerged(t, merged, got)
	if len(got) < 2 {
		t.Fatalf("expected multiple projects, got %d", len(got))
	}
	for i, g := range got {
		d := clipRangesDurationMS(g)
		if d > aiSliceProjectMaxDurationMS {
			t.Errorf("group[%d] duration %d exceeds 30min", i, d)
		}
		if d < aiSliceProjectMinDurationMS {
			t.Errorf("group[%d] duration %d below 5min", i, d)
		}
		for j := 1; j < len(g); j++ {
			if g[j-1].EndTime > g[j].StartTime {
				t.Errorf("group[%d] clip[%d] overlaps next: %#v vs %#v", i, j-1, g[j-1], g[j])
			}
		}
	}
}

func TestSplitClips0IntoProjects_RebalanceShortTail(t *testing.T) {
	// 32 分钟：先切 30，余 2；再把尾部补到 ≥5，且总和仍为 32。
	const total int64 = 32 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := makeUtterances(0, total, 60*1000)
	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	if len(got) != 2 {
		t.Fatalf("groups = %d, want 2", len(got))
	}
	for i, g := range got {
		d := clipRangesDurationMS(g)
		if d < aiSliceProjectMinDurationMS {
			t.Errorf("group[%d] duration %d below 5min", i, d)
		}
		if d > aiSliceProjectMaxDurationMS {
			t.Errorf("group[%d] duration %d exceeds 30min", i, d)
		}
	}
}

func makeUtterances(start, end, step int64) []asr.Utterance {
	if step <= 0 {
		step = 1000
	}
	out := make([]asr.Utterance, 0, (end-start)/step+1)
	for t := start; t < end; t += step {
		uEnd := t + step
		if uEnd > end {
			uEnd = end
		}
		out = append(out, asr.Utterance{StartTime: t, EndTime: uEnd, Text: "x"})
	}
	return out
}

func assertSplitCoversMerged(t *testing.T, merged []model.ClipRange, groups [][]model.ClipRange) {
	t.Helper()
	want := clipRangesDurationMS(merged)
	var got int64
	var flat []model.ClipRange
	for i, g := range groups {
		d := clipRangesDurationMS(g)
		if d <= 0 {
			t.Fatalf("group[%d] empty", i)
		}
		got += d
		flat = append(flat, g...)
	}
	if got != want {
		t.Fatalf("sum duration = %d, want %d (dropped or duplicated selection)", got, want)
	}
	covered := sortAndMergeOverlappingClipRanges(flat)
	if !reflect.DeepEqual(covered, merged) {
		t.Fatalf("covered %#v != merged %#v (gap or extra)", covered, merged)
	}
}
