package steps

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/media"

	"go.uber.org/zap"
)

type stubDurationProber struct {
	msByPath map[string]int64
}

func (s stubDurationProber) ProbeVideoSize(ctx context.Context, inputPath string) (int, int, error) {
	return 0, 0, nil
}

func (s stubDurationProber) ProbeMediaTimeline(ctx context.Context, inputPath string) (media.MediaTimeline, error) {
	ms, ok := s.msByPath[inputPath]
	if !ok || ms <= 0 {
		return media.MediaTimeline{}, errors.New("not found")
	}
	return media.MediaTimeline{FormatDurationSec: float64(ms) / 1000}, nil
}

type mockUploader struct {
	urls map[string]string
	err  error
	keys []string
}

func (m *mockUploader) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	m.keys = append(m.keys, objectKey)
	if m.err != nil {
		return "", m.err
	}
	if m.urls != nil {
		if u, ok := m.urls[localPath]; ok {
			return u, nil
		}
		if u, ok := m.urls[objectKey]; ok {
			return u, nil
		}
	}
	return "https://oss.example/" + objectKey, nil
}

type mockVideosAPI struct {
	lastReq capcutmate.AddVideosRequest
	err     error
}

func (m *mockVideosAPI) CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error) {
	return &capcutmate.CreateDraftResponse{DraftURL: "http://draft"}, nil
}

func (m *mockVideosAPI) AddVideos(ctx context.Context, req capcutmate.AddVideosRequest, recordDir string) (*capcutmate.AddVideosResponse, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &capcutmate.AddVideosResponse{Code: 0, DraftURL: req.DraftURL, TrackID: "track-1"}, nil
}

func (m *mockVideosAPI) AddCaptions(ctx context.Context, req capcutmate.AddCaptionsRequest, recordDir string) (*capcutmate.AddCaptionsResponse, error) {
	return &capcutmate.AddCaptionsResponse{Code: 0, DraftURL: req.DraftURL}, nil
}

func TestBuildDraftClipObjectKey(t *testing.T) {
	got := BuildDraftClipObjectKey("job-1", `D:\tmp\clip_000.mp4`)
	want := "temp/draft/job-1/clip_000.mp4"
	if got != want {
		t.Fatalf("BuildDraftClipObjectKey() = %q, want %q", got, want)
	}
	got = BuildDraftClipObjectKey("  ", "/a/b/c.mp4")
	if got != "temp/draft/unknown/c.mp4" {
		t.Fatalf("empty jobID = %q", got)
	}
}

func TestVideosStep_Run_UploadsAndUsesObjectURL(t *testing.T) {
	clip0 := filepath.Join(t.TempDir(), "clip_000.mp4")
	clip1 := filepath.Join(t.TempDir(), "clip_001.mp4")
	uploader := &mockUploader{}
	api := &mockVideosAPI{}
	step := VideosStep{
		API:      api,
		Uploader: uploader,
		Prober:   stubDurationProber{msByPath: map[string]int64{clip0: 1000, clip1: 1500}},
		Logger:   zap.NewNop(),
	}

	s := &session.Session{
		JobID:     "task-42",
		DraftURL:  "http://example.com/draft",
		ClipPaths: []string{clip0, clip1},
		Clips: []model.ClipRange{
			{StartTime: 0, EndTime: 1000},
			{StartTime: 2000, EndTime: 3500},
		},
		Timeline: session.NewTimeline(),
	}
	if err := step.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(uploader.keys) != 2 {
		t.Fatalf("upload keys = %v, want 2", uploader.keys)
	}
	if uploader.keys[0] != "temp/draft/task-42/clip_000.mp4" {
		t.Errorf("key[0] = %q", uploader.keys[0])
	}
	if !strings.Contains(api.lastReq.VideoInfos, "https://oss.example/temp/draft/task-42/clip_000.mp4") {
		t.Errorf("VideoInfos = %s, want object storage URL", api.lastReq.VideoInfos)
	}
	if !strings.Contains(api.lastReq.VideoInfos, "clip_001.mp4") {
		t.Errorf("VideoInfos missing clip_001: %s", api.lastReq.VideoInfos)
	}
	// VideosStep 应写入 ClipPlacements，供字幕步骤与 add_videos 时间轴对齐。
	if len(s.ClipPlacements) != 2 {
		t.Fatalf("ClipPlacements = %d, want 2", len(s.ClipPlacements))
	}
	if s.ClipPlacements[0].SourceStartMS != 0 || s.ClipPlacements[0].SourceEndMS != 1000 {
		t.Errorf("placement[0] source = %#v", s.ClipPlacements[0])
	}
	if s.ClipPlacements[0].DraftStartUS != 0 || s.ClipPlacements[0].DraftEndUS != 1_000_000 {
		t.Errorf("placement[0] draft = %#v", s.ClipPlacements[0])
	}
	if s.ClipPlacements[1].DraftStartUS != 1_000_000 || s.ClipPlacements[1].DraftEndUS != 2_500_000 {
		t.Errorf("placement[1] draft = %#v", s.ClipPlacements[1])
	}
}

func TestResolveClipDraftDurationMS(t *testing.T) {
	ctx := context.Background()
	prober := stubDurationProber{msByPath: map[string]int64{"a.mp4": 980}}
	if got := resolveClipDraftDurationMS(ctx, prober, "a.mp4", 1000); got != 980 {
		t.Fatalf("got %d want 980", got)
	}
	if got := resolveClipDraftDurationMS(ctx, prober, "missing.mp4", 1000); got != 1000 {
		t.Fatalf("fallback got %d", got)
	}
	// 偏差过大回退理论值
	prober = stubDurationProber{msByPath: map[string]int64{"b.mp4": 8000}}
	if got := resolveClipDraftDurationMS(ctx, prober, "b.mp4", 1000); got != 1000 {
		t.Fatalf("skew fallback got %d", got)
	}
}

func TestVideosStep_Run_UsesProbedDurationOnTimeline(t *testing.T) {
	clip0 := filepath.Join(t.TempDir(), "clip_000.mp4")
	api := &mockVideosAPI{}
	step := VideosStep{
		API:      api,
		Uploader: &mockUploader{},
		Prober:   stubDurationProber{msByPath: map[string]int64{clip0: 900}},
		Logger:   zap.NewNop(),
	}
	s := &session.Session{
		JobID:     "task-dur",
		DraftURL:  "http://example.com/draft",
		ClipPaths: []string{clip0},
		Clips:     []model.ClipRange{{StartTime: 1000, EndTime: 2000}},
		Timeline:  session.NewTimeline(),
	}
	if err := step.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if s.ClipPlacements[0].DraftEndUS-s.ClipPlacements[0].DraftStartUS != 900_000 {
		t.Fatalf("draft dur = %#v, want 900000us", s.ClipPlacements[0])
	}
	if s.ClipPlacements[0].SourceStartMS != 1000 || s.ClipPlacements[0].SourceEndMS != 2000 {
		t.Fatalf("source range should stay requested: %#v", s.ClipPlacements[0])
	}
}

func TestVideosStep_Run_UploaderMissing(t *testing.T) {
	step := VideosStep{API: &mockVideosAPI{}, Logger: zap.NewNop()}
	err := step.Run(context.Background(), &session.Session{DraftURL: "http://d"})
	if err == nil || !strings.Contains(err.Error(), "对象存储未配置") {
		t.Fatalf("error = %v, want 对象存储未配置", err)
	}
}

func TestVideosStep_Run_UploadFailed(t *testing.T) {
	clip := filepath.Join(t.TempDir(), "clip_000.mp4")
	step := VideosStep{
		API:      &mockVideosAPI{},
		Uploader: &mockUploader{err: errors.New("upload down")},
		Logger:   zap.NewNop(),
	}
	err := step.Run(context.Background(), &session.Session{
		JobID: "j", DraftURL: "http://d",
		ClipPaths: []string{clip},
		Clips:     []model.ClipRange{{StartTime: 0, EndTime: 500}},
		Timeline:  session.NewTimeline(),
	})
	if err == nil || !strings.Contains(err.Error(), "上传第 0 段") {
		t.Fatalf("error = %v", err)
	}
}
