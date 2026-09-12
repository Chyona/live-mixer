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

func (m *mockCutter) ConcatMediaFiles(ctx context.Context, files []string, outputPath string) error {
	return os.WriteFile(outputPath, []byte("concat"), 0o644)
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

type recordingDownloader struct {
	urls []string
}

func (d *recordingDownloader) Download(ctx context.Context, url, dest string) (string, error) {
	d.urls = append(d.urls, url)
	return mockDownloader{}.Download(ctx, url, dest)
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

func TestPipeline_Run_LiveUsesMediaWindows(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	ingest := filepath.Join(root, "ingest")
	if err := os.MkdirAll(ingest, 0o755); err != nil {
		t.Fatal(err)
	}
	masterPath := filepath.Join(ingest, "master.mp4")
	if err := os.WriteFile(masterPath, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	cutter := &recordingCutter{}
	windows := model.MediaWindowList{{
		Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true,
		URL: "https://cdn.example/windows/window_00000.mp4",
	}}
	s := &session.Session{
		JobID: "job-master",
		Material: &model.LiveMaterial{
			LiveStatus:   model.LiveStatusLive,
			LiveURL:      "https://cdn.example/master.mp4",
			Duration:     600000,
			MediaWindows: windows.Marshal(),
		},
		StagingDir:     staging,
		RecordDir:      filepath.Join(root, "record"),
		LocalIngestDir: ingest,
		Clips:          []model.ClipRange{{StartTime: 0, EndTime: 3000}},
	}
	p := NewPipeline(nil, cutter, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.SourceMode != "master_mp4" {
		t.Fatalf("SourceMode=%q want master_mp4", s.SourceMode)
	}
	if len(cutter.inputs) != 1 || !strings.HasSuffix(cutter.inputs[0], "master.mp4") {
		t.Fatalf("cut inputs = %v, want master.mp4", cutter.inputs)
	}
}

func TestPipeline_Run_LiveRejectsWithoutMediaWindows(t *testing.T) {
	root := t.TempDir()
	s := &session.Session{
		JobID: "job-live-no-win",
		Material: &model.LiveMaterial{
			LiveStatus:        model.LiveStatusLive,
			RecordPlaylistURL: "https://cdn.example/live.m3u8",
		},
		StagingDir: filepath.Join(root, "staging"),
		RecordDir:  filepath.Join(root, "record"),
		Clips:      []model.ClipRange{{StartTime: 0, EndTime: 3000}},
	}
	p := NewPipeline(mockDownloader{}, &recordingCutter{}, zap.NewNop())
	err := p.Run(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "主 MP4 尚未覆盖") {
		t.Fatalf("err = %v, want master mp4 coverage error", err)
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

func TestPipeline_Run_PrefersLocalTimelineIndexWhenEndedWithoutFinal(t *testing.T) {
	root := t.TempDir()
	ingest := filepath.Join(root, "ingest")
	if err := os.MkdirAll(ingest, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"seg_00000.ts", "seg_00001.ts"} {
		if err := os.WriteFile(filepath.Join(ingest, name), []byte("ts"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staging := filepath.Join(root, "staging")
	cutter := &recordingCutter{}
	s := &session.Session{
		JobID:          "job-local",
		Material:       &model.LiveMaterial{LiveStatus: model.LiveStatusEnded, RecordPlaylistURL: "https://cdn.example/live.m3u8"},
		StagingDir:     staging,
		RecordDir:      filepath.Join(root, "record"),
		LocalIngestDir: ingest,
		Clips:          []model.ClipRange{{StartTime: 0, EndTime: 1000}},
	}
	p := NewPipeline(mockDownloader{}, cutter, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.SourceMode != "local_timeline_index" {
		t.Fatalf("SourceMode=%q want local_timeline_index", s.SourceMode)
	}
	if len(cutter.inputs) != 1 || !strings.HasSuffix(cutter.inputs[0], "seg_00000.ts") {
		t.Fatalf("want single-seg cut, got %v", cutter.inputs)
	}
}

func TestPipeline_Run_PreferFinalMP4WhenEnded(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	cutter := &recordingCutter{}
	dl := &recordingDownloader{}
	s := &session.Session{
		JobID: "job-ended",
		Material: &model.LiveMaterial{
			LiveStatus: model.LiveStatusEnded,
			URLType:    model.URLTypeFile,
			LiveURL:    "https://cdn.example/final.mp4",
		},
		StagingDir: staging,
		RecordDir:  filepath.Join(root, "record"),
		Clips:      []model.ClipRange{{StartTime: 0, EndTime: 1000}},
	}
	p := NewPipeline(dl, cutter, zap.NewNop())
	if err := p.Run(context.Background(), s); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s.SourceMode != "final_mp4" {
		t.Fatalf("SourceMode=%q want final_mp4", s.SourceMode)
	}
	if len(dl.urls) != 1 || dl.urls[0] != "https://cdn.example/final.mp4" {
		t.Fatalf("download urls=%v", dl.urls)
	}
}
