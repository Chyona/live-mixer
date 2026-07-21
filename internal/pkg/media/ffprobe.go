package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// FFprobeProber 基于 ffprobe 的视频分辨率探测器。
type FFprobeProber struct {
	// BinaryPath ffprobe 可执行文件路径，空值时使用 DefaultFFprobeBinary。
	BinaryPath string
}

// NewFFprobeProber 创建 ffprobe 探测器。
func NewFFprobeProber(binaryPath string) *FFprobeProber {
	return &FFprobeProber{BinaryPath: binaryPath}
}

// ProbeVideoSize 使用 ffprobe 读取首个视频流的宽高（像素）。
// 无视频流时返回 (0, 0, nil)，调用方可按「可忽略」处理。
func (p *FFprobeProber) ProbeVideoSize(ctx context.Context, inputPath string) (width, height int, err error) {
	binary := p.BinaryPath
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFprobeBinary
	}
	args := buildProbeVideoSizeArgs(inputPath)
	cmd := exec.CommandContext(ctx, binary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe 探测分辨率失败: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return parseProbeVideoSizeJSON(output)
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

// ffprobeStreamJSON 对应 ffprobe -of json 中的单条 stream。
type ffprobeStreamJSON struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ffprobeOutputJSON 对应 ffprobe -of json 顶层结构。
type ffprobeOutputJSON struct {
	Streams []ffprobeStreamJSON `json:"streams"`
}

// parseProbeVideoSizeJSON 解析 ffprobe JSON，取首个视频流宽高；无流时返回 (0,0,nil)。
func parseProbeVideoSizeJSON(raw []byte) (width, height int, err error) {
	var out ffprobeOutputJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, 0, fmt.Errorf("解析 ffprobe JSON 失败: %w", err)
	}
	if len(out.Streams) == 0 {
		return 0, 0, nil
	}
	w, h := out.Streams[0].Width, out.Streams[0].Height
	if w < 0 || h < 0 {
		return 0, 0, fmt.Errorf("ffprobe 返回非法分辨率: %dx%d", w, h)
	}
	return w, h, nil
}
