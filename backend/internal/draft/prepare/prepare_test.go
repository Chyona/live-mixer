package prepare

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	body := []byte("source")
	if strings.Contains(strings.ToLower(url), ".m3u8") {
		body = []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:6.000,\nhttps://cdn.example/seg_00000.ts\n")
	}
	return dest, os.WriteFile(dest, body, 0o644)
}

type recordingCutter struct {
	mockCutter
	inputs   []string
	playlists []string
}

func (m *recordingCutter) CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	m.inputs = append(m.inputs, inputPath)
	if b, err := os.ReadFile(inputPath); err == nil {
		m.playlists = append(m.playlists, string(b))
	}
	return m.mockCutter.CutVideoSegment(ctx, inputPath, outputPath, startSec, endSec)
}

func (m *recordingCutter) CutVideoSegmentFast(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	m.inputs = append(m.inputs, inputPath)
	if b, err := os.ReadFile(inputPath); err == nil {
		m.playlists = append(m.playlists, string(b))
	}
	return m.mockCutter.CutVideoSegmentFast(ctx, inputPath, outputPath, startSec, endSec)
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
		// 默认关字幕，保留「超 10 分钟走关键帧快切」的既有行为断言。
		Project: &model.VideoProject{EnableCaptions: model.EnableCaptionsOff},
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

func TestPipeline_CutClips_CaptionsAllowFastOver10Min(t *testing.T) {
	cutter := &mockCutter{}
	root := t.TempDir()
	s := &session.Session{
		JobID:      "job-cap",
		Material:   &model.LiveMaterial{LiveURL: "https://example.com/live.mp4"},
		StagingDir: filepath.Join(root, "staging"),
		RecordDir:  filepath.Join(root, "record"),
		Project:    &model.VideoProject{EnableCaptions: model.EnableCaptionsOn},
		Clips: []model.ClipRange{
			{StartTime: 0, EndTime: FastKeyframeCutMinDurationMS + 1},
		},
	}
	p := NewPipeline(mockDownloader{}, cutter, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(cutter.fast) != 1 {
		t.Fatalf("fast calls = %d, want 1 over 10min even with captions", len(cutter.fast))
	}
	if len(cutter.precise) != 0 {
		t.Fatalf("precise calls = %d, want 0 over 10min", len(cutter.precise))
	}
}

func TestProjectWantsCaptions(t *testing.T) {
	if !projectWantsCaptions(nil) {
		t.Fatal("nil project should default to captions on")
	}
	if projectWantsCaptions(&model.VideoProject{EnableCaptions: model.EnableCaptionsOff}) {
		t.Fatal("off")
	}
	if !projectWantsCaptions(&model.VideoProject{EnableCaptions: model.EnableCaptionsOn}) {
		t.Fatal("on")
	}
}

func TestPipeline_Run_SealsLivePlaylistAsVOD(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	cutter := &recordingCutter{}
	s := &session.Session{
		JobID: "job-hls",
		Material: &model.LiveMaterial{
			LiveStatus:        model.LiveStatusLive,
			RecordPlaylistURL: "https://cdn.example/live.m3u8",
		},
		StagingDir: staging,
		RecordDir:  filepath.Join(root, "record"),
		Clips:      []model.ClipRange{{StartTime: 0, EndTime: 3000}},
	}
	p := NewPipeline(mockDownloader{}, cutter, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(cutter.inputs) != 1 {
		t.Fatalf("cut inputs = %d, want 1", len(cutter.inputs))
	}
	got := cutter.inputs[0]
	if !strings.HasSuffix(got, "source_vod.m3u8") {
		t.Fatalf("cut input = %q, want local source_vod.m3u8", got)
	}
	if strings.HasPrefix(got, "http") {
		t.Fatalf("should not cut from remote live URL: %s", got)
	}
	if len(cutter.playlists) != 1 || !strings.Contains(cutter.playlists[0], "#EXT-X-ENDLIST") {
		t.Fatalf("cut-time playlist missing ENDLIST: %q", cutter.playlists)
	}
	if _, err := os.Stat(filepath.Join(staging, "source_vod.m3u8")); !os.IsNotExist(err) {
		t.Fatalf("source_vod.m3u8 should be cleaned up after Run, err=%v", err)
	}
}

func TestMaterializeVODPlaylist_AppendsENDLIST(t *testing.T) {
	root := t.TempDir()
	s := &session.Session{JobID: "j", StagingDir: root}
	p := NewPipeline(mockDownloader{}, &mockCutter{}, zap.NewNop())
	path, err := p.materializeVODPlaylist(context.Background(), s, "https://cdn.example/live.m3u8")
	if err != nil {
		t.Fatalf("materializeVODPlaylist: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "#EXT-X-ENDLIST") {
		t.Fatalf("missing ENDLIST: %s", body)
	}
	if !strings.Contains(string(body), "seg_00000.ts") {
		t.Fatalf("lost segment: %s", body)
	}
}
