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
}

func TestBuildCaptionDiagObjectKey(t *testing.T) {
	if got := BuildCaptionDiagObjectKey("abc"); got != "temp/draft/abc/caption_diag.json" {
		t.Fatalf("got %q", got)
	}
}
