package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"live-mixer/internal/pkg/media"
	"live-mixer/pkg/utils"
)

const (
	// defaultASRAudioObjectPrefix TOS 上 ASR 临时音频的对象键前缀（与项目桶目录 video_editing/ 对齐）。
	defaultASRAudioObjectPrefix = "video_editing/asr"
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

// utilsFileDownloader 使用 pkg/utils.DownloadFile 的默认下载实现。
type utilsFileDownloader struct{}

func (utilsFileDownloader) Download(url, dest string) (string, error) {
	return utils.DownloadFile(url, dest)
}

// LiveMaterialASRAudioPreparer 为直播素材 ASR 准备公网可访问的 WAV URL。
type LiveMaterialASRAudioPreparer interface {
	// Prepare 下载源媒体、转 WAV、上传对象存储，返回可供 ASR 调用的音频 URL。
	// cleanup 用于删除本地临时文件，调用方应在完成后执行 defer cleanup()。
	Prepare(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (audioURL string, cleanup func(), err error)
}

type liveMaterialASRAudioPreparer struct {
	downloader        FileDownloader
	converter         AudioConverter
	uploader          ObjectUploader
	objectKeyPrefix   string
	workDir           string
}

// NewLiveMaterialASRAudioPreparer 创建 ASR 音频预处理服务。
func NewLiveMaterialASRAudioPreparer(
	downloader FileDownloader,
	converter AudioConverter,
	uploader ObjectUploader,
	workDir string,
) LiveMaterialASRAudioPreparer {
	if downloader == nil {
		downloader = utilsFileDownloader{}
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

	workDir, err := os.MkdirTemp(p.resolveWorkDir(), fmt.Sprintf("material-%d-", materialID))
	if err != nil {
		return "", nil, fmt.Errorf("创建临时工作目录失败: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }

	report := func(progress int16) {
		if onProgress != nil {
			onProgress(progress)
		}
	}

	sourcePath := filepath.Join(workDir, "source"+guessSourceExtension(sourceURL))
	report(10)
	if _, err := p.downloader.Download(sourceURL, sourcePath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("下载直播素材失败: %w", err)
	}
	report(25)

	wavPath := filepath.Join(workDir, "asr.wav")
	if err := p.converter.ConvertToASRWAV(ctx, sourcePath, wavPath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("转码 WAV 失败: %w", err)
	}
	report(40)

	objectKey := buildASRAudioObjectKey(p.objectKeyPrefix, materialID)
	audioURL, err := p.uploader.UploadFile(ctx, wavPath, objectKey)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("上传 ASR 音频失败: %w", err)
	}
	report(45)

	return audioURL, cleanup, nil
}

func (p *liveMaterialASRAudioPreparer) resolveWorkDir() string {
	if strings.TrimSpace(p.workDir) != "" {
		return p.workDir
	}
	return filepath.Join(os.TempDir(), "live-mixer", "asr")
}

// guessSourceExtension 根据 URL 路径猜测源文件扩展名，便于 ffmpeg 识别容器格式。
func guessSourceExtension(sourceURL string) string {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return ".mp4"
	}
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	if ext == "" {
		return ".mp4"
	}
	return ext
}

// buildASRAudioObjectKey 生成对象存储键名，统一放在 video_editing/asr 目录下。
func buildASRAudioObjectKey(prefix string, materialID uint) string {
	prefix = strings.Trim(prefix, "/")
	return fmt.Sprintf("%s/%d/%d.wav", prefix, materialID, time.Now().UnixNano())
}
