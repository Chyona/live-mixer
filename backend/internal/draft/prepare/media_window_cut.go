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

// canUseMediaWindows 素材是否已有覆盖成片区间的拼接主 MP4（直播跟播主路径）。
func canUseMediaWindows(s *session.Session) bool {
	if s == nil || s.Material == nil {
		return false
	}
	m := s.Material
	if m.LiveStatus == model.LiveStatusNone && m.SourceMode == model.SourceModeReplay {
		return false
	}
	if m.SourceMode == model.SourceModeReplay {
		return false
	}
	readyMS := m.MasterReadyMS()
	if readyMS <= 0 || m.MasterMP4URL() == "" {
		return false
	}
	var maxEnd int64
	for _, c := range s.Clips {
		if c.EndTime > maxEnd {
			maxEnd = c.EndTime
		}
	}
	return readyMS >= maxEnd && maxEnd > 0
}

func (p *Pipeline) cutClipsByMediaWindows(ctx context.Context, s *session.Session, useFast bool) ([]string, error) {
	readyMS := s.Material.MasterReadyMS()
	if readyMS <= 0 {
		return nil, fmt.Errorf("主 MP4 尚未就绪")
	}
	s.SourceMode = "master_mp4"
	local, err := p.resolveMasterMP4File(ctx, s)
	if err != nil {
		return nil, err
	}
	s.SourcePath = local
	p.Logger.Info("按拼接主 MP4 裁切",
		zap.String("job_id", s.JobID),
		zap.Int64("ready_ms", readyMS),
		zap.Int("clips", len(s.Clips)),
		zap.Bool("fast_keyframe", useFast),
		zap.String("master", local),
	)

	paths := make([]string, 0, len(s.Clips))
	for i, clip := range s.Clips {
		outPath := filepath.Join(s.StagingDir, fmt.Sprintf("clip_%03d.mp4", i))
		if clip.EndTime > readyMS {
			return nil, fmt.Errorf("裁剪第 %d 段：选区超出主 MP4（已就绪 %dms）", i, readyMS)
		}
		startSec := float64(clip.StartTime) / 1000.0
		endSec := float64(clip.EndTime) / 1000.0
		p.Logger.Info("开始主 MP4 裁剪切片",
			zap.String("job_id", s.JobID),
			zap.Int("index", i),
			zap.Int64("start_ms", clip.StartTime),
			zap.Int64("end_ms", clip.EndTime),
			zap.String("output", outPath),
		)
		if err := p.runCut(ctx, local, outPath, startSec, endSec, useFast); err != nil {
			return nil, fmt.Errorf("裁剪第 %d 段失败: %w", i, err)
		}
		paths = append(paths, outPath)
		if len(s.Clips) > 0 {
			localProg := int16(25 + int(25*float64(i+1)/float64(len(s.Clips))))
			s.ReportProgress(localProg)
		}
	}
	return paths, nil
}

func (p *Pipeline) resolveMasterMP4File(ctx context.Context, s *session.Session) (string, error) {
	if dir := strings.TrimSpace(s.LocalIngestDir); dir != "" {
		local := filepath.Join(dir, liveingest.MasterMP4FileName())
		if st, err := os.Stat(local); err == nil && st.Size() > 0 {
			return local, nil
		}
	}
	url := ""
	if s.Material != nil {
		url = s.Material.MasterMP4URL()
		if url == "" {
			url = strings.TrimSpace(s.Material.LiveURL)
			if model.IsProbablyM3U8URL(url) {
				url = ""
			}
		}
	}
	if url == "" {
		return "", fmt.Errorf("主 MP4 无 URL")
	}
	if p.Downloader == nil {
		return "", fmt.Errorf("下载器未配置，无法拉取主 MP4")
	}
	dest := filepath.Join(s.StagingDir, liveingest.MasterMP4FileName())
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
