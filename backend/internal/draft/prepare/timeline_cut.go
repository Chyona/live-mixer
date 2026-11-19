package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"

	"go.uber.org/zap"
)

// canUseLocalTimelineIndex 本地分片目录是否可按 Index 轴裁切。
func canUseLocalTimelineIndex(s *session.Session) bool {
	if s == nil {
		return false
	}
	dir := filepath.Clean(s.LocalIngestDir)
	if dir == "" || dir == "." {
		return false
	}
	return len(liveingest.GlobLocalSegments(dir)) > 0
}

// loadOrBuildTimelineIndex 读取 sidecar；若为空则按标称时长覆盖已知分片下标。
func loadOrBuildTimelineIndex(dir string) *liveingest.MediaTimelineIndex {
	nominal := int64(model.LiveSegmentDurationSec) * 1000
	if nominal <= 0 {
		nominal = 6000
	}
	idx := liveingest.LoadMediaTimelineIndex(dir, nominal)
	files := liveingest.GlobLocalSegments(dir)
	if len(files) == 0 {
		return idx
	}
	maxIdx := int64(-1)
	for _, f := range files {
		base := filepath.Base(f)
		var n int
		if _, err := fmt.Sscanf(base, "seg_%05d.ts", &n); err == nil && int64(n) > maxIdx {
			maxIdx = int64(n)
		}
	}
	if maxIdx < 0 {
		return idx
	}
	// 确保 Resolve/Range 覆盖到最大分片；缺失片用标称时长。
	if len(idx.Segs) == 0 || idx.Segs[len(idx.Segs)-1].Index < maxIdx {
		idx.SetDuration(maxIdx, timelineSegDurOrNominal(idx, maxIdx))
	}
	return idx
}

func timelineSegDurOrNominal(idx *liveingest.MediaTimelineIndex, segIndex int64) int64 {
	if idx == nil {
		return 6000
	}
	for _, s := range idx.Segs {
		if s.Index == segIndex && s.DurMS > 0 {
			return s.DurMS
		}
	}
	if idx.NominalSegMS > 0 {
		return idx.NominalSegMS
	}
	return 6000
}

// cutClipsByTimelineIndex 按 MediaTimelineIndex 将每段 clip 映射为 seg+offset 裁切（禁止整表 ffconcat 全局 seek）。
func (p *Pipeline) cutClipsByTimelineIndex(ctx context.Context, s *session.Session, useFast bool) ([]string, error) {
	dir := filepath.Clean(s.LocalIngestDir)
	idx := loadOrBuildTimelineIndex(dir)
	s.SourceMode = "local_timeline_index"
	s.SourcePath = dir
	p.Logger.Info("按媒体时间轴索引裁切（seg+offset）",
		zap.String("job_id", s.JobID),
		zap.String("local_ingest_dir", dir),
		zap.Int("index_segs", len(idx.Segs)),
		zap.Int64("index_total_ms", idx.TotalMS),
		zap.Int("clips", len(s.Clips)),
		zap.Bool("fast_keyframe", useFast),
	)

	paths := make([]string, 0, len(s.Clips))
	for i, clip := range s.Clips {
		outPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d.mp4", i))
		if err := p.cutOneClipByTimelineIndex(ctx, s, idx, dir, i, clip, outPath, useFast); err != nil {
			return nil, err
		}
		paths = append(paths, outPath)
		if len(s.Clips) > 0 {
			local := int16(25 + int(25*float64(i+1)/float64(len(s.Clips))))
			s.ReportProgress(local)
		}
	}
	return paths, nil
}

func (p *Pipeline) cutOneClipByTimelineIndex(
	ctx context.Context,
	s *session.Session,
	idx *liveingest.MediaTimelineIndex,
	dir string,
	clipIndex int,
	clip model.ClipRange,
	outPath string,
	useFast bool,
) error {
	spans, err := idx.Range(clip.StartTime, clip.EndTime)
	if err != nil {
		return fmt.Errorf("裁剪第 %d 段：时间轴 Range 失败: %w", clipIndex, err)
	}
	spans = liveingest.AttachFilePaths(dir, spans)
	for _, sp := range spans {
		if st, err := os.Stat(sp.FilePath); err != nil || st.Size() == 0 {
			return fmt.Errorf("裁剪第 %d 段：分片不存在或为空 seg=%d path=%s", clipIndex, sp.SegIndex, sp.FilePath)
		}
	}

	wantMS := clip.EndTime - clip.StartTime
	p.Logger.Info("开始 Index 轴裁剪切片",
		zap.String("job_id", s.JobID),
		zap.Int("index", clipIndex),
		zap.Int64("start_ms", clip.StartTime),
		zap.Int64("end_ms", clip.EndTime),
		zap.Int("spans", len(spans)),
		zap.Int64("first_seg", spans[0].SegIndex),
		zap.Int64("first_offset_ms", spans[0].OffsetMS),
		zap.String("output", outPath),
	)

	if len(spans) == 1 {
		startSec := float64(spans[0].OffsetMS) / 1000.0
		endSec := startSec + float64(spans[0].DurMS)/1000.0
		return p.runCut(ctx, spans[0].FilePath, outPath, startSec, endSec, useFast)
	}

	// 跨多片：逐片按片内 offset 切，再 concat；禁止对多段 TS 做 demuxer -ss（短 ffconcat 仍可能切错）。
	parts := make([]string, 0, len(spans))
	for si, sp := range spans {
		part := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d_p%02d.mp4", clipIndex, si))
		startSec := float64(sp.OffsetMS) / 1000.0
		endSec := startSec + float64(sp.DurMS)/1000.0
		if err := p.runCut(ctx, sp.FilePath, part, startSec, endSec, useFast); err != nil {
			for _, pth := range parts {
				_ = os.Remove(pth)
			}
			return fmt.Errorf("裁剪第 %d 段分片 %d 失败: %w", clipIndex, sp.SegIndex, err)
		}
		parts = append(parts, part)
	}
	defer func() {
		for _, pth := range parts {
			_ = os.Remove(pth)
		}
	}()

	if concat, ok := p.Cutter.(mediaFileConcatenator); ok {
		if err := concat.ConcatMediaFiles(ctx, parts, outPath); err != nil {
			return fmt.Errorf("裁剪第 %d 段：拼接分片失败: %w", clipIndex, err)
		}
		return nil
	}

	// 测试 mock 等无 Concat 时：对已切好的 part 做 0 起点 concat（不再二次 seek 进 TS）。
	listPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d_parts.ffconcat", clipIndex))
	if err := liveingest.WriteFFConcatList(parts, listPath); err != nil {
		return fmt.Errorf("裁剪第 %d 段：写 part concat 失败: %w", clipIndex, err)
	}
	defer os.Remove(listPath)
	return p.runCut(ctx, listPath, outPath, 0, float64(wantMS)/1000.0, useFast)
}

// mediaFileConcatenator 可选：真实 ffmpeg 裁剪器支持无损/重封装拼接。
type mediaFileConcatenator interface {
	ConcatMediaFiles(ctx context.Context, files []string, outputPath string) error
}

func (p *Pipeline) runCut(ctx context.Context, input, output string, startSec, endSec float64, useFast bool) error {
	if useFast {
		return p.Cutter.CutVideoSegmentFast(ctx, input, output, startSec, endSec)
	}
	return p.Cutter.CutVideoSegment(ctx, input, output, startSec, endSec)
}
