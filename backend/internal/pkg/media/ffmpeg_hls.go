package media

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	hlsReconnectDelayMax = 2
	segmentWatchInterval = 800 * time.Millisecond
)

// HLSInputArgs 在 -i 之前插入的 HLS/HTTP 参数。
// 本地 m3u8（如 seal 后的 source_vod.m3u8）分片常为 https，必须放宽 protocol_whitelist，
// 否则 ffmpeg 默认只允许 file,crypto,data，会报 Protocol 'https' not on whitelist。
func HLSInputArgs(input string) []string {
	if !IsHTTPURL(input) && !IsM3U8URL(input) {
		return nil
	}
	args := []string{
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto",
	}
	if IsHTTPURL(input) {
		args = append(args,
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_on_network_error", "1",
			"-rw_timeout", "15000000",
		)
	}
	return args
}

func prependHLSInputArgs(args []string, input string) []string {
	extra := HLSInputArgs(input)
	if len(extra) == 0 {
		return args
	}
	out := make([]string, 0, len(args)+len(extra))
	inserted := false
	for i := 0; i < len(args); i++ {
		if !inserted && args[i] == "-i" {
			out = append(out, extra...)
			inserted = true
		}
		out = append(out, args[i])
	}
	return out
}

func (c *FFmpegConverter) ffmpegBinary() string {
	if strings.TrimSpace(c.BinaryPath) == "" {
		return DefaultFFmpegBinary
	}
	return c.BinaryPath
}

func (c *FFmpegConverter) runFFmpeg(ctx context.Context, args []string, errPrefix string) error {
	cmd := exec.CommandContext(ctx, c.ffmpegBinary(), args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if len(msg) > 1200 {
			msg = msg[:1200]
		}
		return fmt.Errorf("%s: %w, output: %s", errPrefix, err, msg)
	}
	return nil
}

// ConvertURLToASRMP3 从本地文件或 HLS/HTTP URL 抽 ASR MP3（不对齐）。
func (c *FFmpegConverter) ConvertURLToASRMP3(ctx context.Context, input, outputPath string) error {
	args := buildASRMP3Args(c.resolvedSampleRate(), c.resolvedChannels(), c.resolvedMP3Bitrate(), input, outputPath, ASRAlignOptions{})
	args = prependHLSInputArgs(args, input)
	return c.runFFmpeg(ctx, args, "ffmpeg 从源抽 ASR MP3 失败")
}

// ConcatMediaFiles 按清单无损拼接分片为 mp4。
func (c *FFmpegConverter) ConcatMediaFiles(ctx context.Context, files []string, outputPath string) error {
	if len(files) == 0 {
		return fmt.Errorf("没有可拼接的分片")
	}
	listPath := outputPath + ".concat.txt"
	var b strings.Builder
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			abs = f
		}
		escaped := strings.ReplaceAll(abs, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `'\''`)
		b.WriteString("file '")
		b.WriteString(escaped)
		b.WriteString("'\n")
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("写入 concat 清单失败: %w", err)
	}
	defer os.Remove(listPath)

	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c", "copy",
		"-movflags", "+faststart",
		outputPath,
	}
	if err := c.runFFmpeg(ctx, args, "ffmpeg 拼接分片失败"); err != nil {
		// TS 拼接成 mp4 偶发 copy 失败时回退重编码。
		args = []string{
			"-y",
			"-threads", strconv.Itoa(DefaultFFmpegThreads),
			"-f", "concat",
			"-safe", "0",
			"-i", listPath,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "18",
			"-c:a", "aac",
			"-b:a", "192k",
			"-movflags", "+faststart",
			outputPath,
		}
		return c.runFFmpeg(ctx, args, "ffmpeg 拼接分片（重编码）失败")
	}
	return nil
}

// RecordHLSSegments 将 HLS 拉流写成固定时长 TS 分片。
// onComplete 在每个「已写完」的分片上回调（不含当前仍在写入的最后一个文件，直至进程退出）。
func (c *FFmpegConverter) RecordHLSSegments(
	ctx context.Context,
	inputURL, outDir string,
	startIndex int,
	segmentSec int,
	onComplete func(index int, path string),
) error {
	if segmentSec <= 0 {
		segmentSec = 6
	}
	if startIndex < 0 {
		startIndex = 0
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("创建录像目录失败: %w", err)
	}
	pattern := filepath.Join(outDir, "seg_%05d.ts")
	args := buildRecordHLSSegmentArgs(inputURL, pattern, startIndex, segmentSec)

	cmd := exec.CommandContext(ctx, c.ffmpegBinary(), args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 录像失败: %w", err)
	}

	reported := map[int]struct{}{}
	stopWatch := make(chan struct{})
	go func() {
		ticker := time.NewTicker(segmentWatchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.emitCompletedSegments(outDir, startIndex, reported, onComplete, false)
			}
		}
	}()

	errBuf := &strings.Builder{}
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			line := sc.Text()
			if errBuf.Len() < 800 {
				errBuf.WriteString(line)
				errBuf.WriteByte('\n')
			}
		}
	}()

	waitErr := cmd.Wait()
	close(stopWatch)
	c.emitCompletedSegments(outDir, startIndex, reported, onComplete, true)
	if waitErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(errBuf.String())
		return fmt.Errorf("ffmpeg 录像退出: %w %s", waitErr, msg)
	}
	return nil
}

// buildRecordHLSSegmentArgs 生成边录边播用的 TS 分片参数。
// 不重置每片 PTS，避免浏览器 MSE 因时间戳回绕解码失败；续录时用 output_ts_offset 接上一段。
func buildRecordHLSSegmentArgs(inputURL, pattern string, startIndex, segmentSec int) []string {
	if segmentSec <= 0 {
		segmentSec = 6
	}
	if startIndex < 0 {
		startIndex = 0
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	args = append(args, HLSInputArgs(inputURL)...)
	args = append(args,
		"-i", inputURL,
		"-c", "copy",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-f", "segment",
		"-segment_time", strconv.Itoa(segmentSec),
		"-segment_format", "mpegts",
		"-segment_start_number", strconv.Itoa(startIndex),
	)
	if startIndex > 0 {
		args = append(args, "-output_ts_offset", strconv.Itoa(startIndex*segmentSec))
	}
	return append(args, pattern)
}

func (c *FFmpegConverter) emitCompletedSegments(outDir string, startIndex int, reported map[int]struct{}, onComplete func(int, string), includeLast bool) {
	if onComplete == nil {
		return
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "seg_*.ts"))
	if err != nil || len(matches) == 0 {
		return
	}
	sort.Strings(matches)
	limit := len(matches)
	if !includeLast && limit > 0 {
		limit--
	}
	for _, p := range matches[:limit] {
		idx, ok := parseSegIndex(filepath.Base(p))
		if !ok || idx < startIndex {
			continue
		}
		if _, done := reported[idx]; done {
			continue
		}
		info, statErr := os.Stat(p)
		if statErr != nil || info.Size() == 0 {
			continue
		}
		reported[idx] = struct{}{}
		onComplete(idx, p)
	}
}

func parseSegIndex(name string) (int, bool) {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(name, "_")
	if len(parts) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ProbeHLSHasMedia 用 ffmpeg 尝试读取约 1 秒，确认能解出媒体包。
func (c *FFmpegConverter) ProbeHLSHasMedia(ctx context.Context, inputURL string) error {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, HLSInputArgs(inputURL)...)
	args = append(args, "-t", "1", "-i", inputURL, "-f", "null", "-")
	return c.runFFmpeg(ctx, args, "ffmpeg 探测 HLS 失败")
}
