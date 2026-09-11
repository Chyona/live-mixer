// Package prepare 负责草稿素材准备：下载直播源、合并相邻片段、ffmpeg 裁剪。
// 不调用 capcut-mate。
package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"

	"go.uber.org/zap"
)

const (
	// ClipMergeGapMS 相邻片段间隔 ≤ 该值（毫秒）时合并后再 ffmpeg 裁剪。
	ClipMergeGapMS = 500
	// FastKeyframeCutMinDurationMS 成片总时长超过该值时改用关键帧快速裁剪（毫秒）。
	// 10 分钟以内仍走精确重编码。
	FastKeyframeCutMinDurationMS int64 = 10 * 60 * 1000
)

// FileDownloader 下载远程文件到本地的抽象。
type FileDownloader interface {
	Download(ctx context.Context, url, dest string) (string, error)
}

// VideoSegmentCutter 视频裁剪抽象：精确重编码或关键帧快速拷贝。
type VideoSegmentCutter interface {
	CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error
	CutVideoSegmentFast(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error
}

// Pipeline 素材准备流水线。
type Pipeline struct {
	Downloader FileDownloader
	Cutter     VideoSegmentCutter
	Logger     *zap.Logger
}

// NewPipeline 创建素材准备流水线。
func NewPipeline(downloader FileDownloader, cutter VideoSegmentCutter, logger *zap.Logger) *Pipeline {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Pipeline{Downloader: downloader, Cutter: cutter, Logger: logger}
}

// Run 下载直播视频并按 Session.Clips 裁剪出本地切片。
func (p *Pipeline) Run(ctx context.Context, s *session.Session) error {
	if s == nil {
		return fmt.Errorf("session 不能为空")
	}
	if s.Material == nil {
		return fmt.Errorf("直播素材为空")
	}
	if p.Cutter == nil {
		return fmt.Errorf("裁剪器未配置")
	}

	if err := os.MkdirAll(s.StagingDir, 0o755); err != nil {
		return fmt.Errorf("创建任务暂存目录失败: %w", err)
	}
	if err := os.MkdirAll(s.RecordDir, 0o755); err != nil {
		return fmt.Errorf("创建 capcut-mate 录制目录失败: %w", err)
	}

	s.ReportProgress(15)

	// 直播中 / 本地仍有分片：按 Index 轴 seg+offset 裁切（禁止整表 ffconcat 全局 seek）。
	// 已 ended 且可拿到 final.mp4 时优先单文件（见 resolveDraftSourceURL）。
	preferFinal := shouldPreferFinalMP4(s)
	if !preferFinal && canUseLocalTimelineIndex(s) {
		p.Logger.Info("开始准备直播视频（本地时间轴索引）",
			zap.String("job_id", s.JobID),
			zap.String("local_ingest_dir", s.LocalIngestDir),
			zap.String("staging_dir", s.StagingDir),
		)
		s.ReportProgress(25)
		clips := s.Clips
		useFast := UseFastKeyframeCut(clips)
		cutMode := "precise"
		if useFast {
			cutMode = "keyframe_copy"
		}
		s.FastKeyframe = useFast
		s.CutMode = cutMode
		paths, err := p.cutClipsByTimelineIndex(ctx, s, useFast)
		if err != nil {
			return err
		}
		s.ClipPaths = paths
		s.ReportProgress(50)
		return nil
	}

	sourceURL := resolveDraftSourceURL(s.Material)
	if sourceURL == "" {
		// ended 优先 final 失败时，仍可尝试本地分片 Index 裁切。
		if canUseLocalTimelineIndex(s) {
			s.ReportProgress(25)
			useFast := UseFastKeyframeCut(s.Clips)
			s.FastKeyframe = useFast
			if useFast {
				s.CutMode = "keyframe_copy"
			} else {
				s.CutMode = "precise"
			}
			paths, err := p.cutClipsByTimelineIndex(ctx, s, useFast)
			if err != nil {
				return err
			}
			s.ClipPaths = paths
			s.ReportProgress(50)
			return nil
		}
		return fmt.Errorf("直播素材没有可裁剪的媒体地址")
	}
	if p.Downloader == nil {
		return fmt.Errorf("下载器未配置")
	}

	skipDownload := media.IsM3U8URL(sourceURL)
	p.Logger.Info("开始准备直播视频",
		zap.String("job_id", s.JobID),
		zap.String("source_url", sourceURL),
		zap.Bool("hls_direct", skipDownload),
		zap.Bool("prefer_final", preferFinal),
		zap.String("local_ingest_dir", s.LocalIngestDir),
		zap.String("staging_dir", s.StagingDir),
	)

	cleanupSource := func() {}
	if localFinal, ok := tryLocalFinalMP4(s); ok {
		s.SourcePath = localFinal
		s.SourceMode = "final_mp4"
	} else if skipDownload {
		s.SourceMode = "remote_hls"
		// 跟播中的 EVENT playlist 无 ENDLIST 时，ffmpeg 会从 live edge 起播。
		// 先下载清单并 Seal 为 VOD，再按时间轴裁切。
		vodPath, err := p.materializeVODPlaylist(ctx, s, sourceURL)
		if err != nil {
			return err
		}
		s.SourcePath = vodPath
		cleanupSource = func() {
			_ = os.Remove(vodPath)
			if s.SourcePath == vodPath {
				s.SourcePath = ""
			}
		}
	} else {
		if preferFinal || strings.Contains(strings.ToLower(sourceURL), "final") {
			s.SourceMode = "final_mp4"
		} else {
			s.SourceMode = "downloaded"
		}
		s.SourcePath = filepath.Join(s.StagingDir, "source.mp4")
		stopHeartbeat := startDownloadHeartbeat(ctx, s)
		_, err := p.Downloader.Download(ctx, sourceURL, s.SourcePath)
		stopHeartbeat()
		if err != nil {
			_ = os.Remove(s.SourcePath)
			s.SourcePath = ""
			return fmt.Errorf("下载直播视频失败: %w", err)
		}
		cleanupSource = func() { p.removeDownloadedSource(s) }
	}
	defer cleanupSource()

	s.ReportProgress(25)

	paths, err := p.cutClips(ctx, s)
	if err != nil {
		return err
	}
	s.ClipPaths = paths
	s.ReportProgress(50)
	return nil
}

// shouldPreferFinalMP4 关播完成后优先单文件 final.mp4（本地或 LiveURL）。
func shouldPreferFinalMP4(s *session.Session) bool {
	if s == nil || s.Material == nil {
		return false
	}
	m := s.Material
	if m.LiveStatus == model.LiveStatusLive || m.LiveStatus == model.LiveStatusEnding {
		return false
	}
	if _, ok := tryLocalFinalMP4(s); ok {
		return true
	}
	if m.URLType == model.URLTypeFile && strings.TrimSpace(m.LiveURL) != "" {
		return true
	}
	if m.LiveStatus == model.LiveStatusEnded && strings.TrimSpace(m.LiveURL) != "" {
		return true
	}
	return false
}

// tryLocalFinalMP4 若本地 ingest 目录仍有 final.mp4 则返回路径。
func tryLocalFinalMP4(s *session.Session) (string, bool) {
	if s == nil {
		return "", false
	}
	dir := strings.TrimSpace(s.LocalIngestDir)
	if dir == "" {
		return "", false
	}
	p := filepath.Join(dir, "final.mp4")
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		return "", false
	}
	return p, true
}

// materializeVODPlaylist 下载远程 m3u8 并补 ENDLIST，返回本地路径供 ffmpeg 随机访问。
func (p *Pipeline) materializeVODPlaylist(ctx context.Context, s *session.Session, sourceURL string) (string, error) {
	dest := filepath.Join(s.StagingDir, "source_vod.m3u8")
	if _, err := p.Downloader.Download(ctx, sourceURL, dest); err != nil {
		return "", fmt.Errorf("下载跟播播放列表失败: %w", err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		_ = os.Remove(dest)
		return "", fmt.Errorf("读取跟播播放列表失败: %w", err)
	}
	// 先删再写：CachingDownloader 可能硬链接到共享缓存，直接覆盖会污染缓存。
	_ = os.Remove(dest)
	sealed := liveingest.SealPlaylistAsVOD(string(raw))
	hadEndlist := strings.Contains(string(raw), "#EXT-X-ENDLIST")
	if err := os.WriteFile(dest, []byte(sealed), 0o644); err != nil {
		_ = os.Remove(dest)
		return "", fmt.Errorf("写入 VOD 播放列表失败: %w", err)
	}
	p.Logger.Info("已物化 VOD 播放列表供裁切",
		zap.String("job_id", s.JobID),
		zap.String("source_url", sourceURL),
		zap.String("local_playlist", dest),
		zap.Bool("appended_endlist", !hadEndlist),
	)
	return dest, nil
}

func resolveDraftSourceURL(m *model.LiveMaterial) string {
	if m == nil {
		return ""
	}
	// 已 ended / url_type=file：优先 final.mp4（LiveURL），远程 HLS 仅降级。
	if m.URLType == model.URLTypeFile && strings.TrimSpace(m.LiveURL) != "" {
		return strings.TrimSpace(m.LiveURL)
	}
	if m.LiveStatus == model.LiveStatusEnded && strings.TrimSpace(m.LiveURL) != "" {
		return strings.TrimSpace(m.LiveURL)
	}
	if m.LiveStatus == model.LiveStatusLive || m.LiveStatus == model.LiveStatusEnding {
		if m.RecordPlaylistURL != "" {
			return m.RecordPlaylistURL
		}
	}
	return m.ProcessMediaURL()
}

// removeDownloadedSource 删除 prepare 下载的全量直播源；文件不存在时忽略。
func (p *Pipeline) removeDownloadedSource(s *session.Session) {
	if s == nil || s.SourcePath == "" {
		return
	}
	path := s.SourcePath
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			p.Logger.Warn("删除直播源文件失败",
				zap.String("job_id", s.JobID),
				zap.String("path", path),
				zap.Error(err),
			)
		}
		return
	}
	p.Logger.Info("已删除直播源文件",
		zap.String("job_id", s.JobID),
		zap.String("path", path),
	)
	s.SourcePath = ""
}

// startDownloadHeartbeat 在大文件下载期间周期性回写进度，刷新 task.updated_at。
// 仅当本地文件字节数增长时才心跳，避免「连接假死但心跳仍刷」导致 RequeueStale 永不回收。
func startDownloadHeartbeat(ctx context.Context, s *session.Session) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		var lastSize int64 = -1
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				path := ""
				if s != nil {
					path = s.SourcePath
				}
				size := fileSizeOrZero(path)
				if size <= lastSize {
					continue
				}
				lastSize = size
				s.ReportProgress(15)
			}
		}
	}()
	return func() { close(done) }
}

func fileSizeOrZero(path string) int64 {
	if path == "" {
		return 0
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func (p *Pipeline) cutClips(ctx context.Context, s *session.Session) ([]string, error) {
	clips := s.Clips
	totalMS := TotalClipDurationMS(clips)
	// 成片总时长 >10 分钟走关键帧快切；字幕对齐依赖 VideosStep 探测真实片长 + CaptionsStep 按比例缩放。
	// 不再因「开字幕」强制全精确重编码：对 HLS 直切时精确模式曾极慢（逐段从片头解码）。
	useFast := UseFastKeyframeCut(clips)
	cutMode := "precise"
	if useFast {
		cutMode = "keyframe_copy"
	}
	s.FastKeyframe = useFast
	s.CutMode = cutMode
	p.Logger.Info("选择草稿切片裁剪方式",
		zap.String("job_id", s.JobID),
		zap.Int("clips", len(clips)),
		zap.Int64("total_duration_ms", totalMS),
		zap.Int64("fast_cut_threshold_ms", FastKeyframeCutMinDurationMS),
		zap.Bool("fast_keyframe", useFast),
		zap.String("cut_mode", cutMode),
		zap.Bool("captions", projectWantsCaptions(s.Project)),
	)
	paths := make([]string, 0, len(clips))
	for i, clip := range clips {
		outPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d.mp4", i))
		startSec := float64(clip.StartTime) / 1000.0
		endSec := float64(clip.EndTime) / 1000.0
		mode := cutMode
		p.Logger.Info("开始 ffmpeg 裁剪切片",
			zap.String("job_id", s.JobID),
			zap.Int("index", i),
			zap.Int64("start_ms", clip.StartTime),
			zap.Int64("end_ms", clip.EndTime),
			zap.String("mode", mode),
			zap.String("output", outPath),
		)
		var err error
		if useFast {
			err = p.Cutter.CutVideoSegmentFast(ctx, s.SourcePath, outPath, startSec, endSec)
		} else {
			err = p.Cutter.CutVideoSegment(ctx, s.SourcePath, outPath, startSec, endSec)
		}
		if err != nil {
			return nil, fmt.Errorf("裁剪第 %d 段失败: %w", i, err)
		}
		paths = append(paths, outPath)

		// 裁剪阶段本地进度：25 → 50，按片段数线性推进。
		if len(clips) > 0 {
			local := int16(25 + int(25*float64(i+1)/float64(len(clips))))
			s.ReportProgress(local)
		}
	}
	return paths, nil
}

// TotalClipDurationMS 返回成片总时长（各片段时长之和，毫秒）。
func TotalClipDurationMS(clips []model.ClipRange) int64 {
	var total int64
	for _, clip := range clips {
		if clip.EndTime > clip.StartTime {
			total += clip.EndTime - clip.StartTime
		}
	}
	return total
}

// UseFastKeyframeCut 成片总时长超过 10 分钟时使用关键帧快速裁剪。
func UseFastKeyframeCut(clips []model.ClipRange) bool {
	return TotalClipDurationMS(clips) > FastKeyframeCutMinDurationMS
}

// projectWantsCaptions 项目需要生成字幕时，裁剪必须帧精确，避免与 ASR 时间轴错位。
func projectWantsCaptions(project *model.VideoProject) bool {
	if project == nil {
		return true
	}
	return project.EnableCaptions != model.EnableCaptionsOff
}

// ResolveClipRanges 优先从 video_project.clips1 提取时间段；为空则回退 clips0。
func ResolveClipRanges(project *model.VideoProject) ([]model.ClipRange, error) {
	if project == nil {
		return nil, fmt.Errorf("video_project 不能为空")
	}
	if clips := clips1ToRanges(project.Clips1); len(clips) > 0 {
		return clips, nil
	}
	if len(project.Clips0) == 0 {
		return nil, fmt.Errorf("video_project.clips1/clips0 均为空，无法生成草稿")
	}
	return project.Clips0, nil
}

func clips1ToRanges(clips []model.ClipWithText) []model.ClipRange {
	out := make([]model.ClipRange, 0, len(clips))
	for _, c := range clips {
		out = append(out, model.ClipRange{StartTime: c.StartTime, EndTime: c.EndTime})
	}
	return out
}

// ValidateClipRanges 校验切片时间段合法。
func ValidateClipRanges(clips []model.ClipRange) error {
	for _, clip := range clips {
		if clip.StartTime < 0 || clip.EndTime <= clip.StartTime {
			return fmt.Errorf("clips 时间段无效：start_time 须小于 end_time 且均非负")
		}
	}
	return nil
}

// MergeAdjacentClipRanges 按列表顺序合并相邻片段：严格保留入参顺序，不排序。
// 仅当列表中相邻两项满足 next.Start >= cur.Start 且 gap=next.Start-cur.End ≤ maxGapMS
// （含向前重叠）时合并为 [cur.Start, max(cur.End, next.End)]。
func MergeAdjacentClipRanges(clips []model.ClipRange, maxGapMS int64) []model.ClipRange {
	if len(clips) == 0 {
		return nil
	}
	if len(clips) == 1 {
		return []model.ClipRange{{StartTime: clips[0].StartTime, EndTime: clips[0].EndTime}}
	}

	out := make([]model.ClipRange, 0, len(clips))
	cur := clips[0]
	for i := 1; i < len(clips); i++ {
		next := clips[i]
		gap := next.StartTime - cur.EndTime
		// 列表顺序优先：时间上回跳的相邻项不合，避免负 gap 误吞片段。
		if next.StartTime >= cur.StartTime && gap <= maxGapMS {
			if next.EndTime > cur.EndTime {
				cur.EndTime = next.EndTime
			}
			continue
		}
		out = append(out, cur)
		cur = next
	}
	out = append(out, cur)
	return out
}
