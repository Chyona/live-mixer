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

func TestTargetProjectCount(t *testing.T) {
	const min = int64(60 * 1000)
	tests := []struct {
		name   string
		wallMS int64
		asrMS  int64
		want   int
	}{
		{name: "20min dense", wallMS: 20 * min, asrMS: 20 * min, want: 1},
		{name: "30min dense", wallMS: 30 * min, asrMS: 30 * min, want: 1},
		{name: "32min dense needs 2 for ASR cap", wallMS: 32 * min, asrMS: 32 * min, want: 2},
		{name: "45min dense", wallMS: 45 * min, asrMS: 45 * min, want: 2},
		{name: "60min dense", wallMS: 60 * min, asrMS: 60 * min, want: 2},
		{name: "90min dense", wallMS: 90 * min, asrMS: 90 * min, want: 3},
		{name: "120min dense", wallMS: 120 * min, asrMS: 120 * min, want: 4},
		{name: "60min sparse 12min ASR stays 1", wallMS: 60 * min, asrMS: 12 * min, want: 1},
		{name: "90min sparse 24min ASR at most 2", wallMS: 90 * min, asrMS: 24 * min, want: 2},
		{name: "120min half speech", wallMS: 120 * min, asrMS: 60 * min, want: 4},
		{name: "all silence", wallMS: 60 * min, asrMS: 0, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := targetProjectCount(tt.wallMS, tt.asrMS)
			if got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
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
	assertDenseProjectCount(t, hourMS, len(got))
	assertGroupsASR(t, got, utterances)
}

func TestSplitClips0IntoProjects_NoDroppedGap(t *testing.T) {
	// 用户选 [0,60] 分钟：拆分后不得出现 [0,30]+[40,60] 这种中间丢 10 分钟。
	const hourMS int64 = 60 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: hourMS}}
	utterances := makeUtterances(0, hourMS, 60*1000)
	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	assertDenseProjectCount(t, hourMS, len(got))
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
	assertDenseProjectCount(t, clipRangesDurationMS(merged), len(got))
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
	wallMS := clipRangesDurationMS(merged)
	if wallMS <= aiSliceProjectMaxASRMS {
		t.Fatalf("fixture total = %d, want > 30min", wallMS)
	}

	utterances := makeUtterances(0, 90*60*1000, 30*1000)
	paragraphs := []model.ASRParagraph{
		{StartTime: 0, EndTime: 30 * 60 * 1000},
		{StartTime: 30 * 60 * 1000, EndTime: 60 * 60 * 1000},
		{StartTime: 60 * 60 * 1000, EndTime: 90 * 60 * 1000},
	}
	got := splitClips0IntoProjects(merged, utterances, paragraphs)
	assertSplitCoversMerged(t, merged, got)
	assertDenseProjectCount(t, wallMS, len(got))
	assertGroupsASR(t, got, utterances)
	for i, g := range got {
		for j := 1; j < len(g); j++ {
			if g[j-1].EndTime > g[j].StartTime {
				t.Errorf("group[%d] clip[%d] overlaps next: %#v vs %#v", i, j-1, g[j-1], g[j])
			}
		}
	}
}

func TestSplitClips0IntoProjects_RebalanceShortTail(t *testing.T) {
	// 32 分钟满口播：ASR 上限要求拆 2 组，且每组有效 ASR ≥10 分钟。
	const total int64 = 32 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := makeUtterances(0, total, 60*1000)
	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	if len(got) != 2 {
		t.Fatalf("groups = %d, want 2", len(got))
	}
	assertGroupsASR(t, got, utterances)
}

func TestSplitClips0IntoProjects_SilenceAllowsClips0Over30Min(t *testing.T) {
	// 60 分钟选区仅 12 分钟 ASR：有效内容不足拆两段，应保持 1 个项目，clips0 墙钟 >30 分钟。
	const total int64 = 60 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := makeDutyCycleUtterances(0, total, 60*1000, 4*60*1000)
	asrMS := clipRangesASRDurationMS(merged, utterances)
	if asrMS != 12*60*1000 {
		t.Fatalf("fixture ASR = %d, want 12min", asrMS)
	}

	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	if len(got) != 1 {
		t.Fatalf("groups = %d, want 1 (ASR too short to split)", len(got))
	}
	if clipRangesDurationMS(got[0]) <= aiSliceProjectMaxASRMS {
		t.Fatalf("clips0 duration = %d, want > 30min when mostly silence", clipRangesDurationMS(got[0]))
	}
	if clipRangesASRDurationMS(got[0], utterances) != asrMS {
		t.Fatalf("group ASR = %d, want %d", clipRangesASRDurationMS(got[0], utterances), asrMS)
	}
}

func TestSplitClips0IntoProjects_SparseSpeechFewerThanWallClock(t *testing.T) {
	// 90 分钟选区约 24 分钟 ASR：N/30=3，但每组须 >10 分钟 ASR，最多 2 个项目。
	const total int64 = 90 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := makeDutyCycleUtterances(0, total, 80*1000, 220*1000)
	asrMS := clipRangesASRDurationMS(merged, utterances)
	if asrMS < 20*60*1000 || asrMS > 26*60*1000 {
		t.Fatalf("fixture ASR = %d, want ~24min", asrMS)
	}

	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	if len(got) != 2 {
		t.Fatalf("groups = %d, want 2", len(got))
	}
	assertGroupsASR(t, got, utterances)
	var maxWall int64
	for _, g := range got {
		d := clipRangesDurationMS(g)
		if d > maxWall {
			maxWall = d
		}
	}
	if maxWall <= aiSliceProjectMaxASRMS {
		t.Fatalf("expected some clips0 > 30min due to silence, max wall = %d", maxWall)
	}
}

func TestSplitClips0IntoProjects_HalfSpeechKeepsNOver30(t *testing.T) {
	// 120 分钟、约一半口播：应分成约 4 个项目，每组 ASR ≤30 且 ≥10。
	const total int64 = 120 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := makeDutyCycleUtterances(0, total, 60*1000, 60*1000)
	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	assertDenseProjectCount(t, total, len(got))
	assertGroupsASR(t, got, utterances)
}

func TestSplitClips0IntoProjects_LeadingSilenceMergesIntoFirstSpeech(t *testing.T) {
	// 片头 30 分钟静音 + 后面 90 分钟口播：第 1 个项目必须带上静音且含 ≥10 分钟 ASR，不能单独成空任务。
	const silenceMS int64 = 30 * 60 * 1000
	const total int64 = 120 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := makeUtterances(silenceMS, total, 10*1000)

	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	if len(got) < 2 {
		t.Fatalf("groups = %d, want >= 2", len(got))
	}
	if got[0][0].StartTime != 0 {
		t.Fatalf("first clips0 start = %d, want 0 (leading silence absorbed)", got[0][0].StartTime)
	}
	if len(filterUtterancesByClips0(utterances, got[0])) == 0 {
		t.Fatal("first project has no ASR utterances")
	}
	assertGroupsASR(t, got, utterances)
	for i, g := range got {
		if len(filterUtterancesByClips0(utterances, g)) == 0 {
			t.Errorf("group[%d] has no ASR utterances: %#v", i, g)
		}
	}
}

func TestSplitClips0IntoProjects_LeadingSilenceLongFirstUtterance(t *testing.T) {
	// 片头静音后第一句超过 30 分钟：不得把静音单独切成一组。
	const silenceMS int64 = 30 * 60 * 1000
	const utterEnd int64 = silenceMS + 40*60*1000
	const total int64 = 90 * 60 * 1000
	merged := []model.ClipRange{{StartTime: 0, EndTime: total}}
	utterances := []asr.Utterance{
		{StartTime: silenceMS, EndTime: utterEnd, Text: "超长开场"},
	}
	utterances = append(utterances, makeUtterances(utterEnd, total, 60*1000)...)

	got := splitClips0IntoProjects(merged, utterances, nil)
	assertSplitCoversMerged(t, merged, got)
	if len(filterUtterancesByClips0(utterances, got[0])) == 0 {
		t.Fatalf("first project has no ASR, clips0=%#v", got[0])
	}
	if clipRangesDurationMS(got[0]) <= silenceMS {
		t.Fatalf("first clips0 duration %d should include silence+speech", clipRangesDurationMS(got[0]))
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

func makeDutyCycleUtterances(start, end, speechMS, gapMS int64) []asr.Utterance {
	if speechMS <= 0 {
		return nil
	}
	out := make([]asr.Utterance, 0)
	t := start
	for t < end {
		uEnd := t + speechMS
		if uEnd > end {
			uEnd = end
		}
		if uEnd > t {
			out = append(out, asr.Utterance{StartTime: t, EndTime: uEnd, Text: "x"})
		}
		t = uEnd + gapMS
	}
	return out
}

func assertDenseProjectCount(t *testing.T, wallMS int64, got int) {
	t.Helper()
	want := int((wallMS + aiSliceProjectTargetWallMS/2) / aiSliceProjectTargetWallMS)
	if want < 1 {
		want = 1
	}
	if got < want-1 || got > want+1 {
		t.Errorf("project count = %d, want ~%d (±1) for %d min wall", got, want, wallMS/(60*1000))
	}
}

func assertGroupsASR(t *testing.T, groups [][]model.ClipRange, utterances []asr.Utterance) {
	t.Helper()
	for i, g := range groups {
		asrMS := clipRangesASRDurationMS(g, utterances)
		if asrMS > aiSliceProjectMaxASRMS {
			t.Errorf("group[%d] ASR %d exceeds 30min", i, asrMS)
		}
		if len(groups) > 1 && asrMS < aiSliceProjectMinASRMS {
			t.Errorf("group[%d] ASR %d below 10min", i, asrMS)
		}
	}
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
