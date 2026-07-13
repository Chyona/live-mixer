package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"
	"live-mixer/pkg/utils"

	"go.uber.org/zap"
)

const (
	// defaultASRAudioObjectPrefix ASR 临时文件在 base_path/temp 下的相对路径前缀。
	defaultASRAudioObjectPrefix = storage.SubDirTemp
	// defaultTempDirName 进程工作目录下的临时文件根目录名。
	defaultTempDirName = "temp"
)

// FileDownloader 下载远程文件到本地的抽象，便于单元测试注入 mock。
type FileDownloader interface {
	Download(url, dest string) (string, error)
}

// AudioConverter 将媒体文件转为 ASR 适用 WAV 的抽象。
type AudioConverter interface {
	ConvertToASRWAV(ctx context.Context, inputPath, outputPath string) error
}

// ObjectUploader 上传本地文件到对象存储的抽象。
type ObjectUploader interface {
	UploadFile(ctx context.Context, localPath, objectKey string) (string, error)
}

// utilsFileDownloader 使用 pkg/utils 的默认下载实现（无日志）。
type utilsFileDownloader struct{}

func (utilsFileDownloader) Download(url, dest string) (string, error) {
	return utils.DownloadFile(url, dest)
}

// LiveMaterialASRAudioPreparer 为直播素材 ASR 准备公网可访问的媒体 URL。
type LiveMaterialASRAudioPreparer interface {
	// Prepare 下载源媒体、上传对象存储，返回可供 ASR 调用的媒体 URL。
	// cleanup 用于删除本地临时文件，调用方应在完成后执行 defer cleanup()。
	Prepare(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (string, func(), error)
}

type liveMaterialASRAudioPreparer struct {
	downloader      FileDownloader
	converter         AudioConverter
	uploader          ObjectUploader
	objectKeyPrefix   string
	workDir           string
	logger            *zap.Logger
}

// NewLiveMaterialASRAudioPreparer 创建 ASR 音频预处理服务。
func NewLiveMaterialASRAudioPreparer(
	downloader FileDownloader,
	converter AudioConverter,
	uploader ObjectUploader,
	workDir string,
	logger *zap.Logger,
) LiveMaterialASRAudioPreparer {
	if logger == nil {
		logger = zap.NewNop()
	}
	if downloader == nil {
		downloader = newLoggingResumableDownloader(logger)
	}
	if converter == nil {
		converter = media.NewFFmpegConverter("")
	}
	return &liveMaterialASRAudioPreparer{
		downloader:      downloader,
		converter:       converter,
		uploader:        uploader,
		objectKeyPrefix: defaultASRAudioObjectPrefix,
		workDir:         workDir,
		logger:          logger,
	}
}

func (p *liveMaterialASRAudioPreparer) Prepare(
	ctx context.Context,
	materialID uint,
	sourceURL string,
	onProgress func(progress int16),
) (string, func(), error) {
	if p.uploader == nil {
		return "", nil, fmt.Errorf("对象存储未配置，无法上传 ASR 音频")
	}

	tempDir, err := p.ensureTempDir()
	if err != nil {
		return "", nil, err
	}

	sessionID := newASRSessionID()
	sourcePath := buildASRLocalPath(tempDir, sessionID)
	cleanup := func() {
		_ = os.Remove(sourcePath)
	}

	p.logger.Info("开始 ASR 音频预处理",
		zap.Uint("material_id", materialID),
		zap.String("source_url", sourceURL),
		zap.String("session_id", sessionID),
		zap.String("source_path", sourcePath),
	)

	report := func(progress int16) {
		if onProgress != nil {
			onProgress(progress)
		}
	}

	report(10)
	p.logger.Info("开始下载直播素材",
		zap.Uint("material_id", materialID),
		zap.String("source_url", sourceURL),
		zap.String("dest", sourcePath),
	)
	if _, err := p.downloader.Download(sourceURL, sourcePath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("下载直播素材失败: %w", err)
	}
	report(25)

	report(25)

	objectKey := buildASRObjectKey(p.objectKeyPrefix, sessionID)
	p.logger.Info("开始上传 ASR 临时媒体",
		zap.Uint("material_id", materialID),
		zap.String("object_key", objectKey),
		zap.String("local_path", sourcePath),
	)
	report(40)
	audioURL, err := p.uploader.UploadFile(ctx, sourcePath, objectKey)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("上传 ASR 音频失败: %w", err)
	}
	report(45)

	p.logger.Info("ASR 音频预处理完成",
		zap.Uint("material_id", materialID),
		zap.String("audio_url", audioURL),
	)

	return audioURL, cleanup, nil
}

func (p *liveMaterialASRAudioPreparer) resolveTempDir() (string, error) {
	if strings.TrimSpace(p.workDir) != "" {
		return p.workDir, nil
	}
	return defaultTempDir()
}

// processBaseDir 返回当前进程工作目录；获取失败时回退为可执行文件所在目录。
func processBaseDir() (string, error) {
	if wd, err := os.Getwd(); err == nil {
		return wd, nil
	}
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取进程目录失败: %w", err)
	}
	return filepath.Dir(execPath), nil
}

// defaultTempDir 返回进程目录下 temp 路径。
func defaultTempDir() (string, error) {
	base, err := processBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, defaultTempDirName), nil
}

// ensureTempDir 确保临时目录存在，用于存放 temp/asr_{uuid}.mp4 等文件。
func (p *liveMaterialASRAudioPreparer) ensureTempDir() (string, error) {
	tempDir, err := p.resolveTempDir()
	if err != nil {
		return "", fmt.Errorf("解析临时目录失败: %w", err)
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}
	return tempDir, nil
}
