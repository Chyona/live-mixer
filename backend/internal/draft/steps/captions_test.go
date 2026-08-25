package steps

import (
	"context"
	"strings"
	"testing"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/capcutmate"

	"go.uber.org/zap"
)

type mockCaptionsAPI struct {
	lastReq     capcutmate.AddCaptionsRequest
	err         error
	called      int
	createCalls int
	addVidCalls int
}

func (m *mockCaptionsAPI) CreateDraft(ctx context.Context, width, height int, recordDir string) (*capcutmate.CreateDraftResponse, error) {
	m.createCalls++
	return &capcutmate.CreateDraftResponse{DraftURL: "http://draft"}, nil
}

func (m *mockCaptionsAPI) AddVideos(ctx context.Context, req capcutmate.AddVideosRequest, recordDir string) (*capcutmate.AddVideosResponse, error) {
	m.addVidCalls++
	return &capcutmate.AddVideosResponse{Code: 0, DraftURL: req.DraftURL}, nil
}

func (m *mockCaptionsAPI) AddCaptions(ctx context.Context, req capcutmate.AddCaptionsRequest, recordDir string) (*capcutmate.AddCaptionsResponse, error) {
	m.called++
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &capcutmate.AddCaptionsResponse{
		Code: 0, DraftURL: req.DraftURL, TrackID: "cap-track",
		SegmentIDs: []string{"seg-1"},
	}, nil
}

func TestBuildCaptionsFromASR_MapsToDraftTimeline(t *testing.T) {
	// 两段切片：源 [0,1000]、[2000,3500] ms；草稿依次铺到 [0,1e6]、[1e6,2.5e6] us。
	placements := []session.ClipPlacement{
		{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		{SourceStartMS: 2000, SourceEndMS: 3500, DraftStartUS: 1_000_000, DraftEndUS: 2_500_000},
	}
	liveASR := `{"result":{"utterances":[
		{"additions":{},"start_time":100,"end_time":800,"text":"第一段话","words":[]},
		{"additions":{},"start_time":2100,"end_time":3000,"text":"第二段话","words":[]},
		{"additions":{},"start_time":5000,"end_time":6000,"text":"不在切片内","words":[]}
	]}}`

	got := BuildCaptionsFromASR(liveASR, placements)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// 第一段：相对源起点 100ms → 草稿 100000us
	if got[0].Text != "第一段话" || got[0].Start != 100_000 || got[0].End != 800_000 {
		t.Errorf("caption[0] = %#v", got[0])
	}
	// 第二段：相对源 100ms 偏移 → 草稿 1_000_000 + 100_000
	if got[1].Text != "第二段话" || got[1].Start != 1_100_000 || got[1].End != 2_000_000 {
		t.Errorf("caption[1] = %#v", got[1])
	}
}

func TestBuildCaptionsFromASR_ClampsPartialOverlap(t *testing.T) {
	// 分句 [500,1500] 与切片 [1000,2000] 重叠 [1000,1500] → 草稿起点 0 时为 [0,500000]
	placements := []session.ClipPlacement{
		{SourceStartMS: 1000, SourceEndMS: 2000, DraftStartUS: 0, DraftEndUS: 1_000_000},
	}
	liveASR := `{"result":{"utterances":[
		{"additions":{},"start_time":500,"end_time":1500,"text":"跨边界","words":[]}
	]}}`

	got := BuildCaptionsFromASR(liveASR, placements)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Start != 0 || got[0].End != 500_000 || got[0].Text != "跨边界" {
		t.Errorf("caption = %#v", got[0])
	}
}

func TestBuildCaptionsFromASR_SplitsLongCaption(t *testing.T) {
	placements := []session.ClipPlacement{
		{SourceStartMS: 0, SourceEndMS: 20000, DraftStartUS: 0, DraftEndUS: 20_000_000},
	}
	// 好， + 超长后半句 → 多条字幕，每条 ≤12 字且首尾无标点
	liveASR := `{"result":{"utterances":[
		{"additions":{},"start_time":0,"end_time":10000,"text":"好，我里面给你们去搭个这个嗯蕾丝美学的米色","words":[]}
	]}}`
	got := BuildCaptionsFromASR(liveASR, placements)
	if len(got) < 2 {
		t.Fatalf("len=%d want >=2: %#v", len(got), got)
	}
	if got[0].Text != "好" {
		t.Errorf("first=%q", got[0].Text)
	}
	for i, c := range got {
		runes := []rune(c.Text)
		if n := len(runes); n > 12 {
			t.Errorf("caption[%d] len=%d text=%q", i, n, c.Text)
		}
		if len(runes) > 0 {
			if runes[0] == '，' || runes[0] == '。' || runes[len(runes)-1] == '，' || runes[len(runes)-1] == '。' {
				t.Errorf("caption[%d] has edge punct: %q", i, c.Text)
			}
		}
		if c.End <= c.Start {
			t.Errorf("caption[%d] bad time %#v", i, c)
		}
	}
}

func TestBuildCaptionsFromASR_Empty(t *testing.T) {
	if got := BuildCaptionsFromASR("", nil); got != nil {
		t.Errorf("empty = %#v", got)
	}
	if got := BuildCaptionsFromASR("{}", []session.ClipPlacement{{SourceStartMS: 0, SourceEndMS: 1}}); got != nil {
		t.Errorf("no utterances = %#v", got)
	}
}

func TestCaptionsStep_Run_Success(t *testing.T) {
	api := &mockCaptionsAPI{}
	step := CaptionsStep{API: api, Logger: zap.NewNop()}
	s := &session.Session{
		JobID:    "job-1",
		DraftURL: "http://example.com/draft",
		Project:  &model.VideoProject{ID: 1, EnableCaptions: model.EnableCaptionsOn},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[
				{"additions":{},"start_time":0,"end_time":500,"text":"你好","words":[]}
			]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		},
		Timeline: session.NewTimeline(),
	}
	if err := step.Run(context.Background(), s); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if api.called != 1 {
		t.Fatalf("AddCaptions calls = %d, want 1", api.called)
	}
	if !strings.Contains(api.lastReq.Captions, "你好") {
		t.Errorf("captions = %s", api.lastReq.Captions)
	}
	if api.lastReq.Font != defaultCaptionFont || api.lastReq.TransformY != defaultCaptionTransformY {
		t.Errorf("style = font=%q ty=%v", api.lastReq.Font, api.lastReq.TransformY)
	}
	if api.lastReq.FontSize != defaultCaptionFontSize {
		t.Errorf("font_size = %d, want %d", api.lastReq.FontSize, defaultCaptionFontSize)
	}
	if api.lastReq.TextColor != defaultCaptionTextColor || api.lastReq.BorderColor != defaultCaptionBorderColor {
		t.Errorf("colors = text=%q border=%q", api.lastReq.TextColor, api.lastReq.BorderColor)
	}
	tr := s.Timeline.Tracks[session.TrackSubtitle]
	if tr == nil || tr.TrackID != "cap-track" {
		t.Errorf("subtitle track = %#v", tr)
	}
}

func TestCaptionsStep_Run_SkipsWhenDisabled(t *testing.T) {
	api := &mockCaptionsAPI{}
	step := CaptionsStep{API: api, Logger: zap.NewNop()}
	err := step.Run(context.Background(), &session.Session{
		DraftURL: "http://d",
		Project:  &model.VideoProject{ID: 9, EnableCaptions: model.EnableCaptionsOff},
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[{"start_time":0,"end_time":100,"text":"有字","words":[]}]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if api.called != 0 {
		t.Fatalf("AddCaptions should be skipped, calls=%d", api.called)
	}
}

func TestCaptionsStep_Run_SkipsWhenNoCaptions(t *testing.T) {
	api := &mockCaptionsAPI{}
	step := CaptionsStep{API: api, Logger: zap.NewNop()}
	err := step.Run(context.Background(), &session.Session{
		DraftURL: "http://d",
		Material: &model.LiveMaterial{LiveASR: "{}"},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if api.called != 0 {
		t.Fatalf("AddCaptions should be skipped, calls=%d", api.called)
	}
}

func TestCaptionsStep_Run_APIError(t *testing.T) {
	api := &mockCaptionsAPI{err: context.DeadlineExceeded}
	step := CaptionsStep{API: api, Logger: zap.NewNop()}
	err := step.Run(context.Background(), &session.Session{
		DraftURL: "http://d",
		Material: &model.LiveMaterial{
			LiveASR: `{"result":{"utterances":[{"additions":{},"start_time":0,"end_time":100,"text":"x","words":[]}]}}`,
		},
		ClipPlacements: []session.ClipPlacement{
			{SourceStartMS: 0, SourceEndMS: 1000, DraftStartUS: 0, DraftEndUS: 1_000_000},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "添加字幕失败") {
		t.Fatalf("error = %v", err)
	}
}
