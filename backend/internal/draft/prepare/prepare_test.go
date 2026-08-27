package prepare

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"

	"go.uber.org/zap"
)

func TestTotalClipDurationMS(t *testing.T) {
	tests := []struct {
		name  string
		clips []model.ClipRange
		want  int64
	}{
		{name: "empty", clips: nil, want: 0},
		{
			name:  "single",
			clips: []model.ClipRange{{StartTime: 1000, EndTime: 4000}},
			want:  3000,
		},
		{
			name: "sum_two",
			clips: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 5000, EndTime: 8000},
			},
			want: 4000,
		},
		{
			name:  "skip_invalid",
			clips: []model.ClipRange{{StartTime: 10, EndTime: 10}},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TotalClipDurationMS(tt.clips); got != tt.want {
				t.Errorf("TotalClipDurationMS() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUseFastKeyframeCut(t *testing.T) {
	tests := []struct {
		name  string
		clips []model.ClipRange
		want  bool
	}{
		{
			name:  "exactly_10min_uses_precise",
			clips: []model.ClipRange{{StartTime: 0, EndTime: FastKeyframeCutMinDurationMS}},
			want:  false,
		},
		{
			name:  "within_10min_uses_precise",
			clips: []model.ClipRange{{StartTime: 0, EndTime: FastKeyframeCutMinDurationMS - 1}},
			want:  false,
		},
		{
			name:  "over_10min_uses_fast",
			clips: []model.ClipRange{{StartTime: 0, EndTime: FastKeyframeCutMinDurationMS + 1}},
			want:  true,
		},
		{
			name: "sum_over_10min_uses_fast",
			clips: []model.ClipRange{
				{StartTime: 0, EndTime: 5 * 60 * 1000},
				{StartTime: 400000, EndTime: 400000 + 5*60*1000 + 1},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UseFastKeyframeCut(tt.clips); got != tt.want {
				t.Errorf("UseFastKeyframeCut() = %v, want %v (total=%d)", got, tt.want, TotalClipDurationMS(tt.clips))
			}
		})
	}
}

type mockCutter struct {
	precise []string
	fast    []string
}

func (m *mockCutter) CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	m.precise = append(m.precise, outputPath)
	return os.WriteFile(outputPath, []byte("precise"), 0o644)
}

func (m *mockCutter) CutVideoSegmentFast(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	m.fast = append(m.fast, outputPath)
	return os.WriteFile(outputPath, []byte("fast"), 0o644)
}

type mockDownloader struct{}

func (mockDownloader) Download(ctx context.Context, url, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, []byte("source"), 0o644)
}

func runCutPipeline(t *testing.T, clips []model.ClipRange, cutter *mockCutter) {
	t.Helper()
	root := t.TempDir()
	s := &session.Session{
		JobID:      "job",
		Material:   &model.LiveMaterial{LiveURL: "https://example.com/live.mp4"},
		StagingDir: filepath.Join(root, "staging"),
		RecordDir:  filepath.Join(root, "record"),
		Clips:      clips,
	}
	p := NewPipeline(mockDownloader{}, cutter, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestPipeline_CutClips_PreciseWithin10Min(t *testing.T) {
	cutter := &mockCutter{}
	runCutPipeline(t, []model.ClipRange{
		{StartTime: 0, EndTime: FastKeyframeCutMinDurationMS},
	}, cutter)
	if len(cutter.precise) != 1 {
		t.Fatalf("precise calls = %d, want 1", len(cutter.precise))
	}
	if len(cutter.fast) != 0 {
		t.Fatalf("fast calls = %d, want 0", len(cutter.fast))
	}
}

func TestPipeline_Run_RemovesSourceAfterCut(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	s := &session.Session{
		JobID:      "job",
		Material:   &model.LiveMaterial{LiveURL: "https://example.com/live.mp4"},
		StagingDir: staging,
		RecordDir:  filepath.Join(root, "record"),
		Clips:      []model.ClipRange{{StartTime: 0, EndTime: 1000}},
	}
	p := NewPipeline(mockDownloader{}, &mockCutter{}, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "source.mp4")); !os.IsNotExist(err) {
		t.Fatalf("source.mp4 should be removed after cut, stat err = %v", err)
	}
	if len(s.ClipPaths) != 1 {
		t.Fatalf("ClipPaths = %d, want 1", len(s.ClipPaths))
	}
	if _, err := os.Stat(s.ClipPaths[0]); err != nil {
		t.Fatalf("clip file missing: %v", err)
	}
	if s.SourcePath != "" {
		t.Errorf("SourcePath = %q, want empty after delete", s.SourcePath)
	}
}

func TestPipeline_CutClips_FastWhenOver10Min(t *testing.T) {
	cutter := &mockCutter{}
	runCutPipeline(t, []model.ClipRange{
		{StartTime: 0, EndTime: 400000},
		{StartTime: 500000, EndTime: 500000 + FastKeyframeCutMinDurationMS - 399999},
	}, cutter)
	if len(cutter.fast) != 2 {
		t.Fatalf("fast calls = %d, want 2", len(cutter.fast))
	}
	if len(cutter.precise) != 0 {
		t.Fatalf("precise calls = %d, want 0", len(cutter.precise))
	}
}
