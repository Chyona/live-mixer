package service

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"live-mixer/pkg/utils"
	"live-mixer/pkg/utils/urlrewrite"
)

// loggingResumableDownloader 带断点续传、重试与日志输出的默认下载实现。
type loggingResumableDownloader struct {
	logger *zap.Logger
}

// rewritingDownloader 在下载前将 URL Host 替换为内网地址。
type rewritingDownloader struct {
	inner    FileDownloader
	rewriter *urlrewrite.Rewriter
	logger   *zap.Logger
}

func newLoggingResumableDownloader(logger *zap.Logger, rewriter *urlrewrite.Rewriter) FileDownloader {
	if logger == nil {
		logger = zap.NewNop()
	}
	base := loggingResumableDownloader{logger: logger}
	if rewriter == nil || rewriter.Empty() {
		return base
	}
	return rewritingDownloader{inner: base, rewriter: rewriter, logger: logger}
}

// NewFileDownloader 创建带可选内网 URL 转换的默认下载器。
func NewFileDownloader(logger *zap.Logger, rewriter *urlrewrite.Rewriter) FileDownloader {
	return newLoggingResumableDownloader(logger, rewriter)
}

func (d rewritingDownloader) Download(url, dest string) (string, error) {
	downloadURL := url
	if rewritten, ok := d.rewriter.Rewrite(url); ok {
		downloadURL = rewritten
		d.logger.Info("下载 URL 已转换为内网地址",
			zap.String("original_url", url),
			zap.String("download_url", downloadURL),
		)
	}
	return d.inner.Download(downloadURL, dest)
}

func (d loggingResumableDownloader) Download(url, dest string) (string, error) {
	d.logger.Info("开始下载远程文件",
		zap.String("url", url),
		zap.String("dest", dest),
	)

	savedPath, err := utils.DownloadFileWithConfig(url, dest, utils.DownloadConfig{
		MaxRetries: utils.DefaultDownloadMaxRetries,
		OnRetry: func(attempt, maxAttempts int, retryErr error, resumeOffset int64) {
			d.logger.Warn("远程文件下载失败，将从断点重试",
				zap.Int("attempt", attempt),
				zap.Int("max_attempts", maxAttempts),
				zap.Int64("resume_offset", resumeOffset),
				zap.String("url", url),
				zap.String("dest", dest),
				zap.Error(retryErr),
			)
		},
	})
	if err != nil {
		return "", err
	}

	size, statErr := fileSizeOrZero(savedPath)
	if statErr != nil {
		d.logger.Info("远程文件下载完成",
			zap.String("path", savedPath),
			zap.String("url", url),
		)
		return savedPath, nil
	}

	d.logger.Info("远程文件下载完成",
		zap.String("path", savedPath),
		zap.String("url", url),
		zap.Int64("size", size),
	)
	return savedPath, nil
}

func fileSizeOrZero(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat file %s failed: %w", path, err)
	}
	return info.Size(), nil
}
