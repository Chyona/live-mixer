package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"live-mixer/internal/draft/session"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"

	"go.uber.org/zap"
)

// canUseMediaWindows 素材是否已有覆盖成片区间的就绪媒体窗（直播跟播主路径）。
func canUseMediaWindows(s *session.Session) bool {
	if s == nil || s.Material == nil {
		return false
	}
	m := s.Material
	// 回放走 m3u8/文件；跟播态（含已关播的 live/upcoming）才用媒体窗。
	if m.LiveStatus == model.LiveStatusNone && m.SourceMode == model.SourceModeReplay {
		return false
	}
	if m.SourceMode == model.SourceModeReplay {
		return false
	}
	windows := m.ParsedMediaWindows()
	if windows.ReadyCount() == 0 {
		return false
	}
	var maxEnd int64
	for _, c := range s.Clips {
		if c.EndTime > maxEnd {
			maxEnd = c.EndTime
		}
	}
	return windows.TotalReadyMS() >= maxEnd && maxEnd > 0
}

func (p *Pipeline) cutClipsByMediaWindows(ctx context.Context, s *session.Session, useFast bool) ([]string, error) {
	windows := s.Material.ParsedMediaWindows()
	s.SourceMode = "media_windows"
	s.SourcePath = ""
	p.Logger.Info("按媒体窗 MP4 裁切",
		zap.String("job_id", s.JobID),
		zap.Int("ready_windows", windows.ReadyCount()),
		zap.Int64("ready_ms", windows.TotalReadyMS()),
		zap.Int("clips", len(s.Clips)),
		zap.Bool("fast_keyframe", useFast),
	)

	paths := make([]string, 0, len(s.Clips))
	for i, clip := range s.Clips {
		outPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d.mp4", i))
		if err := p.cutOneClipByMediaWindows(ctx, s, windows, i, clip, outPath, useFast); err != nil {
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

func (p *Pipeline) cutOneClipByMediaWindows(
	ctx context.Context,
	s *session.Session,
	windows model.MediaWindowList,
	clipIndex int,
	clip model.ClipRange,
	outPath string,
	useFast bool,
) error {
	spans, err := windows.ResolveRange(clip.StartTime, clip.EndTime)
	if err != nil {
		return fmt.Errorf("裁剪第 %d 段：媒体窗 ResolveRange 失败: %w", clipIndex, err)
	}
	wantMS := clip.EndTime - clip.StartTime
	p.Logger.Info("开始媒体窗裁剪切片",
		zap.String("job_id", s.JobID),
		zap.Int("index", clipIndex),
		zap.Int64("start_ms", clip.StartTime),
		zap.Int64("end_ms", clip.EndTime),
		zap.Int("spans", len(spans)),
		zap.String("output", outPath),
	)

	resolved := make([]struct {
		path string
		off  int64
		dur  int64
	}, 0, len(spans))
	for _, sp := range spans {
		local, err := p.resolveMediaWindowFile(ctx, s, sp.Window)
		if err != nil {
			return fmt.Errorf("裁剪第 %d 段：获取媒体窗 %d 失败: %w", clipIndex, sp.Window.Index, err)
		}
		resolved = append(resolved, struct {
			path string
			off  int64
			dur  int64
		}{path: local, off: sp.OffsetMS, dur: sp.DurMS})
	}

	if len(resolved) == 1 {
		startSec := float64(resolved[0].off) / 1000.0
		endSec := startSec + float64(resolved[0].dur)/1000.0
		return p.runCut(ctx, resolved[0].path, outPath, startSec, endSec, useFast)
	}

	parts := make([]string, 0, len(resolved))
	for si, sp := range resolved {
		part := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d_w%02d.mp4", clipIndex, si))
		startSec := float64(sp.off) / 1000.0
		endSec := startSec + float64(sp.dur)/1000.0
		if err := p.runCut(ctx, sp.path, part, startSec, endSec, useFast); err != nil {
			for _, pth := range parts {
				_ = os.Remove(pth)
			}
			return fmt.Errorf("裁剪第 %d 段窗片 %d 失败: %w", clipIndex, si, err)
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
			return fmt.Errorf("裁剪第 %d 段：拼接媒体窗失败: %w", clipIndex, err)
		}
		return nil
	}
	listPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d_windows.ffconcat", clipIndex))
	if err := liveingest.WriteFFConcatList(parts, listPath); err != nil {
		return fmt.Errorf("裁剪第 %d 段：写 window concat 失败: %w", clipIndex, err)
	}
	defer os.Remove(listPath)
	return p.runCut(ctx, listPath, outPath, 0, float64(wantMS)/1000.0, useFast)
}

func (p *Pipeline) resolveMediaWindowFile(ctx context.Context, s *session.Session, win model.MediaWindow) (string, error) {
	if dir := strings.TrimSpace(s.LocalIngestDir); dir != "" {
		local := filepath.Join(dir, "windows", liveingest.WindowMP4FileName(win.Index))
		if st, err := os.Stat(local); err == nil && st.Size() > 0 {
			return local, nil
		}
	}
	url := strings.TrimSpace(win.URL)
	if url == "" {
		return "", fmt.Errorf("媒体窗 %d 无 URL", win.Index)
	}
	if p.Downloader == nil {
		return "", fmt.Errorf("下载器未配置，无法拉取媒体窗")
	}
	dest := filepath.Join(s.StagingDir, "windows", liveingest.WindowMP4FileName(win.Index))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}
	if _, err := p.Downloader.Download(ctx, url, dest); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	return dest, nil
}
