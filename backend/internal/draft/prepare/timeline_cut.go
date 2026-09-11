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

	// 跨多片：只 concat 本 clip 覆盖的少数 ts，再对短 concat 做输出侧 -ss。
	files := make([]string, 0, len(spans))
	for _, sp := range spans {
		files = append(files, sp.FilePath)
	}
	listPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d_span.ffconcat", clipIndex))
	if err := liveingest.WriteFFConcatList(files, listPath); err != nil {
		return fmt.Errorf("裁剪第 %d 段：写短 concat 失败: %w", clipIndex, err)
	}
	defer os.Remove(listPath)

	startSec := float64(spans[0].OffsetMS) / 1000.0
	endSec := startSec + float64(wantMS)/1000.0
	return p.runCut(ctx, listPath, outPath, startSec, endSec, useFast)
}

func (p *Pipeline) runCut(ctx context.Context, input, output string, startSec, endSec float64, useFast bool) error {
	if useFast {
		return p.Cutter.CutVideoSegmentFast(ctx, input, output, startSec, endSec)
	}
	return p.Cutter.CutVideoSegment(ctx, input, output, startSec, endSec)
}
