package draft

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"

	"go.uber.org/zap"
)

type mockCapCutAPI struct {
	createResp    *capcutmate.CreateDraftResponse
	createErr     error
	addResp       *capcutmate.AddVideosResponse
	addErr        error
	captionsResp  *capcutmate.AddCaptionsResponse
	captionsErr   error
	createCalls   int
	addCalls      int
	captionsCalls int
	lastWidth     int
	lastHeight    int
	lastAdd       capcutmate.AddVideosRequest
	lastCaptions  capcutmate.AddCaptionsRequest
}

func (m *mockCapCutAPI) CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error) {
	m.createCalls++
	m.lastWidth = width
	m.lastHeight = height
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.createResp != nil {
		return m.createResp, nil
	}
	return &capcutmate.CreateDraftResponse{Code: 0, DraftURL: "http://example.com/draft"}, nil
}

func (m *mockCapCutAPI) AddVideos(ctx context.Context, req capcutmate.AddVideosRequest, recordDir string) (*capcutmate.AddVideosResponse, error) {
	m.addCalls++
	m.lastAdd = req
	if m.addErr != nil {
		return nil, m.addErr
	}
	if m.addResp != nil {
		return m.addResp, nil
	}
	return &capcutmate.AddVideosResponse{Code: 0, DraftURL: req.DraftURL}, nil
}

func (m *mockCapCutAPI) AddCaptions(ctx context.Context, req capcutmate.AddCaptionsRequest, recordDir string) (*capcutmate.AddCaptionsResponse, error) {
	m.captionsCalls++
	m.lastCaptions = req
	if m.captionsErr != nil {
		return nil, m.captionsErr
	}
	if m.captionsResp != nil {
		return m.captionsResp, nil
	}
	return &capcutmate.AddCaptionsResponse{Code: 0, DraftURL: req.DraftURL}, nil
}

type mockVideoCutter struct {
	err   error
	calls []string
}

func (m *mockVideoCutter) CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	m.calls = append(m.calls, outputPath)
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(outputPath, []byte("fake-mp4"), 0o644)
}

type mockDownloader struct {
	err error
}

func (m *mockDownloader) Download(url, dest string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, []byte("source"), 0o644)
}

// mockObjectUploader 模拟对象存储上传，返回可识别的公网 URL。
type mockObjectUploader struct {
	err   error
	calls []string
}

func (m *mockObjectUploader) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	m.calls = append(m.calls, objectKey)
	if m.err != nil {
		return "", m.err
	}
	return "https://oss.example/" + objectKey, nil
}

func TestResolveClipRanges_PreferClips1(t *testing.T) {
	project := &model.VideoProject{
		Clips0: []model.ClipRange{{StartTime: 0, EndTime: 100}},
		Clips1: []model.ClipWithText{{Text: "hi", StartTime: 10, EndTime: 50, Words: []model.ClipWord{}}},
	}
	clips, err := prepare.ResolveClipRanges(project)
	if err != nil {
		t.Fatalf("ResolveClipRanges() error = %v", err)
	}
	if len(clips) != 1 || clips[0].StartTime != 10 || clips[0].EndTime != 50 {
		t.Errorf("clips = %#v", clips)
	}
}

func TestMergeAdjacentClipRanges(t *testing.T) {
	const gap = prepare.ClipMergeGapMS
	tests := []struct {
		name string
		in   []model.ClipRange
		want []model.ClipRange
	}{
		{name: "empty", in: nil, want: nil},
		{
			name: "gap_exactly_500_merges",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1500, EndTime: 2000},
			},
			want: []model.ClipRange{{StartTime: 0, EndTime: 2000}},
		},
		{
			name: "gap_501_keeps_two",
			in: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1501, EndTime: 2000},
			},
			want: []model.ClipRange{
				{StartTime: 0, EndTime: 1000},
				{StartTime: 1501, EndTime: 2000},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prepare.MergeAdjacentClipRanges(tt.in, gap)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGenerator_Build_Success(t *testing.T) {
	stagingRoot := t.TempDir()
	jobID := "job-1"
	capcut := &mockCapCutAPI{}
	cutter := &mockVideoCutter{}
	uploader := &mockObjectUploader{}
	gen := NewGenerator(GeneratorDeps{
		CapCut: capcut, Cutter: cutter, Downloader: &mockDownloader{},
		Uploader: uploader, Logger: zap.NewNop(),
	})

	material := &model.LiveMaterial{
		LiveURL: "https://example.com/live.mp4",
		LiveASR: `{"result":{"utterances":[
			{"additions":{},"start_time":100,"end_time":800,"text":"第一段","words":[]},
			{"additions":{},"start_time":2100,"end_time":3000,"text":"第二段","words":[]}
		]}}`,
	}
	project := &model.VideoProject{
		ID: 9, Width: 1080, Height: 1920, EnableCaptions: model.EnableCaptionsOn,
		Clips1: []model.ClipWithText{
			{Text: "a", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}},
			{Text: "b", StartTime: 2000, EndTime: 3500, Words: []model.ClipWord{}},
		},
	}
	var progressLog []int16
	result, err := gen.Build(context.Background(), Request{
		JobID:      jobID,
		Material:   material,
		Project:    project,
		CanvasW:    1080,
		CanvasH:    1920,
		StagingDir: filepath.Join(stagingRoot, "staging", jobID),
		RecordDir:  filepath.Join(stagingRoot, "staging", jobID, "capcut_mate"),
		Progress:   func(local int16) { progressLog = append(progressLog, local) },
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if result.DraftURL != "http://example.com/draft" {
		t.Errorf("DraftURL = %s", result.DraftURL)
	}
	if result.ClipsTarURL != "https://oss.example/temp/draft/job-1/job-1.tar" {
		t.Errorf("ClipsTarURL = %q", result.ClipsTarURL)
	}
	if capcut.createCalls != 1 || capcut.addCalls != 1 || capcut.captionsCalls != 1 {
		t.Errorf("capcut calls create=%d add=%d captions=%d", capcut.createCalls, capcut.addCalls, capcut.captionsCalls)
	}
	if !strings.Contains(capcut.lastCaptions.Captions, "第一段") || !strings.Contains(capcut.lastCaptions.Captions, "第二段") {
		t.Errorf("captions = %s", capcut.lastCaptions.Captions)
	}
	// 第二段字幕应映射到草稿时间轴第二段起点之后（第一段时长 1000ms = 1e6 us）。
	if !strings.Contains(capcut.lastCaptions.Captions, `"start":1100000`) {
		t.Errorf("expected second caption start 1100000 in %s", capcut.lastCaptions.Captions)
	}
	if len(cutter.calls) != 2 {
		t.Errorf("cut calls = %d, want 2", len(cutter.calls))
	}
	// add_videos 应使用对象存储 URL，而非 WEB_ROOT_URL 映射。
	if !strings.Contains(capcut.lastAdd.VideoInfos, "https://oss.example/temp/draft/job-1/clip_000.mp4") {
		t.Errorf("video_infos = %s, want object storage URL", capcut.lastAdd.VideoInfos)
	}
	// 2 个切片 + 1 个 tar 包。
	if len(uploader.calls) != 3 {
		t.Errorf("upload calls = %d, want 3", len(uploader.calls))
	}
	if len(progressLog) == 0 {
		t.Error("expected progress callbacks")
	}
}

func TestGenerator_Build_MergesAdjacentClips(t *testing.T) {
	stagingRoot := t.TempDir()
	cutter := &mockVideoCutter{}
	gen := NewGenerator(GeneratorDeps{
		CapCut: &mockCapCutAPI{}, Cutter: cutter, Downloader: &mockDownloader{},
		Uploader: &mockObjectUploader{}, Logger: zap.NewNop(),
	})
	_, err := gen.Build(context.Background(), Request{
		JobID:    "j",
		Material: &model.LiveMaterial{LiveURL: "https://example.com/a.mp4"},
		Project: &model.VideoProject{
			Clips1: []model.ClipWithText{
				{Text: "a", StartTime: 0, EndTime: 1000, Words: []model.ClipWord{}},
				{Text: "b", StartTime: 1300, EndTime: 2000, Words: []model.ClipWord{}},
			},
		},
		CanvasW: 1080, CanvasH: 1920,
		StagingDir: filepath.Join(stagingRoot, "s"),
		RecordDir:  filepath.Join(stagingRoot, "r"),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(cutter.calls) != 1 {
		t.Fatalf("cut calls = %d, want 1 after merge", len(cutter.calls))
	}
}

func TestGenerator_Build_UsesExplicitClips(t *testing.T) {
	stagingRoot := t.TempDir()
	cutter := &mockVideoCutter{}
	gen := NewGenerator(GeneratorDeps{
		CapCut: &mockCapCutAPI{}, Cutter: cutter, Downloader: &mockDownloader{},
		Uploader: &mockObjectUploader{}, Logger: zap.NewNop(),
	})
	_, err := gen.Build(context.Background(), Request{
		JobID:    "j",
		Material: &model.LiveMaterial{LiveURL: "https://example.com/a.mp4"},
		Project:  &model.VideoProject{Clips1: []model.ClipWithText{{StartTime: 0, EndTime: 9999}}},
		Clips:    []model.ClipRange{{StartTime: 0, EndTime: 500}},
		CanvasW:  720, CanvasH: 1280,
		StagingDir: filepath.Join(stagingRoot, "s"),
		RecordDir:  filepath.Join(stagingRoot, "r"),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(cutter.calls) != 1 {
		t.Fatalf("cut calls = %d, want 1 from explicit clips", len(cutter.calls))
	}
}

func TestGenerator_Build_CapCutFail(t *testing.T) {
	stagingRoot := t.TempDir()
	gen := NewGenerator(GeneratorDeps{
		CapCut: &mockCapCutAPI{createErr: errors.New("capcut down")},
		Cutter: &mockVideoCutter{}, Downloader: &mockDownloader{},
		Uploader: &mockObjectUploader{}, Logger: zap.NewNop(),
	})
	_, err := gen.Build(context.Background(), Request{
		JobID:    "j",
		Material: &model.LiveMaterial{LiveURL: "https://example.com/a.mp4"},
		Project: &model.VideoProject{
			Clips1: []model.ClipWithText{{Text: "a", StartTime: 0, EndTime: 500, Words: []model.ClipWord{}}},
		},
		CanvasW: 1080, CanvasH: 1920,
		StagingDir: filepath.Join(stagingRoot, "s"),
		RecordDir:  filepath.Join(stagingRoot, "r"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerator_Build_UploaderMissing(t *testing.T) {
	stagingRoot := t.TempDir()
	gen := NewGenerator(GeneratorDeps{
		CapCut: &mockCapCutAPI{}, Cutter: &mockVideoCutter{}, Downloader: &mockDownloader{},
		Logger: zap.NewNop(),
	})
	_, err := gen.Build(context.Background(), Request{
		JobID:    "j",
		Material: &model.LiveMaterial{LiveURL: "https://example.com/a.mp4"},
		Project: &model.VideoProject{
			Clips1: []model.ClipWithText{{Text: "a", StartTime: 0, EndTime: 500, Words: []model.ClipWord{}}},
		},
		CanvasW: 1080, CanvasH: 1920,
		StagingDir: filepath.Join(stagingRoot, "s"),
		RecordDir:  filepath.Join(stagingRoot, "r"),
	})
	if err == nil || !strings.Contains(err.Error(), "对象存储未配置") {
		t.Fatalf("error = %v, want 对象存储未配置", err)
	}
}

func TestResolveCanvasSize(t *testing.T) {
	project := &model.VideoProject{Width: 720, Height: 1280}
	w, h := ResolveCanvasSize(1080, 1920, project)
	if w != 1080 || h != 1920 {
		t.Fatalf("request override = %dx%d", w, h)
	}
	w, h = ResolveCanvasSize(0, 0, project)
	if w != 720 || h != 1280 {
		t.Fatalf("project fallback = %dx%d", w, h)
	}
	w, h = ResolveCanvasSize(0, 0, &model.VideoProject{})
	if w != DefaultCanvasWidth || h != DefaultCanvasHeight {
		t.Fatalf("default = %dx%d", w, h)
	}
}
