package steps

import (
	"context"
	"fmt"
	"strings"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/capcutmate"

	"go.uber.org/zap"
)

// 默认字幕样式（与 capcut-mate add_captions 文档示例对齐）。
const (
	defaultCaptionAlignment   = 1
	defaultCaptionAlpha       = 1.0
	defaultCaptionTextColor   = "#ffde00"
	defaultCaptionBorderColor = "#000000"
	defaultCaptionFontSize    = 10
	defaultCaptionScale       = 1.0
	defaultCaptionTransformY  = -360.0
	defaultCaptionFont        = "得意黑"
)

// CaptionsStep 在 add_videos 之后调用 add_captions，用 ASR 分句生成与视频切片同步的字幕。
type CaptionsStep struct {
	API    CapCutMateAPI
	Logger *zap.Logger
}

// Name 返回步骤名。
func (CaptionsStep) Name() string { return NameCaptions }

// Run 从 Material.LiveASR 解析分句，按 VideosStep 写入的 ClipPlacements 映射到草稿时间轴后批量添加字幕。
// 无可用字幕时跳过接口调用（不视为失败），保证无 ASR 场景仍可产出草稿。
func (st CaptionsStep) Run(ctx context.Context, s *session.Session) error {
	if s == nil {
		return fmt.Errorf("session 不能为空")
	}
	if st.API == nil {
		return fmt.Errorf("capcut-mate 客户端未配置")
	}
	if s.DraftURL == "" {
		return fmt.Errorf("draft_url 为空，请先执行 create/videos 步骤")
	}
	logger := st.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	if s.Project != nil && s.Project.EnableCaptions == model.EnableCaptionsOff {
		logger.Info("项目未开启字幕，跳过 add_captions",
			zap.String("job_id", s.JobID),
			zap.Uint("video_project_id", s.Project.ID),
		)
		s.ReportProgress(95)
		return nil
	}

	liveASR := ""
	if s.Material != nil {
		liveASR = s.Material.LiveASR
	}
	items := BuildCaptionsFromASR(liveASR, s.ClipPlacements)
	if len(items) == 0 {
		logger.Info("无可用 ASR 字幕，跳过 add_captions",
			zap.String("job_id", s.JobID),
			zap.Int("placements", len(s.ClipPlacements)),
		)
		s.ReportProgress(95)
		return nil
	}

	captionsJSON, err := capcutmate.BuildCaptionsJSON(items)
	if err != nil {
		return err
	}

	logger.Info("调用 capcut-mate 批量添加字幕",
		zap.String("job_id", s.JobID),
		zap.Int("captions", len(items)),
		zap.String("draft_url", s.DraftURL),
	)
	resp, err := st.API.AddCaptions(ctx, capcutmate.AddCaptionsRequest{
		DraftURL:    s.DraftURL,
		Captions:    captionsJSON,
		Alignment:   defaultCaptionAlignment,
		Alpha:       defaultCaptionAlpha,
		TextColor:   defaultCaptionTextColor,
		BorderColor: defaultCaptionBorderColor,
		FontSize:    defaultCaptionFontSize,
		ScaleX:      defaultCaptionScale,
		ScaleY:      defaultCaptionScale,
		TransformX:  0,
		TransformY:  defaultCaptionTransformY,
		StyleText:   false,
		Underline:   false,
		Italic:      false,
		Bold:        false,
		TextEffect:  "",
		Font:        defaultCaptionFont,
	}, s.RecordDir)
	if err != nil {
		return fmt.Errorf("向草稿添加字幕失败: %w", err)
	}
	if resp.DraftURL != "" {
		s.DraftURL = resp.DraftURL
	}
	if s.Timeline == nil {
		s.Timeline = session.NewTimeline()
	}
	tr := s.Timeline.EnsureTrack(session.TrackSubtitle)
	tr.TrackID = resp.TrackID
	tr.SegmentIDs = append([]string(nil), resp.SegmentIDs...)
	s.ReportProgress(95)
	return nil
}

// BuildCaptionsFromASR 将 live_asr JSON 分句映射到草稿字幕时间轴。
// placements 来自 VideosStep，保证字幕 start/end（微秒）与 add_videos 切片一一对齐。
// 长句会先按标点/字数断句并切分时间，再与切片重叠裁剪，避免字幕越出对应视频段。
func BuildCaptionsFromASR(liveASRJSON string, placements []session.ClipPlacement) []capcutmate.CaptionItem {
	utterances := asr.FormatUtterancesForAPI(liveASRJSON)
	if len(utterances) == 0 || len(placements) == 0 {
		return nil
	}

	out := make([]capcutmate.CaptionItem, 0)
	for _, p := range placements {
		for _, u := range utterances {
			if strings.TrimSpace(u.Text) == "" {
				continue
			}
			// 分句与切片无重叠则跳过。
			if u.EndTime <= p.SourceStartMS || u.StartTime >= p.SourceEndMS {
				continue
			}
			for _, seg := range asr.SplitUtteranceForCaptions(u) {
				text := strings.TrimSpace(seg.Text)
				if text == "" {
					continue
				}
				if seg.EndTime <= p.SourceStartMS || seg.StartTime >= p.SourceEndMS {
					continue
				}
				overlapStartMS := seg.StartTime
				if overlapStartMS < p.SourceStartMS {
					overlapStartMS = p.SourceStartMS
				}
				overlapEndMS := seg.EndTime
				if overlapEndMS > p.SourceEndMS {
					overlapEndMS = p.SourceEndMS
				}
				if overlapEndMS <= overlapStartMS {
					continue
				}
				draftStartUS := p.DraftStartUS + (overlapStartMS-p.SourceStartMS)*1000
				draftEndUS := p.DraftStartUS + (overlapEndMS-p.SourceStartMS)*1000
				if draftEndUS > p.DraftEndUS {
					draftEndUS = p.DraftEndUS
				}
				if draftEndUS <= draftStartUS {
					continue
				}
				out = append(out, capcutmate.CaptionItem{
					Start: draftStartUS,
					End:   draftEndUS,
					Text:  text,
				})
			}
		}
	}
	return out
}
