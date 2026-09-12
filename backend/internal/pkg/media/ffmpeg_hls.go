package media

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

// ConcatDemuxerInputArgs 本地 ffconcat 清单需在 -i 前声明 demuxer。
func ConcatDemuxerInputArgs(input string) []string {
	lower := strings.ToLower(strings.TrimSpace(input))
	if strings.HasSuffix(lower, ".ffconcat") || strings.HasSuffix(lower, ".concat.txt") {
		return []string{"-f", "concat", "-safe", "0"}
	}
	return nil
}

func prependHLSInputArgs(args []string, input string) []string {
	extra := append(ConcatDemuxerInputArgs(input), HLSInputArgs(input)...)
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

// ConcatMediaFiles 按清单拼接分片为 mp4。
// 先 concat copy，再 copy 视频并重编码音轨（aresample=async + atrim），避免 TS 拼接后
// 「解码 PCM 比视频长一截」——后续从该 MP4 抽等长 ASR MP3 的前提。
// 注意：仅适用于 TS/毛时间戳分片；已封装好的 window MP4 请用 ConcatMP4ContinuousTimeline。
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

	copyPath := outputPath + ".copy.mp4"
	defer os.Remove(copyPath)
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c", "copy",
		"-movflags", "+faststart",
		copyPath,
	}
	if err := c.runFFmpeg(ctx, args, "ffmpeg 拼接分片失败"); err != nil {
		// TS 拼接成 mp4 偶发 copy 失败时回退整段重编码。
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
	if err := c.remuxCopyVideoSyncAudio(ctx, copyPath, outputPath); err != nil {
		return fmt.Errorf("拼接后对齐音轨失败: %w", err)
	}
	return nil
}

// ConcatMP4ContinuousTimeline 将已封装好的 MP4（如 window_N）按内容解码后拼成一条从 0 起的连续时间轴。
// 与 ConcatMediaFiles 不同：不做 bitstream copy + aresample=async，避免多 MP4 接缝把音轨拉长。
// 音视频均重编码；拼接后以视频时长为权威，音轨 apad/atrim 贴齐。
func (c *FFmpegConverter) ConcatMP4ContinuousTimeline(ctx context.Context, files []string, outputPath string) error {
	if len(files) == 0 {
		return fmt.Errorf("没有可拼接的 MP4")
	}
	if len(files) == 1 {
		return copyMediaFile(files[0], outputPath)
	}
	for i, f := range files {
		if st, err := os.Stat(f); err != nil || st.Size() == 0 {
			return fmt.Errorf("拼接输入[%d]无效: %s", i, f)
		}
	}

	args := buildConcatMP4ContinuousArgs(files, outputPath)
	tmpOut := outputPath + ".timeline.mp4"
	args[len(args)-1] = tmpOut
	_ = os.Remove(tmpOut)
	if err := c.runFFmpeg(ctx, args, "ffmpeg 连续时间轴拼接失败"); err != nil {
		_ = os.Remove(tmpOut)
		return err
	}
	if err := c.alignAudioToVideoDuration(ctx, tmpOut, outputPath); err != nil {
		_ = os.Remove(tmpOut)
		return err
	}
	_ = os.Remove(tmpOut)
	return nil
}

// buildConcatMP4ContinuousArgs 构造 filter_complex concat 参数（输出路径为最后一项）。
func buildConcatMP4ContinuousArgs(files []string, outputPath string) []string {
	n := len(files)
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
	}
	for _, f := range files {
		args = append(args, "-i", f)
	}
	var fc strings.Builder
	for i := 0; i < n; i++ {
		fc.WriteString(fmt.Sprintf("[%d:v:0][%d:a:0]", i, i))
	}
	fc.WriteString(fmt.Sprintf("concat=n=%d:v=1:a=1[v][a]", n))
	args = append(args,
		"-filter_complex", fc.String(),
		"-map", "[v]",
		"-map", "[a]",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		outputPath,
	)
	return args
}

// alignAudioToVideoDuration 以视频时长为权威，重编码音轨并硬裁/补齐到等长。
func (c *FFmpegConverter) alignAudioToVideoDuration(ctx context.Context, inputPath, outputPath string) error {
	prober := NewFFprobeProber("")
	tl, err := prober.ProbeMediaTimeline(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("探测连续轴拼接结果失败: %w", err)
	}
	durSec := tl.VideoDurationSec
	if durSec <= 0 {
		durSec = tl.FormatDurationSec
	}
	if durSec <= 0 {
		return fmt.Errorf("连续轴拼接结果视频时长无效")
	}
	dur := formatFFmpegSeconds(durSec)
	af := fmt.Sprintf(
		"aresample=async=%d:first_pts=0,asetpts=PTS-STARTPTS,apad=whole_dur=%s,atrim=duration=%s,asetpts=PTS-STARTPTS",
		asrAudioAsyncMaxSamplesPerSec, dur, dur,
	)
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-i", inputPath,
		"-c:v", "copy",
		"-af", af,
		"-c:a", "aac",
		"-b:a", "192k",
		"-t", dur,
		"-movflags", "+faststart",
		outputPath,
	}
	return c.runFFmpeg(ctx, args, "ffmpeg 按视频轴对齐音轨失败")
}

func copyMediaFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// remuxCopyVideoSyncAudio 保留视频比特流，按容器时间轴重编码音轨并硬裁到等长。
func (c *FFmpegConverter) remuxCopyVideoSyncAudio(ctx context.Context, inputPath, outputPath string) error {
	prober := NewFFprobeProber("")
	durSec, err := prober.ProbeDurationSec(ctx, inputPath)
	if err != nil || durSec <= 0 {
		tl, perr := prober.ProbeMediaTimeline(ctx, inputPath)
		if perr != nil {
			return fmt.Errorf("探测拼接结果时长失败: %w", err)
		}
		durSec = tl.FormatDurationSec
		if durSec <= 0 && tl.HasVideo {
			durSec = tl.VideoDurationSec
		}
		if durSec <= 0 {
			return fmt.Errorf("拼接结果时长无效")
		}
	}
	dur := formatFFmpegSeconds(durSec)
	af := fmt.Sprintf(
		"aresample=async=%d:first_pts=0,asetpts=PTS-STARTPTS,apad=whole_dur=%s,atrim=duration=%s,asetpts=PTS-STARTPTS",
		asrAudioAsyncMaxSamplesPerSec, dur, dur,
	)
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-i", inputPath,
		"-c:v", "copy",
		"-af", af,
		"-c:a", "aac",
		"-b:a", "192k",
		"-t", dur,
		"-movflags", "+faststart",
		outputPath,
	}
	return c.runFFmpeg(ctx, args, "ffmpeg 对齐音轨失败")
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

// RemuxToMPEGTS 将本地媒体（如窗 MP4）无损/低损 remux 为 MPEG-TS，供 HLS 预览与 MP4 同源。
func (c *FFmpegConverter) RemuxToMPEGTS(ctx context.Context, inputPath, outputPath string) error {
	args := []string{
		"-y",
		"-threads", strconv.Itoa(DefaultFFmpegThreads),
		"-i", inputPath,
		"-c", "copy",
		"-bsf:v", "h264_mp4toannexb",
		"-f", "mpegts",
		outputPath,
	}
	if err := c.runFFmpeg(ctx, args, "ffmpeg remux MPEG-TS 失败"); err != nil {
		// 部分封装无 annexb 比特流滤镜时回退重编码。
		args = []string{
			"-y",
			"-threads", strconv.Itoa(DefaultFFmpegThreads),
			"-i", inputPath,
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "18",
			"-c:a", "aac",
			"-b:a", "192k",
			"-f", "mpegts",
			outputPath,
		}
		return c.runFFmpeg(ctx, args, "ffmpeg remux MPEG-TS（重编码）失败")
	}
	return nil
}
