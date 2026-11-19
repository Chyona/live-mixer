package steps

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"

	"go.uber.org/zap"
)

// ObjectUploader 上传本地文件到对象存储，返回公网可访问 URL。
// 与 service.ObjectUploader 签名一致，便于注入 storage.Client。
type ObjectUploader interface {
	UploadFile(ctx context.Context, localPath, objectKey string) (string, error)
}

// VideosStep 调用 capcut-mate add_videos，将裁剪切片铺到主视频轨。
type VideosStep struct {
	API      CapCutMateAPI
	Uploader ObjectUploader
	// Prober 探测切片真实时长；nil 时使用默认 ffprobe。
	Prober media.MediaTimelineProber
	Logger *zap.Logger
}

// Name 返回步骤名。
func (VideosStep) Name() string { return "videos" }

// Run 上传本地切片到对象存储后，批量添加视频并更新 DraftURL / Timeline。
func (st VideosStep) Run(ctx context.Context, s *session.Session) error {
	if s == nil {
		return fmt.Errorf("session 不能为空")
	}
	if st.API == nil {
		return fmt.Errorf("capcut-mate 客户端未配置")
	}
	if st.Uploader == nil {
		return fmt.Errorf("对象存储未配置，无法上传草稿切片")
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

	videoInfos, placements, err := buildVideoInfos(ctx, s, st.Uploader, st.mediaProber(), logger)
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
	// 写入切片映射，供后续字幕步骤按同一时间轴对齐 ASR。
	s.ClipPlacements = placements
	tr := s.Timeline.EnsureTrack(session.TrackMainVideo)
	tr.TrackID = addResp.TrackID
	tr.SegmentIDs = append([]string(nil), addResp.SegmentIDs...)
	s.ReportProgress(85)
	return nil
}

// mediaProber 返回时长探测器；未注入时使用默认 ffprobe。
func (st VideosStep) mediaProber() media.MediaTimelineProber {
	if st.Prober != nil {
		return st.Prober
	}
	return media.NewFFprobeProber("")
}

// buildVideoInfos 将本地切片上传到对象存储，组装 add_videos 所需的 VideoInfo 列表，
// 同时生成源时间轴 ↔ 草稿时间轴的 ClipPlacement，供字幕同步使用。
func buildVideoInfos(ctx context.Context, s *session.Session, uploader ObjectUploader, prober media.MediaTimelineProber, logger *zap.Logger) ([]capcutmate.VideoInfo, []session.ClipPlacement, error) {
	infos := make([]capcutmate.VideoInfo, 0, len(s.ClipPaths))
	placements := make([]session.ClipPlacement, 0, len(s.ClipPaths))
	for i, localPath := range s.ClipPaths {
		objectKey := BuildDraftClipObjectKey(s.JobID, localPath)
		logger.Info("上传草稿切片到对象存储",
			zap.String("job_id", s.JobID),
			zap.Int("clip_index", i),
			zap.String("local_path", localPath),
			zap.String("object_key", objectKey),
		)
		videoURL, err := uploader.UploadFile(ctx, localPath, objectKey)
		if err != nil {
			return nil, nil, fmt.Errorf("上传第 %d 段切片失败: %w", i, err)
		}
		if strings.TrimSpace(videoURL) == "" {
			return nil, nil, fmt.Errorf("第 %d 段切片上传后 URL 为空", i)
		}
		sourceStart := s.Clips[i].StartTime
		sourceEnd := s.Clips[i].EndTime
		wantMS := sourceEnd - sourceStart
		if wantMS <= 0 {
			return nil, nil, fmt.Errorf("第 %d 段时长无效", i)
		}
		// 草稿轨时长用切片真实时长，避免 CapCut 按文件时长铺轨后与字幕（按理论时长）错位。
		durMS := resolveClipDraftDurationMS(ctx, prober, localPath, wantMS)
		durUS := durMS * 1000 // 毫秒 → 微秒
		startUS := s.Timeline.Advance(durUS)
		endUS := startUS + durUS
		infos = append(infos, capcutmate.VideoInfo{
			VideoURL: videoURL,
			Start:    startUS,
			End:      endUS,
			Volume:   1,
		})
		placements = append(placements, session.ClipPlacement{
			SourceStartMS: sourceStart,
			SourceEndMS:   sourceEnd,
			DraftStartUS:  startUS,
			DraftEndUS:    endUS,
		})
	}
	return infos, placements, nil
}

// resolveClipDraftDurationMS 探测切片真实时长；失败或偏差过大时回退理论时长。
func resolveClipDraftDurationMS(ctx context.Context, prober media.MediaTimelineProber, localPath string, wantMS int64) int64 {
	if wantMS <= 0 {
		return 0
	}
	if prober == nil {
		return wantMS
	}
	tl, err := prober.ProbeMediaTimeline(ctx, localPath)
	if err != nil {
		return wantMS
	}
	actual := tl.DurationMS()
	if actual <= 0 {
		return wantMS
	}
	const maxSkewMS int64 = 2500
	if actual > wantMS+maxSkewMS || wantMS > actual+maxSkewMS {
		return wantMS
	}
	return actual
}

// BuildDraftClipObjectKey 生成草稿切片对象键：temp/draft/{jobID}/{文件名}。
// 由 storage.Client 再拼接 base_path（如 video_editing/）。
func BuildDraftClipObjectKey(jobID, localPath string) string {
	name := filepath.Base(localPath)
	id := strings.TrimSpace(jobID)
	if id == "" {
		id = "unknown"
	}
	// 对象键统一使用正斜杠，避免 Windows 路径分隔符污染键名。
	return path.Join(storage.SubDirTemp, "draft", id, name)
}
