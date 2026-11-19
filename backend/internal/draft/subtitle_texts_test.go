package draft

import (
	"context"
	"strings"
	"testing"

	"live-mixer/internal/model"

	"go.uber.org/zap"
)

// stubCaptionSegmenter 返回预设断行，便于验证 SubtitleTexts 复用草稿断行逻辑。
type stubCaptionSegmenter struct {
	calls    int
	gotTexts []string
	result   [][]string
}

func (s *stubCaptionSegmenter) SegmentTexts(_ context.Context, texts []string) [][]string {
	s.calls++
	s.gotTexts = texts
	if s.result != nil {
		return s.result
	}
	return make([][]string, len(texts))
}

// SubtitleTexts 的文本必须来自与草稿相同的合并/断行管线，且忽略未开启字幕的项目。
func TestBuilder_SubtitleTexts_UsesCaptionPipeline(t *testing.T) {
	b := NewBuilder(nil, nil, nil, zap.NewNop())
	segmenter := &stubCaptionSegmenter{result: [][]string{{"第一行字幕", "第二行字幕"}}}
	b.Segmenter = segmenter

	project := &model.VideoProject{
		EnableCaptions: model.EnableCaptionsOn,
		Clips1: []model.ClipWithText{
			{Text: "第一行字幕第二行字幕", StartTime: 0, EndTime: 1000},
		},
	}

	lines, err := b.SubtitleTexts(context.Background(), "job-1", &model.LiveMaterial{}, project)
	if err != nil {
		t.Fatalf("SubtitleTexts() error = %v", err)
	}
	if got, want := strings.Join(lines, "|"), "第一行字幕|第二行字幕"; got != want {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if segmenter.calls != 1 || len(segmenter.gotTexts) != 1 || segmenter.gotTexts[0] != "第一行字幕第二行字幕" {
		t.Errorf("segmenter calls = %d, texts = %#v", segmenter.calls, segmenter.gotTexts)
	}
}

func TestBuilder_SubtitleTexts_SkipsWhenCaptionsDisabled(t *testing.T) {
	b := NewBuilder(nil, nil, nil, zap.NewNop())
	project := &model.VideoProject{
		EnableCaptions: model.EnableCaptionsOff,
		Clips1: []model.ClipWithText{
			{Text: "文案", StartTime: 0, EndTime: 1000},
		},
	}

	lines, err := b.SubtitleTexts(context.Background(), "job-1", nil, project)
	if err != nil {
		t.Fatalf("SubtitleTexts() error = %v", err)
	}
	if lines != nil {
		t.Fatalf("lines = %#v, want nil", lines)
	}
}

// 切片文案为空时应与草稿一致回退到 ASR 分句。
func TestBuilder_SubtitleTexts_FallsBackToASRWhenTextEmpty(t *testing.T) {
	b := NewBuilder(nil, nil, nil, zap.NewNop())
	project := &model.VideoProject{
		EnableCaptions: model.EnableCaptionsOn,
		Clips1: []model.ClipWithText{
			{Text: "", StartTime: 0, EndTime: 1000},
		},
	}
	material := &model.LiveMaterial{
		LiveASR: `{"result":{"utterances":[
			{"additions":{},"start_time":0,"end_time":1000,"text":"ASR字幕","words":[]}
		]}}`,
	}

	lines, err := b.SubtitleTexts(context.Background(), "job-1", material, project)
	if err != nil {
		t.Fatalf("SubtitleTexts() error = %v", err)
	}
	if len(lines) != 1 || lines[0] != "ASR字幕" {
		t.Fatalf("lines = %#v, want [ASR字幕]", lines)
	}
}
