// Package media 封装直播素材处理所需的本地媒体转码能力。
package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

const (
	// DefaultFFprobeBinary 默认 ffprobe 可执行文件名。
	DefaultFFprobeBinary = "ffprobe"
)

// VideoProber 探测本地媒体文件视频分辨率的抽象，便于单测注入。
type VideoProber interface {
	ProbeVideoSize(ctx context.Context, inputPath string) (width, height int, err error)
}

// MediaTimelineProber 探测媒体时间轴与分辨率，供 ASR 抽音频对齐使用。
type MediaTimelineProber interface {
	VideoProber
	ProbeMediaTimeline(ctx context.Context, inputPath string) (MediaTimeline, error)
}

// MediaTimeline 描述源媒体音视频时间轴与分辨率。
type MediaTimeline struct {
	Width             int
	Height            int
	HasVideo          bool
	HasAudio          bool
	VideoStartSec     float64
	VideoDurationSec  float64
	AudioStartSec     float64
	AudioDurationSec  float64
	FormatDurationSec float64
}

// ASRAlignOptions 将音轨对齐到视频时间轴所需的转码参数。
type ASRAlignOptions struct {
	// LeadPadMs 音轨晚于参考起点时，片头补静音的毫秒数。
	LeadPadMs int64
	// TrimStartSec 音轨早于参考起点时，裁掉的前缀秒数。
	TrimStartSec float64
	// TargetDurSec 输出音频应对齐到的目标时长（秒）；<=0 表示不封口。
	TargetDurSec float64
}

// AlignOptions 根据探测结果计算 ASR MP3 对齐参数。
// 参考起点取视频 start_time（无视频则为 0）；目标时长优先视频 duration，其次容器，再次音频。
func (t MediaTimeline) AlignOptions() ASRAlignOptions {
	refStart := 0.0
	if t.HasVideo {
		refStart = t.VideoStartSec
	}

	targetDur := 0.0
	switch {
	case t.HasVideo && t.VideoDurationSec > 0:
		targetDur = t.VideoDurationSec
	case t.FormatDurationSec > 0:
		targetDur = t.FormatDurationSec
	case t.HasAudio && t.AudioDurationSec > 0:
		targetDur = t.AudioDurationSec
	}

	opts := ASRAlignOptions{TargetDurSec: targetDur}
	if !t.HasAudio {
		return opts
	}

	delta := t.AudioStartSec - refStart
	if delta > 0 {
		opts.LeadPadMs = int64(math.Round(delta * 1000))
	} else if delta < 0 {
		opts.TrimStartSec = -delta
	}
	return opts
}

// FFprobeProber 基于 ffprobe 的媒体探测器。
type FFprobeProber struct {
	// BinaryPath ffprobe 可执行文件路径，空值时使用 DefaultFFprobeBinary。
	BinaryPath string
}

// NewFFprobeProber 创建 ffprobe 探测器。
func NewFFprobeProber(binaryPath string) *FFprobeProber {
	return &FFprobeProber{BinaryPath: binaryPath}
}

func (p *FFprobeProber) binary() string {
	if strings.TrimSpace(p.BinaryPath) == "" {
		return DefaultFFprobeBinary
	}
	return p.BinaryPath
}

// ProbeVideoSize 使用 ffprobe 读取首个视频流的宽高（像素）。
// 无视频流时返回 (0, 0, nil)，调用方可按「可忽略」处理。
func (p *FFprobeProber) ProbeVideoSize(ctx context.Context, inputPath string) (width, height int, err error) {
	tl, err := p.ProbeMediaTimeline(ctx, inputPath)
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe 探测分辨率失败: %w", err)
	}
	return tl.Width, tl.Height, nil
}

// ProbeMediaTimeline 探测音视频 start_time/duration 与分辨率。
func (p *FFprobeProber) ProbeMediaTimeline(ctx context.Context, inputPath string) (MediaTimeline, error) {
	args := buildProbeMediaTimelineArgs(inputPath)
	cmd := exec.CommandContext(ctx, p.binary(), args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return MediaTimeline{}, fmt.Errorf("ffprobe 探测时间轴失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	tl, err := parseProbeMediaTimelineJSON(output)
	if err != nil {
		return MediaTimeline{}, err
	}
	return tl, nil
}

// buildProbeVideoSizeArgs 构建 ffprobe 探测视频宽高的参数列表，便于单元测试校验。
func buildProbeVideoSizeArgs(inputPath string) []string {
	return []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "json",
		inputPath,
	}
}

// buildProbeMediaTimelineArgs 构建一次探测音视频时间轴与分辨率的参数。
func buildProbeMediaTimelineArgs(inputPath string) []string {
	return []string{
		"-v", "error",
		"-show_entries", "stream=codec_type,width,height,start_time,duration:format=duration",
		"-of", "json",
		inputPath,
	}
}

// ffprobeStreamJSON 对应 ffprobe -of json 中的单条 stream。
type ffprobeStreamJSON struct {
	CodecType string `json:"codec_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	StartTime string `json:"start_time"`
	Duration  string `json:"duration"`
}

// ffprobeFormatJSON 对应 format 段。
type ffprobeFormatJSON struct {
	Duration string `json:"duration"`
}

// ffprobeOutputJSON 对应 ffprobe -of json 顶层结构。
type ffprobeOutputJSON struct {
	Streams []ffprobeStreamJSON `json:"streams"`
	Format  ffprobeFormatJSON   `json:"format"`
}

// parseProbeVideoSizeJSON 解析 ffprobe JSON，取首个视频流宽高；无流时返回 (0,0,nil)。
func parseProbeVideoSizeJSON(raw []byte) (width, height int, err error) {
	var out ffprobeOutputJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, 0, fmt.Errorf("解析 ffprobe JSON 失败: %w", err)
	}
	for _, s := range out.Streams {
		if s.CodecType != "" && s.CodecType != "video" {
			continue
		}
		if s.Width > 0 || s.Height > 0 {
			if s.Width < 0 || s.Height < 0 {
				return 0, 0, fmt.Errorf("ffprobe 返回非法分辨率: %dx%d", s.Width, s.Height)
			}
			return s.Width, s.Height, nil
		}
	}
	if len(out.Streams) == 0 {
		return 0, 0, nil
	}
	// 兼容仅含 width/height、无 codec_type 的旧探测 JSON。
	w, h := out.Streams[0].Width, out.Streams[0].Height
	if w < 0 || h < 0 {
		return 0, 0, fmt.Errorf("ffprobe 返回非法分辨率: %dx%d", w, h)
	}
	return w, h, nil
}

func parseProbeMediaTimelineJSON(raw []byte) (MediaTimeline, error) {
	var out ffprobeOutputJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return MediaTimeline{}, fmt.Errorf("解析 ffprobe JSON 失败: %w", err)
	}

	tl := MediaTimeline{}
	formatDur, err := parseFlexibleFloat(out.Format.Duration)
	if err != nil {
		return MediaTimeline{}, fmt.Errorf("解析 format.duration 失败: %w", err)
	}
	tl.FormatDurationSec = formatDur

	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			if tl.HasVideo {
				continue
			}
			start, err := parseFlexibleFloat(s.StartTime)
			if err != nil {
				return MediaTimeline{}, fmt.Errorf("解析视频 start_time 失败: %w", err)
			}
			dur, err := parseFlexibleFloat(s.Duration)
			if err != nil {
				return MediaTimeline{}, fmt.Errorf("解析视频 duration 失败: %w", err)
			}
			if s.Width < 0 || s.Height < 0 {
				return MediaTimeline{}, fmt.Errorf("ffprobe 返回非法分辨率: %dx%d", s.Width, s.Height)
			}
			tl.HasVideo = true
			tl.VideoStartSec = start
			tl.VideoDurationSec = dur
			tl.Width = s.Width
			tl.Height = s.Height
		case "audio":
			if tl.HasAudio {
				continue
			}
			start, err := parseFlexibleFloat(s.StartTime)
			if err != nil {
				return MediaTimeline{}, fmt.Errorf("解析音频 start_time 失败: %w", err)
			}
			dur, err := parseFlexibleFloat(s.Duration)
			if err != nil {
				return MediaTimeline{}, fmt.Errorf("解析音频 duration 失败: %w", err)
			}
			tl.HasAudio = true
			tl.AudioStartSec = start
			tl.AudioDurationSec = dur
		}
	}
	return tl, nil
}

// parseFlexibleFloat 解析 ffprobe 数值字段；空/"N/A" 视为 0。
func parseFlexibleFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("非法浮点值 %q", s)
	}
	return v, nil
}

// ProbeDurationSec 仅读取容器/音频时长（秒），优先 format.duration。
func (p *FFprobeProber) ProbeDurationSec(ctx context.Context, inputPath string) (float64, error) {
	tl, err := p.ProbeMediaTimeline(ctx, inputPath)
	if err != nil {
		return 0, err
	}
	if tl.FormatDurationSec > 0 {
		return tl.FormatDurationSec, nil
	}
	if tl.AudioDurationSec > 0 {
		return tl.AudioDurationSec, nil
	}
	if tl.VideoDurationSec > 0 {
		return tl.VideoDurationSec, nil
	}
	return 0, nil
}
