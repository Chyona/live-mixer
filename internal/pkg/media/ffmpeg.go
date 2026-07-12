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
	// DefaultASRChannels 单声道可显著减小 WAV 体积，且满足语音识别场景。
	DefaultASRChannels = 1
	// DefaultFFmpegBinary 默认 ffmpeg 可执行文件名。
	DefaultFFmpegBinary = "ffmpeg"
)

// FFmpegConverter 基于 ffmpeg 的音频转码器。
type FFmpegConverter struct {
	// BinaryPath ffmpeg 可执行文件路径，空值时使用 DefaultFFmpegBinary。
	BinaryPath string
	// SampleRate 输出 WAV 采样率，0 时使用 DefaultASRSampleRate。
	SampleRate int
	// Channels 输出声道数，0 时使用 DefaultASRChannels。
	Channels int
}

// NewFFmpegConverter 创建 ffmpeg 转码器，未设置的字段使用 ASR 推荐默认值。
func NewFFmpegConverter(binaryPath string) *FFmpegConverter {
	return &FFmpegConverter{BinaryPath: binaryPath}
}

// ConvertToASRWAV 将输入媒体转为 ASR 适用的 PCM WAV（单声道 16bit）。
// 通过降采样与去视频轨减小文件体积，同时保持语音识别所需的基本音质。
func (c *FFmpegConverter) ConvertToASRWAV(ctx context.Context, inputPath, outputPath string) error {
	binary := c.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}

	args := buildASRWAVArgs(c.resolvedSampleRate(), c.resolvedChannels(), inputPath, outputPath)
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 转 WAV 失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// buildASRWAVArgs 构建 ffmpeg 转 ASR WAV 的参数列表，便于单元测试校验。
func buildASRWAVArgs(sampleRate, channels int, inputPath, outputPath string) []string {
	return []string{
		"-y",
		"-i", inputPath,
		"-vn",
		"-ac", strconv.Itoa(channels),
		"-ar", strconv.Itoa(sampleRate),
		"-c:a", "pcm_s16le",
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
