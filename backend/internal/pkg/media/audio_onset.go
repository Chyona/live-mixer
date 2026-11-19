package media

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// OnsetSampleRate 片头 onset 校验用采样率。
	OnsetSampleRate = 16000
)

// ExtractMonoPCM16Window 从媒体抽出 [startSec, startSec+durSec) 的 mono s16le raw PCM。
func ExtractMonoPCM16Window(ctx context.Context, binary, inputPath, outRaw string, startSec, durSec float64, sampleRate int) error {
	if strings.TrimSpace(binary) == "" {
		binary = DefaultFFmpegBinary
	}
	if sampleRate <= 0 {
		sampleRate = OnsetSampleRate
	}
	if durSec <= 0 {
		return fmt.Errorf("无效抽音时长: %.3f", durSec)
	}
	if err := os.MkdirAll(filepath.Dir(outRaw), 0o755); err != nil {
		return err
	}
	args := []string{
		"-y",
		"-threads", "2",
	}
	// concat/m3u8 禁止输入侧 -ss；单文件可用输入侧快速定位。
	if requiresOutputSideSeekOnly(inputPath) {
		args = append(args,
			"-i", inputPath,
			"-ss", formatFFmpegSeconds(startSec),
		)
	} else {
		args = append(args,
			"-ss", formatFFmpegSeconds(startSec),
			"-i", inputPath,
		)
	}
	args = append(args,
		"-t", formatFFmpegSeconds(durSec),
		"-vn",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-f", "s16le",
		outRaw,
	)
	args = prependHLSInputArgs(args, inputPath)
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 抽音失败: %w, output: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReadPCM16File 读取 s16le mono raw。
func ReadPCM16File(path string) ([]int16, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 2 {
		return nil, fmt.Errorf("pcm 过短")
	}
	n := len(b) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out, nil
}

// OnsetShiftMS 归一化互相关：clip 相对 ref 的滞后（正值=clip 内容晚于 ref，即裁切起点偏早）。
// maxLagMS 限制搜索窗；返回最佳 lag 毫秒。
func OnsetShiftMS(ref, clip []int16, sampleRate, maxLagMS int) (shiftMS int64, ok bool) {
	if sampleRate <= 0 || len(ref) < 32 || len(clip) < 32 {
		return 0, false
	}
	maxLag := maxLagMS * sampleRate / 1000
	if maxLag < 1 {
		maxLag = sampleRate / 10 // 默认 ±100ms
	}
	// 取较短长度对齐比较。
	n := len(ref)
	if len(clip) < n {
		n = len(clip)
	}
	if n < 32 {
		return 0, false
	}
	refF := toFloatNorm(ref[:n])
	clipF := toFloatNorm(clip[:n])
	bestLag := 0
	bestScore := math.Inf(-1)
	for lag := -maxLag; lag <= maxLag; lag++ {
		score := nccAtLag(refF, clipF, lag)
		if score > bestScore {
			bestScore = score
			bestLag = lag
		}
	}
	if bestScore < 0.15 {
		// 相关过弱：不可信
		return 0, false
	}
	shiftMS = int64(bestLag) * 1000 / int64(sampleRate)
	return shiftMS, true
}

func toFloatNorm(in []int16) []float64 {
	out := make([]float64, len(in))
	var sum float64
	for i, v := range in {
		out[i] = float64(v)
		sum += out[i]
	}
	mean := sum / float64(len(in))
	var energy float64
	for i := range out {
		out[i] -= mean
		energy += out[i] * out[i]
	}
	if energy > 0 {
		norm := math.Sqrt(energy)
		for i := range out {
			out[i] /= norm
		}
	}
	return out
}

func nccAtLag(ref, clip []float64, lag int) float64 {
	n := len(ref)
	var sum float64
	var count int
	for i := 0; i < n; i++ {
		j := i + lag
		if j < 0 || j >= n {
			continue
		}
		sum += ref[i] * clip[j]
		count++
	}
	if count < n/4 {
		return math.Inf(-1)
	}
	return sum
}
