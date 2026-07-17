// Package media 封装直播素材处理所需的本地媒体转码能力。
package media

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// DefaultASRSampleRate 豆包 ASR 推荐的采样率（16kHz 在识别准确率与体积之间平衡较好）。
	DefaultASRSampleRate = 16000
	// DefaultASRChannels 单声道可显著减小体积，且满足语音识别场景。
	DefaultASRChannels = 1
	// DefaultASRMP3Bitrate 标准 MP3 输出码率（64kbps 兼顾 ASR 音质与文件体积）。
	DefaultASRMP3Bitrate = "64k"
	// DefaultFFmpegBinary 默认 ffmpeg 可执行文件名。
	DefaultFFmpegBinary = "ffmpeg"
	// DefaultFFmpegThreads 单次 ffmpeg 进程线程上限，避免多任务并发打满 CPU。
	DefaultFFmpegThreads = 4
)

// FFmpegConverter 基于 ffmpeg 的音频转码器。
type FFmpegConverter struct {
	// BinaryPath ffmpeg 可执行文件路径，空值时使用 DefaultFFmpegBinary。
	BinaryPath string
	// SampleRate 输出采样率，0 时使用 DefaultASRSampleRate。
	SampleRate int
	// Channels 输出声道数，0 时使用 DefaultASRChannels。
	Channels int
	// MP3Bitrate 输出 MP3 码率，空值时使用 DefaultASRMP3Bitrate。
	MP3Bitrate string
}

// NewFFmpegConverter 创建 ffmpeg 转码器，未设置的字段使用 ASR 推荐默认值。
func NewFFmpegConverter(binaryPath string) *FFmpegConverter {
	return &FFmpegConverter{BinaryPath: binaryPath}
}

// ConvertToASRMP3 将输入媒体转为 ASR 适用的标准 MP3（单声道、16kHz、libmp3lame）。
// 通过去视频轨与降采样减小文件体积，同时保持语音识别所需的基本音质。
func (c *FFmpegConverter) ConvertToASRMP3(ctx context.Context, inputPath, outputPath string) error {
	binary := c.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}

	args := buildASRMP3Args(c.resolvedSampleRate(), c.resolvedChannels(), c.resolvedMP3Bitrate(), inputPath, outputPath)
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 转 MP3 失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// buildASRMP3Args 构建 ffmpeg 转 ASR MP3 的参数列表，便于单元测试校验。
func buildASRMP3Args(sampleRate, channels int, bitrate, inputPath, outputPath string) []string {
	return []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-i", inputPath,
		"-vn",
		"-ac", strconv.Itoa(channels),
		"-ar", strconv.Itoa(sampleRate),
		"-c:a", "libmp3lame",
		"-b:a", bitrate,
		outputPath,
	}
}

func (c *FFmpegConverter) resolvedSampleRate() int {
	if c.SampleRate > 0 {
		return c.SampleRate
	}
	return DefaultASRSampleRate
}

func (c *FFmpegConverter) resolvedChannels() int {
	if c.Channels > 0 {
		return c.Channels
	}
	return DefaultASRChannels
}

func (c *FFmpegConverter) resolvedMP3Bitrate() string {
	if strings.TrimSpace(c.MP3Bitrate) != "" {
		return c.MP3Bitrate
	}
	return DefaultASRMP3Bitrate
}

// CutVideoSegment 按起止秒精确裁剪视频片段（重编码，保证切点准确）。
// 参考命令：
//
//	ffmpeg -y -threads 4 -i input.mp4 -ss 10 -to 30 -map 0:v:0 -map 0:a:0? -c:v libx264 -crf 18 -c:a aac -b:a 192k -movflags +faststart output.mp4
//
// startSec / endSec 单位为秒；endSec 须大于 startSec。
func (c *FFmpegConverter) CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	if endSec <= startSec {
		return fmt.Errorf("裁剪时间无效: start=%.3f end=%.3f", startSec, endSec)
	}
	binary := c.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}

	args := buildCutVideoArgs(inputPath, outputPath, startSec, endSec)
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 裁剪视频失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// buildCutVideoArgs 构建精确裁剪参数列表，便于单元测试校验。
// -ss/-to 放在 -i 之后做输出侧精确裁剪；-map 0:a:0? 表示音频轨可选。
func buildCutVideoArgs(inputPath, outputPath string, startSec, endSec float64) []string {
	return []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-i", inputPath,
		"-ss", formatFFmpegSeconds(startSec),
		"-to", formatFFmpegSeconds(endSec),
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", "libx264",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		outputPath,
	}
}

// formatFFmpegSeconds 将秒数格式化为 ffmpeg 可接受的小数秒字符串。
func formatFFmpegSeconds(sec float64) string {
	// 去掉多余尾随 0，避免过长浮点文本。
	return strconv.FormatFloat(sec, 'f', -1, 64)
}
