package service

import (
	"context"
	"fmt"
	"math"
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
	// asrDurationMismatchWarnSec 对齐后 MP3 与目标时长差超过该阈值（秒）时打 Warn。
	asrDurationMismatchWarnSec = 0.15
)

// FileDownloader 下载远程文件到本地的抽象，便于单元测试注入 mock。
type FileDownloader interface {
	Download(url, dest string) (string, error)
}

// AudioConverter 将媒体文件转为 ASR 适用标准 MP3 的抽象（支持时间轴对齐）。
type AudioConverter interface {
	ConvertToASRMP3Aligned(ctx context.Context, inputPath, outputPath string, align media.ASRAlignOptions) error
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

// ASRAudioPrepareResult ASR 音频预处理结果（含可选分辨率探测）。
type ASRAudioPrepareResult struct {
	AudioURL string
	Width    int
	Height   int
	Cleanup  func()
}

// LiveMaterialASRAudioPreparer 为直播素材 ASR 准备公网可访问的媒体 URL。
type LiveMaterialASRAudioPreparer interface {
	// Prepare 下载源媒体、探测时间轴与分辨率、对齐转标准 MP3、上传对象存储。
	// Cleanup 用于删除本地临时文件，调用方应在完成后执行 defer Cleanup()。
	Prepare(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error)
}

type liveMaterialASRAudioPreparer struct {
	downloader      FileDownloader
	converter       AudioConverter
	uploader        ObjectUploader
	prober          media.MediaTimelineProber
	objectKeyPrefix string
	workDir         string
	logger          *zap.Logger
}

// NewLiveMaterialASRAudioPreparer 创建 ASR 音频预处理服务。
// prober 为 nil 时使用默认 ffprobe 探测器。
func NewLiveMaterialASRAudioPreparer(
	downloader FileDownloader,
	converter AudioConverter,
	uploader ObjectUploader,
	workDir string,
	logger *zap.Logger,
	prober media.MediaTimelineProber,
) LiveMaterialASRAudioPreparer {
	if logger == nil {
		logger = zap.NewNop()
	}
	if downloader == nil {
		downloader = NewFileDownloader(logger, nil)
	}
	if converter == nil {
		converter = media.NewFFmpegConverter("")
	}
	if prober == nil {
		prober = media.NewFFprobeProber("")
	}
	return &liveMaterialASRAudioPreparer{
		downloader:      downloader,
		converter:       converter,
		uploader:        uploader,
		prober:          prober,
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
) (ASRAudioPrepareResult, error) {
	empty := ASRAudioPrepareResult{}
	if p.uploader == nil {
		return empty, fmt.Errorf("对象存储未配置，无法上传 ASR 音频")
	}

	tempDir, err := p.ensureTempDir()
	if err != nil {
		return empty, err
	}

	sessionID := newASRSessionID()
	sourcePath := buildASRSourceLocalPath(tempDir, sessionID)
	mp3Path := buildASRLocalPath(tempDir, sessionID)
	cleanup := func() {
		_ = os.Remove(sourcePath)
		_ = os.Remove(mp3Path)
	}

	p.logger.Info("开始 ASR 音频预处理",
		zap.Uint("material_id", materialID),
		zap.String("source_url", sourceURL),
		zap.String("session_id", sessionID),
		zap.String("source_path", sourcePath),
		zap.String("mp3_path", mp3Path),
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
		return empty, fmt.Errorf("下载直播素材失败: %w", err)
	}
	report(20)

	// 下载完成后探测时间轴与分辨率；失败不阻断后续 ASR（纯音频无视频轨属正常）。
	width, height := 0, 0
	align := media.ASRAlignOptions{}
	if p.prober != nil {
		tl, probeErr := p.prober.ProbeMediaTimeline(ctx, sourcePath)
		if probeErr != nil {
			p.logger.Warn("ffprobe 探测直播素材时间轴失败，将使用默认对齐参数",
				zap.Uint("material_id", materialID),
				zap.String("source_path", sourcePath),
				zap.Error(probeErr),
			)
		} else {
			width, height = tl.Width, tl.Height
			align = tl.AlignOptions()
			p.logger.Info("已探测直播素材时间轴",
				zap.Uint("material_id", materialID),
				zap.Int("width", width),
				zap.Int("height", height),
				zap.Bool("has_video", tl.HasVideo),
				zap.Bool("has_audio", tl.HasAudio),
				zap.Float64("video_start_sec", tl.VideoStartSec),
				zap.Float64("video_duration_sec", tl.VideoDurationSec),
				zap.Float64("audio_start_sec", tl.AudioStartSec),
				zap.Float64("audio_duration_sec", tl.AudioDurationSec),
				zap.Int64("lead_pad_ms", align.LeadPadMs),
				zap.Float64("trim_start_sec", align.TrimStartSec),
				zap.Float64("target_dur_sec", align.TargetDurSec),
			)
		}
	}

	p.logger.Info("开始转码为对齐标准 MP3",
		zap.Uint("material_id", materialID),
		zap.String("input", sourcePath),
		zap.String("output", mp3Path),
		zap.Int64("lead_pad_ms", align.LeadPadMs),
		zap.Float64("trim_start_sec", align.TrimStartSec),
		zap.Float64("target_dur_sec", align.TargetDurSec),
	)
	report(25)
	if err := p.converter.ConvertToASRMP3Aligned(ctx, sourcePath, mp3Path, align); err != nil {
		cleanup()
		return empty, fmt.Errorf("转码 ASR MP3 失败: %w", err)
	}
	report(35)

	p.warnIfDurationMismatch(ctx, materialID, mp3Path, align.TargetDurSec)

	objectKey := buildASRObjectKey(p.objectKeyPrefix, sessionID)
	p.logger.Info("开始上传 ASR 临时媒体",
		zap.Uint("material_id", materialID),
		zap.String("object_key", objectKey),
		zap.String("local_path", mp3Path),
	)
	report(40)
	audioURL, err := p.uploader.UploadFile(ctx, mp3Path, objectKey)
	if err != nil {
		cleanup()
		return empty, fmt.Errorf("上传 ASR 音频失败: %w", err)
	}
	report(45)

	p.logger.Info("ASR 音频预处理完成",
		zap.Uint("material_id", materialID),
		zap.String("audio_url", audioURL),
		zap.Int("width", width),
		zap.Int("height", height),
		zap.Int64("lead_pad_ms", align.LeadPadMs),
		zap.Float64("trim_start_sec", align.TrimStartSec),
		zap.Float64("target_dur_sec", align.TargetDurSec),
	)

	return ASRAudioPrepareResult{
		AudioURL: audioURL,
		Width:    width,
		Height:   height,
		Cleanup:  cleanup,
	}, nil
}

func (p *liveMaterialASRAudioPreparer) warnIfDurationMismatch(ctx context.Context, materialID uint, mp3Path string, targetDurSec float64) {
	if p.prober == nil || targetDurSec <= 0 {
		return
	}
	tl, err := p.prober.ProbeMediaTimeline(ctx, mp3Path)
	if err != nil {
		p.logger.Warn("转码后探测 MP3 时长失败",
			zap.Uint("material_id", materialID),
			zap.String("mp3_path", mp3Path),
			zap.Error(err),
		)
		return
	}
	mp3Dur := tl.FormatDurationSec
	if mp3Dur <= 0 {
		mp3Dur = tl.AudioDurationSec
	}
	delta := math.Abs(mp3Dur - targetDurSec)
	if delta <= asrDurationMismatchWarnSec {
		p.logger.Info("对齐后 MP3 时长校验通过",
			zap.Uint("material_id", materialID),
			zap.Float64("target_dur_sec", targetDurSec),
			zap.Float64("mp3_dur_sec", mp3Dur),
			zap.Float64("delta_sec", delta),
		)
		return
	}
	p.logger.Warn("对齐后 MP3 时长与目标视频时长偏差过大",
		zap.Uint("material_id", materialID),
		zap.Float64("target_dur_sec", targetDurSec),
		zap.Float64("mp3_dur_sec", mp3Dur),
		zap.Float64("delta_sec", delta),
		zap.Float64("warn_threshold_sec", asrDurationMismatchWarnSec),
	)
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

// ensureTempDir 确保临时目录存在，用于存放 asr_{uuid}.src / asr_{uuid}.mp3 等文件。
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
