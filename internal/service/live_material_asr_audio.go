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
	// defaultASRAudioObjectPrefix ASR 临时音频在保存路径下的子目录。
	defaultASRAudioObjectPrefix = "asr"
	// defaultTempDirName 进程工作目录下的临时文件根目录名。
	defaultTempDirName = "temp"
	// defaultASRWorkSubDir ASR 预处理文件在 temp 下的子目录。
	defaultASRWorkSubDir = "asr"
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

	workDir, err := p.createMaterialWorkDir(materialID)
	if err != nil {
		return "", nil, err
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

func (p *liveMaterialASRAudioPreparer) resolveWorkDir() (string, error) {
	if strings.TrimSpace(p.workDir) != "" {
		return p.workDir, nil
	}
	return defaultASRWorkDir()
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

// defaultASRWorkDir 返回进程目录下 temp/asr 路径（不存在时由 createMaterialWorkDir 创建）。
func defaultASRWorkDir() (string, error) {
	base, err := processBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, defaultTempDirName, defaultASRWorkSubDir), nil
}

// createMaterialWorkDir 在 workDir 下创建素材专属临时目录。
// Windows 上 os.MkdirTemp 要求父目录已存在，因此需先 MkdirAll 根目录。
func (p *liveMaterialASRAudioPreparer) createMaterialWorkDir(materialID uint) (string, error) {
	baseDir, err := p.resolveWorkDir()
	if err != nil {
		return "", fmt.Errorf("解析 ASR 工作目录失败: %w", err)
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("创建 ASR 工作根目录失败: %w", err)
	}
	workDir, err := os.MkdirTemp(baseDir, fmt.Sprintf("material-%d-", materialID))
	if err != nil {
		return "", fmt.Errorf("创建临时工作目录失败: %w", err)
	}
	return workDir, nil
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

// buildASRAudioObjectKey 生成对象存储键名，相对路径位于 asr 子目录下（由存储客户端附加 base_path）。
func buildASRAudioObjectKey(prefix string, materialID uint) string {
	prefix = strings.Trim(prefix, "/")
	return fmt.Sprintf("%s/%d/%d.wav", prefix, materialID, time.Now().UnixNano())
}
