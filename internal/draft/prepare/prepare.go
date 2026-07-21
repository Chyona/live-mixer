// Package prepare 负责草稿素材准备：下载直播源、合并相邻片段、ffmpeg 裁剪。
// 不调用 capcut-mate。
package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"

	"go.uber.org/zap"
)

// ClipMergeGapMS 相邻片段间隔 ≤ 该值（毫秒）时合并后再 ffmpeg 裁剪。
const ClipMergeGapMS = 500

// FileDownloader 下载远程文件到本地的抽象。
type FileDownloader interface {
	Download(url, dest string) (string, error)
}

// VideoSegmentCutter 视频精确裁剪抽象。
type VideoSegmentCutter interface {
	CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error
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
	if s.Material == nil || s.Material.LiveURL == "" {
		return fmt.Errorf("直播素材 live_url 为空")
	}
	if p.Downloader == nil {
		return fmt.Errorf("下载器未配置")
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

	p.Logger.Info("开始下载直播视频",
		zap.String("job_id", s.JobID),
		zap.String("live_url", s.Material.LiveURL),
		zap.String("staging_dir", s.StagingDir),
	)
	s.ReportProgress(15)

	s.SourcePath = filepath.Join(s.StagingDir, "source.mp4")
	if _, err := p.Downloader.Download(s.Material.LiveURL, s.SourcePath); err != nil {
		return fmt.Errorf("下载直播视频失败: %w", err)
	}

	s.ReportProgress(25)

	paths, err := p.cutClips(ctx, s)
	if err != nil {
		return err
	}
	s.ClipPaths = paths
	s.ReportProgress(50)
	return nil
}

func (p *Pipeline) cutClips(ctx context.Context, s *session.Session) ([]string, error) {
	clips := s.Clips
	paths := make([]string, 0, len(clips))
	for i, clip := range clips {
		outPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d.mp4", i))
		startSec := float64(clip.StartTime) / 1000.0
		endSec := float64(clip.EndTime) / 1000.0
		p.Logger.Info("开始 ffmpeg 裁剪切片",
			zap.String("job_id", s.JobID),
			zap.Int("index", i),
			zap.Int64("start_ms", clip.StartTime),
			zap.Int64("end_ms", clip.EndTime),
			zap.String("output", outPath),
		)
		if err := p.Cutter.CutVideoSegment(ctx, s.SourcePath, outPath, startSec, endSec); err != nil {
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
//（含向前重叠）时合并为 [cur.Start, max(cur.End, next.End)]。
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
