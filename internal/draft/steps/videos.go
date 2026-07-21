package steps

import (
	"context"
	"fmt"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/pkg/capcutmate"

	"go.uber.org/zap"
)

// VideosStep 调用 capcut-mate add_videos，将裁剪切片铺到主视频轨。
type VideosStep struct {
	API    CapCutMateAPI
	Logger *zap.Logger
}

// Name 返回步骤名。
func (VideosStep) Name() string { return "videos" }

// Run 批量添加视频切片并更新 DraftURL / Timeline。
func (st VideosStep) Run(ctx context.Context, s *session.Session) error {
	if s == nil {
		return fmt.Errorf("session 不能为空")
	}
	if st.API == nil {
		return fmt.Errorf("capcut-mate 客户端未配置")
	}
	if s.DraftURL == "" {
		return fmt.Errorf("draft_url 为空，请先执行 create 步骤")
	}
	logger := st.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	if s.Timeline == nil {
		s.Timeline = session.NewTimeline()
	}

	videoInfos, err := buildVideoInfos(s)
	if err != nil {
		return err
	}
	videoInfosJSON, err := capcutmate.BuildVideoInfosJSON(videoInfos)
	if err != nil {
		return err
	}

	logger.Info("调用 capcut-mate 批量添加视频",
		zap.String("job_id", s.JobID),
		zap.Int("clips", len(videoInfos)),
		zap.String("draft_url", s.DraftURL),
	)
	addResp, err := st.API.AddVideos(ctx, capcutmate.AddVideosRequest{
		Alpha:          1,
		DraftURL:       s.DraftURL,
		ScaleX:         1,
		ScaleY:         1,
		SceneTimelines: []any{},
		VideoInfos:     videoInfosJSON,
	}, s.RecordDir)
	if err != nil {
		return fmt.Errorf("向草稿添加视频失败: %w", err)
	}

	if addResp.DraftURL != "" {
		s.DraftURL = addResp.DraftURL
	}
	tr := s.Timeline.EnsureTrack(session.TrackMainVideo)
	tr.TrackID = addResp.TrackID
	tr.SegmentIDs = append([]string(nil), addResp.SegmentIDs...)
	s.ReportProgress(90)
	return nil
}

func buildVideoInfos(s *session.Session) ([]capcutmate.VideoInfo, error) {
	infos := make([]capcutmate.VideoInfo, 0, len(s.ClipPaths))
	for i, localPath := range s.ClipPaths {
		videoURL, err := s.Web.LocalPathToURL(localPath)
		if err != nil {
			return nil, fmt.Errorf("切片路径转 URL 失败: %w", err)
		}
		durMS := s.Clips[i].EndTime - s.Clips[i].StartTime
		if durMS <= 0 {
			return nil, fmt.Errorf("第 %d 段时长无效", i)
		}
		durUS := durMS * 1000 // 毫秒 → 微秒
		startUS := s.Timeline.Advance(durUS)
		infos = append(infos, capcutmate.VideoInfo{
			VideoURL: videoURL,
			Start:    startUS,
			End:      startUS + durUS,
			Volume:   1,
		})
	}
	return infos, nil
}
