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
	DefaultFFmpegThreads = 6
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
// 无对齐参数时等价于 Align 零值（不裁片头、不补片头、不按目标时长封口）。
func (c *FFmpegConverter) ConvertToASRMP3(ctx context.Context, inputPath, outputPath string) error {
	return c.ConvertToASRMP3Aligned(ctx, inputPath, outputPath, ASRAlignOptions{})
}

// ConvertToASRMP3Aligned 在标准 MP3 转码基础上，将音轨对齐到视频时间轴。
// LeadPadMs>0 时片头补静音；TrimStartSec>0 时裁掉音轨前缀；TargetDurSec>0 时 apad+-t 封口到目标时长。
func (c *FFmpegConverter) ConvertToASRMP3Aligned(ctx context.Context, inputPath, outputPath string, align ASRAlignOptions) error {
	binary := c.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}

	args := buildASRMP3Args(c.resolvedSampleRate(), c.resolvedChannels(), c.resolvedMP3Bitrate(), inputPath, outputPath, align)
	args = prependHLSInputArgs(args, inputPath)
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 转 MP3 失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// buildASRMP3Args 构建 ffmpeg 转 ASR MP3 的参数列表，便于单元测试校验。
func buildASRMP3Args(sampleRate, channels int, bitrate, inputPath, outputPath string, align ASRAlignOptions) []string {
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-i", inputPath,
		"-vn",
		"-af", buildASRAlignAudioFilter(sampleRate, channels, align),
		"-c:a", "libmp3lame",
		"-b:a", bitrate,
	}
	if align.TargetDurSec > 0 {
		args = append(args, "-t", formatFFmpegSeconds(align.TargetDurSec))
	}
	args = append(args, outputPath)
	return args
}

// buildASRAlignAudioFilter 构建将对齐到视频时间轴的 -af 滤镜链。
// 始终 asetpts 归零，避免源片 audio.start_time>0 时 apad/whole_dur 按错误时间戳提前结束。
func buildASRAlignAudioFilter(sampleRate, channels int, align ASRAlignOptions) string {
	parts := make([]string, 0, 6)
	if align.TrimStartSec > 0 {
		parts = append(parts, fmt.Sprintf("atrim=start=%s", formatFFmpegSeconds(align.TrimStartSec)))
	}
	parts = append(parts, "asetpts=PTS-STARTPTS")
	if align.LeadPadMs > 0 {
		// delays=N:all=1 对任意声道数片头补静音，使内容落在视频时间轴上。
		parts = append(parts, fmt.Sprintf("adelay=delays=%d:all=1", align.LeadPadMs))
	}
	parts = append(parts, fmt.Sprintf("aformat=sample_rates=%d:channel_layouts=%s", sampleRate, channelLayoutName(channels)))
	if align.TargetDurSec > 0 {
		parts = append(parts, fmt.Sprintf("apad=whole_dur=%s", formatFFmpegSeconds(align.TargetDurSec)))
	}
	return strings.Join(parts, ",")
}

func channelLayoutName(channels int) string {
	switch channels {
	case 1:
		return "mono"
	case 2:
		return "stereo"
	default:
		return fmt.Sprintf("%d", channels)
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

// AudioLoudness 描述 volumedetect 测得的音量（单位 dB）。
type AudioLoudness struct {
	MeanVolumeDB float64
	MaxVolumeDB  float64
}

// IsNearSilence 判断是否接近数字静音（ASR 会返回 20000003）。
// 真实人声直播 max_volume 通常远高于 -40dB；纯静音约为 -91dB。
func (l AudioLoudness) IsNearSilence() bool {
	return l.MaxVolumeDB < -40
}

// ProbeAudioLoudness 使用 ffmpeg volumedetect 探测音频响度。
func (c *FFmpegConverter) ProbeAudioLoudness(ctx context.Context, inputPath string) (AudioLoudness, error) {
	binary := c.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}
	args := []string{
		"-hide_banner",
		"-i", inputPath,
		"-af", "volumedetect",
		"-f", "null",
		"-",
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	// volumedetect 写在 stderr；部分环境下退出码非 0 但仍有有效统计。
	loud, parseErr := parseVolumeDetectOutput(string(output))
	if parseErr != nil {
		if err != nil {
			return AudioLoudness{}, fmt.Errorf("探测音频响度失败: %w, output: %s", err, strings.TrimSpace(string(output)))
		}
		return AudioLoudness{}, parseErr
	}
	return loud, nil
}

func parseVolumeDetectOutput(raw string) (AudioLoudness, error) {
	var (
		mean, max float64
		gotMean   bool
		gotMax    bool
	)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := parseVolumeDetectField(line, "mean_volume:"); ok {
			mean = v
			gotMean = true
		}
		if v, ok := parseVolumeDetectField(line, "max_volume:"); ok {
			max = v
			gotMax = true
		}
	}
	if !gotMean || !gotMax {
		return AudioLoudness{}, fmt.Errorf("未解析到 volumedetect 结果")
	}
	return AudioLoudness{MeanVolumeDB: mean, MaxVolumeDB: max}, nil
}

func parseVolumeDetectField(line, key string) (float64, bool) {
	idx := strings.Index(strings.ToLower(line), key)
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(line[idx+len(key):])
	rest = strings.TrimSpace(strings.TrimSuffix(rest, "dB"))
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// CutVideoSegment 按起止秒裁剪视频片段（重编码；混合 -ss 粗定位 + 精定位）。
// 参考命令（起点 ≥15s）：
//
//	ffmpeg -y -threads 6 -ss 85 -i input.mp4 -ss 15 -t 20 -map 0:v:0 -map 0:a:0? -c:v libx264 -crf 18 -c:a aac -b:a 192k -movflags +faststart output.mp4
//
// startSec / endSec 单位为秒；endSec 须大于 startSec。
func (c *FFmpegConverter) CutVideoSegment(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	return c.runCutVideo(ctx, inputPath, outputPath, startSec, endSec, false)
}

// CutVideoSegmentFast 按起止秒快速裁剪（-c copy，对齐到关键帧，不重编码）。
// 参考命令：
//
//	ffmpeg -y -threads 6 -ss 10 -i input.mp4 -t 20 -map 0:v:0 -map 0:a:0? -c copy -avoid_negative_ts make_zero -movflags +faststart output.mp4
func (c *FFmpegConverter) CutVideoSegmentFast(ctx context.Context, inputPath, outputPath string, startSec, endSec float64) error {
	return c.runCutVideo(ctx, inputPath, outputPath, startSec, endSec, true)
}

func (c *FFmpegConverter) runCutVideo(ctx context.Context, inputPath, outputPath string, startSec, endSec float64, fast bool) error {
	if endSec <= startSec {
		return fmt.Errorf("裁剪时间无效: start=%.3f end=%.3f", startSec, endSec)
	}
	binary := c.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}

	var args []string
	if fast {
		args = buildCutVideoFastArgs(inputPath, outputPath, startSec, endSec)
	} else {
		args = buildCutVideoArgs(inputPath, outputPath, startSec, endSec)
	}
	args = prependHLSInputArgs(args, inputPath)
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if fast {
			return fmt.Errorf("ffmpeg 快速裁剪视频失败: %w, output: %s", err, strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("ffmpeg 裁剪视频失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// preciseCutCoarseBackSec 精确裁剪时先按输入 -ss 回退的秒数，再在解码侧微调。
// 对 HLS/远程源：避免「纯 -ss 在 -i 之后」从片头解码到起点（草稿极慢）；
// 对本地文件：同样用混合定位，兼顾速度与帧精度。
const preciseCutCoarseBackSec = 15.0

// buildCutVideoArgs 构建精确裁剪参数列表，便于单元测试校验。
// 采用「输入粗定位 + 输出精定位」：
//   - 先 -ss (start-15s) -i … 快速靠近目标（HLS 友好）；
//   - 再 -ss 15 -t dur 解码侧落到精确起点并重编码。
//
// start < 15s 时仍用 -i 后 -ss，从文件头精确解码（成本可接受）。
// -map 0:a:0? 表示音频轨可选。
func buildCutVideoArgs(inputPath, outputPath string, startSec, endSec float64) []string {
	dur := endSec - startSec
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
	}
	if startSec >= preciseCutCoarseBackSec {
		args = append(args,
			"-ss", formatFFmpegSeconds(startSec-preciseCutCoarseBackSec),
			"-i", inputPath,
			"-ss", formatFFmpegSeconds(preciseCutCoarseBackSec),
		)
	} else {
		args = append(args,
			"-i", inputPath,
			"-ss", formatFFmpegSeconds(startSec),
		)
	}
	return append(args,
		"-t", formatFFmpegSeconds(dur),
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", "libx264",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		outputPath,
	)
}

// buildCutVideoFastArgs 构建关键帧快速裁剪参数：流拷贝、不重编码。
// -ss 在 -i 之前按关键帧定位；-c copy 直接拷贝音视频包。
func buildCutVideoFastArgs(inputPath, outputPath string, startSec, endSec float64) []string {
	return []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-ss", formatFFmpegSeconds(startSec),
		"-i", inputPath,
		"-t", formatFFmpegSeconds(endSec - startSec),
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		outputPath,
	}
}

// formatFFmpegSeconds 将秒数格式化为 ffmpeg 可接受的小数秒字符串。
func formatFFmpegSeconds(sec float64) string {
	// 去掉多余尾随 0，避免过长浮点文本。
	return strconv.FormatFloat(sec, 'f', -1, 64)
}
