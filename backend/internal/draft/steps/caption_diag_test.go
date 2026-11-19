package steps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/media"
)

type stubTimelineProber struct {
	durations map[string]int64
}

func (s stubTimelineProber) ProbeMediaTimeline(ctx context.Context, inputPath string) (media.MediaTimeline, error) {
	if d, ok := s.durations[inputPath]; ok {
		return media.MediaTimeline{FormatDurationSec: float64(d) / 1000.0}, nil
	}
	return media.MediaTimeline{}, os.ErrNotExist
}

func (s stubTimelineProber) ProbeVideoSize(ctx context.Context, inputPath string) (width, height int, err error) {
	return 0, 0, nil
}

func TestBuildCaptionDiagReport_ClassifiesCutSkew(t *testing.T) {
	dir := t.TempDir()
	clip0 := filepath.Join(dir, "clip_000.mp4")
	if err := os.WriteFile(clip0, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session.Session{
		JobID:        "diag-1",
		StagingDir:   dir,
		CutMode:      "precise",
		FastKeyframe: false,
		Project:      &model.VideoProject{EnableCaptions: model.EnableCaptionsOn},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[
				{"start_time":100,"end_time":800,"text":"你好","words":[]}
			]}}`,
		},
		ClipPaths: []string{clip0},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_500_000},
		},
	}
	prober := stubTimelineProber{durations: map[string]int64{clip0: 1500}}
	report, err := BuildCaptionDiagReport(context.Background(), s, prober)
	if err != nil {
		t.Fatalf("BuildCaptionDiagReport: %v", err)
	}
	if report.Summary.ClipCount != 1 || report.Summary.SuspectCutClips != 1 {
		t.Fatalf("summary clips=%d suspect_cut=%d", report.Summary.ClipCount, report.Summary.SuspectCutClips)
	}
	if report.Clips[0].DeltaMS != 500 {
		t.Fatalf("delta = %d, want 500", report.Clips[0].DeltaMS)
	}
	if report.Summary.LikelyLayer != "L2_cut" && report.Summary.LikelyLayer != "L2_and_L1" {
		t.Fatalf("likely_layer = %q", report.Summary.LikelyLayer)
	}
}

func TestBuildCaptionDiagReport_MapSelfConsistent(t *testing.T) {
	s := &session.Session{
		JobID:   "diag-2",
		CutMode: "precise",
		Project: &model.VideoProject{EnableCaptions: model.EnableCaptionsOn},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[
				{"start_time":100,"end_time":900,"text":"对齐检查","words":[]}
			]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		},
	}
	report, err := BuildCaptionDiagReport(context.Background(), s, stubTimelineProber{})
	if err != nil {
		t.Fatalf("BuildCaptionDiagReport: %v", err)
	}
	if report.Summary.CaptionCount == 0 {
		t.Fatal("expected captions")
	}
	for _, c := range report.Captions {
		if c.SuspectMap {
			t.Fatalf("map_err should be near 0, got %#v", c)
		}
	}
	if report.Summary.LikelyLayer != "ok_or_mild" {
		t.Fatalf("likely_layer = %q, want ok_or_mild", report.Summary.LikelyLayer)
	}
}

// session.CaptionLines（LLM 断行建议）应既影响字幕拆行，也体现在诊断报告的 caption_lines_source。
func TestBuildCaptionDiagReport_CaptionLinesSource(t *testing.T) {
	s := &session.Session{
		JobID:   "diag-3",
		CutMode: "precise",
		Project: &model.VideoProject{EnableCaptions: model.EnableCaptionsOn},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[
				{"start_time":0,"end_time":900,"text":"原句","words":[]}
			]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		},
		ClipTexts:    []model.ClipWithText{{Text: "第一段话", StartTime: 0, EndTime: 1000}},
		CaptionLines: [][]string{{"第一段", "话"}},
	}

	report, err := BuildCaptionDiagReport(context.Background(), s, stubTimelineProber{})
	if err != nil {
		t.Fatalf("BuildCaptionDiagReport: %v", err)
	}
	if report.CaptionLinesSource != "llm" {
		t.Errorf("caption_lines_source = %q, want llm", report.CaptionLinesSource)
	}
	if len(report.Captions) != 2 || report.Captions[0].Text != "第一段" {
		t.Fatalf("未按断行建议拆行: %#v", report.Captions)
	}

	// 无断行建议时回退规则折行，并标注 rule。
	s.CaptionLines = nil
	report, err = BuildCaptionDiagReport(context.Background(), s, stubTimelineProber{})
	if err != nil {
		t.Fatalf("BuildCaptionDiagReport: %v", err)
	}
	if report.CaptionLinesSource != "rule" {
		t.Errorf("caption_lines_source = %q, want rule", report.CaptionLinesSource)
	}
	if len(report.Captions) != 1 || report.Captions[0].Text != "第一段话" {
		t.Fatalf("规则折行结果异常: %#v", report.Captions)
	}
}

// 切片文案与 words 真实不一致时时间只能按字数均分：诊断要记 timing_source 并归到 L4_timing_proportional，
// 否则「音字不同步」会被误判成 ASR 或裁切问题。
func TestBuildCaptionDiagReport_FlagsProportionalTiming(t *testing.T) {
	s := &session.Session{
		JobID:   "diag-prop",
		CutMode: "precise",
		Project: &model.VideoProject{EnableCaptions: model.EnableCaptionsOn},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 12000, DraftStartUS: 0, DraftEndUS: 12_000_000},
		},
		ClipTexts: []model.ClipWithText{{
			Text:      "明天会更好，我们一起努力走下去",
			StartTime: 0,
			EndTime:   12000,
			// words 与文案对不上（首字即不同），词级对齐必然失败。
			Words: []model.ClipWord{
				{Text: "今", StartTime: 0, EndTime: 1000},
				{Text: "天", StartTime: 1000, EndTime: 2000},
			},
		}},
	}

	report, err := BuildCaptionDiagReport(context.Background(), s, stubTimelineProber{})
	if err != nil {
		t.Fatalf("BuildCaptionDiagReport: %v", err)
	}
	if len(report.Captions) != 2 {
		t.Fatalf("captions = %#v, want 2 lines", report.Captions)
	}
	for _, c := range report.Captions {
		if c.TimingSource != "proportional" {
			t.Fatalf("timing_source = %q, want proportional (%#v)", c.TimingSource, c)
		}
	}
	if report.Summary.ProportionalCaptions != 2 {
		t.Fatalf("proportional_captions = %d, want 2", report.Summary.ProportionalCaptions)
	}
	// 切片 12s ≥ captionDiagProportionalSpanMS：均分漂移肉眼可见，单独计数。
	if report.Summary.ProportionalLongCaptions != 2 {
		t.Fatalf("proportional_long_captions = %d, want 2", report.Summary.ProportionalLongCaptions)
	}
	if report.Summary.LikelyLayer != "L4_timing_proportional" {
		t.Fatalf("likely_layer = %q, want L4_timing_proportional", report.Summary.LikelyLayer)
	}
}

// 线上问题的回归：words 里的填充空格带 start=end=-1，过去会让词级对齐整体失败、
// 整条切片退化为按字数均分。修好后这条要落在 words，不得计入 proportional。
func TestBuildCaptionDiagReport_SpaceFillerKeepsWordTiming(t *testing.T) {
	s := &session.Session{
		JobID:   "diag-space",
		CutMode: "precise",
		Project: &model.VideoProject{EnableCaptions: model.EnableCaptionsOn},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 6000, DraftStartUS: 0, DraftEndUS: 6_000_000},
		},
		ClipTexts: []model.ClipWithText{{
			Text:      "今天聊 K 型时代",
			StartTime: 0,
			EndTime:   6000,
			Words: []model.ClipWord{
				{Text: "今", StartTime: 0, EndTime: 500},
				{Text: "天", StartTime: 500, EndTime: 1000},
				{Text: "聊", StartTime: 1000, EndTime: 1500},
				{Text: " ", StartTime: -1, EndTime: -1},
				{Text: "K", StartTime: 2000, EndTime: 2500},
				{Text: " ", StartTime: -1, EndTime: -1},
				{Text: "型", StartTime: 3000, EndTime: 3500},
				{Text: "时", StartTime: 3500, EndTime: 4000},
				{Text: "代", StartTime: 4000, EndTime: 4500},
			},
		}},
	}

	report, err := BuildCaptionDiagReport(context.Background(), s, stubTimelineProber{})
	if err != nil {
		t.Fatalf("BuildCaptionDiagReport: %v", err)
	}
	if len(report.Captions) != 1 {
		t.Fatalf("captions = %#v, want 1 line", report.Captions)
	}
	if report.Captions[0].TimingSource != "words" {
		t.Fatalf("timing_source = %q, want words", report.Captions[0].TimingSource)
	}
	if report.Summary.ProportionalCaptions != 0 || report.Summary.ProportionalLongCaptions != 0 {
		t.Fatalf("不应有均分字幕: %#v", report.Summary)
	}
	if report.Summary.LikelyLayer != "ok_or_mild" {
		t.Fatalf("likely_layer = %q, want ok_or_mild", report.Summary.LikelyLayer)
	}
	if report.Captions[0].ASRStartMS != 0 || report.Captions[0].ASREndMS != 4500 {
		t.Fatalf("行时间 = %d-%d, want 0-4500", report.Captions[0].ASRStartMS, report.Captions[0].ASREndMS)
	}
}

// 窗口空隙只统计本次草稿用到的源区间：整场 ASR 里别处的接缝与本次成片无关，报出来会把排查带偏。
func TestFillASRWindowGapSummary_ScopedToUsedRanges(t *testing.T) {
	// 空隙 [590s, 630s] 中点 610s，落在 10 分钟窗边界 600s 的 ±45s 内。
	liveASR := `{"result":{"utterances":[
		{"start_time":0,"end_time":590000,"text":"前","words":[]},
		{"start_time":630000,"end_time":700000,"text":"后","words":[]}
	]}}`

	var all CaptionDiagSummary
	fillASRWindowGapSummary(liveASR, nil, &all)
	if all.ASRWindowGapCount != 1 || all.ASRMaxWindowGapMS != 40000 {
		t.Fatalf("不限范围应报出空隙: %#v", all)
	}

	var headOnly CaptionDiagSummary
	fillASRWindowGapSummary(liveASR, usedSourceRanges([]session.ClipPlacement{
		{SourceStartMS: 0, SourceEndMS: 300_000},
	}), &headOnly)
	if headOnly.ASRWindowGapCount != 0 {
		t.Fatalf("用到的区间在接缝之前，不该报空隙: %#v", headOnly)
	}

	var straddle CaptionDiagSummary
	fillASRWindowGapSummary(liveASR, usedSourceRanges([]session.ClipPlacement{
		{SourceStartMS: 600_000, SourceEndMS: 800_000},
	}), &straddle)
	if straddle.ASRWindowGapCount != 1 {
		t.Fatalf("切片跨接缝时应报出空隙: %#v", straddle)
	}
}

func TestWriteCaptionDiagReport(t *testing.T) {
	dir := t.TempDir()
	report := &CaptionDiagReport{
		JobID:   "w1",
		CutMode: "precise",
		Hint:    "ok",
		Summary: CaptionDiagSummary{LikelyLayer: "ok_or_mild"},
	}
	path, err := WriteCaptionDiagReport(dir, report)
	if err != nil {
		t.Fatalf("WriteCaptionDiagReport: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got CaptionDiagReport
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.JobID != "w1" || got.Summary.LikelyLayer != "ok_or_mild" {
		t.Fatalf("got %#v", got)
	}
}

func TestClassifyCaptionDiag(t *testing.T) {
	layer, _ := classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 5, SuspectCutClips: 2, DeltaMSP90: 400,
		CaptionCount: 10, SuspectMapRatio: 0.2, MapErrMSP90Abs: 80,
	})
	if layer != "L2_and_L1" {
		t.Fatalf("layer = %q, want L2_and_L1", layer)
	}
	layer, _ = classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 5, SuspectCutClips: 0, DeltaMSP90: 10,
		CaptionCount: 10, SuspectMapRatio: 0.2, MapErrMSP90Abs: 80,
	})
	if layer != "L1_or_L4_asr" {
		t.Fatalf("layer = %q, want L1_or_L4_asr", layer)
	}
	layer, _ = classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 2, CaptionCount: 5,
		ASRWindowGapCount: 1, ASRMaxWindowGapMS: 8000,
	})
	if layer != "L1_window_boundary" {
		t.Fatalf("layer = %q, want L1_window_boundary", layer)
	}
	layer, _ = classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 5, CaptionCount: 10, SuspectContentClips: 2,
		MapErrMSP90Abs: 0, SuspectMapRatio: 0,
	})
	if layer != "content_seek_mismatch" {
		t.Fatalf("layer = %q, want content_seek_mismatch", layer)
	}

	// 短切片里零星几条均分：words 缺失时唯一可行的分配，不算成簇异常。
	layer, _ = classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 3, CaptionCount: 6, ProportionalCaptions: 1,
	})
	if layer != "ok_or_mild" {
		t.Fatalf("layer = %q, want ok_or_mild", layer)
	}
	// 长切片均分：漂移与条长成正比，一条也算异常（线上问题即此类）。
	layer, _ = classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 3, CaptionCount: 40, ProportionalCaptions: 1, ProportionalLongCaptions: 1,
	})
	if layer != "L4_timing_proportional" {
		t.Fatalf("layer = %q, want L4_timing_proportional", layer)
	}
	// 切片都短但过半字幕都在均分：整批退化，同样要报。
	layer, _ = classifyCaptionDiag(CaptionDiagSummary{
		ClipCount: 3, CaptionCount: 4, ProportionalCaptions: 3,
	})
	if layer != "L4_timing_proportional" {
		t.Fatalf("layer = %q, want L4_timing_proportional", layer)
	}
}

func TestBuildCaptionDiagObjectKey(t *testing.T) {
	if got := BuildCaptionDiagObjectKey("abc"); got != "temp/draft/abc/caption_diag.json" {
		t.Fatalf("got %q", got)
	}
}
