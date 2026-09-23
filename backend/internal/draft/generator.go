package draft

import (
	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/draft/steps"
	"live-mixer/internal/pkg/media"

	"go.uber.org/zap"
)

// GeneratorDeps 构造纯草稿 Generator 所需依赖。
type GeneratorDeps struct {
	CapCut     CapCutMateAPI
	Cutter     prepare.VideoSegmentCutter
	Downloader prepare.FileDownloader
	// Uploader 将本地切片上传到对象存储；add_videos 使用其返回的公网 URL。
	Uploader steps.ObjectUploader
	Logger   *zap.Logger
	// Segmenter 可选的 LLM 断句器；nil 表示字幕行完全由规则折行决定。
	Segmenter CaptionSegmenter
	// NewDownloader 当 Downloader 为 nil 时的工厂。
	NewDownloader func(logger *zap.Logger) prepare.FileDownloader
}

// NewGenerator 创建纯草稿组装器（无任务调度）。
func NewGenerator(deps GeneratorDeps) Generator {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	cutter := deps.Cutter
	if cutter == nil {
		cutter = media.NewFFmpegConverter("")
	}
	downloader := deps.Downloader
	if downloader == nil && deps.NewDownloader != nil {
		downloader = deps.NewDownloader(logger)
	}
	prep := prepare.NewPipeline(downloader, cutter, logger)
	builder := NewBuilder(prep, deps.CapCut, deps.Uploader, logger)
	builder.Segmenter = deps.Segmenter
	return builder
}
