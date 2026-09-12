package media

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRecordHLSSegmentArgs_NoResetTimestamps(t *testing.T) {
	args := buildRecordHLSSegmentArgs("https://ex.example/live.m3u8", "seg_%05d.ts", 0, 6)
	for i, a := range args {
		if a == "-reset_timestamps" {
			t.Fatalf("reset_timestamps should not be set at %d: %v", i, args)
		}
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-segment_format mpegts") {
		t.Fatalf("missing mpegts segment format: %v", args)
	}
	if strings.Contains(joined, "-output_ts_offset") {
		t.Fatalf("first session should not offset timestamps: %v", args)
	}
}

func TestBuildRecordHLSSegmentArgs_ResumeOffset(t *testing.T) {
	args := buildRecordHLSSegmentArgs("https://ex.example/live.m3u8", "seg_%05d.ts", 10, 6)
	found := false
	for i, a := range args {
		if a == "-output_ts_offset" && i+1 < len(args) && args[i+1] == "60" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing output_ts_offset 60 in %v", args)
	}
}

func TestParseVolumeDetectOutput(t *testing.T) {
	raw := `
[Parsed_volumedetect_0 @ 0x0] n_samples: 1000
[Parsed_volumedetect_0 @ 0x0] mean_volume: -18.5 dB
[Parsed_volumedetect_0 @ 0x0] max_volume: -3.2 dB
`
	got, err := parseVolumeDetectOutput(raw)
	if err != nil {
		t.Fatalf("parseVolumeDetectOutput() error = %v", err)
	}
	if got.MeanVolumeDB != -18.5 || got.MaxVolumeDB != -3.2 {
		t.Fatalf("got = %+v", got)
	}
	if got.IsNearSilence() {
		t.Fatal("expected not near silence")
	}
	silent := AudioLoudness{MeanVolumeDB: -91, MaxVolumeDB: -91}
	if !silent.IsNearSilence() {
		t.Fatal("expected near silence")
	}
}

func TestBuildASRMP3Args_NoAlign(t *testing.T) {
	args := buildASRMP3Args(DefaultASRSampleRate, DefaultASRChannels, DefaultASRMP3Bitrate, "/in.mp4", "/out.mp3", ASRAlignOptions{})
	wantAF := "aresample=async=10000:first_pts=0,asetpts=PTS-STARTPTS,aformat=sample_rates=16000:channel_layouts=mono"
	want := []string{
		"-y", "-threads", "6", "-i", "/in.mp4", "-vn",
		"-af", wantAF,
		"-c:a", "libmp3lame", "-b:a", "64k", "/out.mp3",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d; args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildASRMP3Args_WithAlign(t *testing.T) {
	align := ASRAlignOptions{LeadPadMs: 1000, TrimStartSec: 0.5, TargetDurSec: 5}
	args := buildASRMP3Args(DefaultASRSampleRate, DefaultASRChannels, DefaultASRMP3Bitrate, "/in.mp4", "/out.mp3", align)
	wantAF := "aresample=async=10000:first_pts=0,atrim=start=0.5,asetpts=PTS-STARTPTS,adelay=delays=1000:all=1,aformat=sample_rates=16000:channel_layouts=mono,apad=whole_dur=5,atrim=duration=5,asetpts=PTS-STARTPTS"
	want := []string{
		"-y", "-threads", "6", "-i", "/in.mp4", "-vn",
		"-af", wantAF,
		"-c:a", "libmp3lame", "-b:a", "64k",
		"-t", "5",
		"/out.mp3",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d; args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildASRMP3RangeArgs_SeekAndDuration(t *testing.T) {
	align := ASRAlignOptions{TargetDurSec: 120}
	args := buildASRMP3RangeArgs(DefaultASRSampleRate, DefaultASRChannels, DefaultASRMP3Bitrate, "/win.mp4", "/chunk.mp3", 240, 120, align)
	want := []string{
		"-y", "-threads", "6",
		"-ss", "240", "-i", "/win.mp4", "-t", "120",
		"-vn", "-af", "aresample=async=10000:first_pts=0,asetpts=PTS-STARTPTS,aformat=sample_rates=16000:channel_layouts=mono,apad=whole_dur=120,atrim=duration=120,asetpts=PTS-STARTPTS",
		"-c:a", "libmp3lame", "-b:a", "64k",
		"-t", "120",
		"/chunk.mp3",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d; args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildASRAlignAudioFilter_StereoLeadPad(t *testing.T) {
	got := buildASRAlignAudioFilter(16000, 2, ASRAlignOptions{LeadPadMs: 250})
	want := "aresample=async=10000:first_pts=0,asetpts=PTS-STARTPTS,adelay=delays=250:all=1,aformat=sample_rates=16000:channel_layouts=stereo"
	if got != want {
		t.Errorf("filter = %q, want %q", got, want)
	}
}

func TestBuildCutVideoArgs_ShortStart(t *testing.T) {
	args := buildCutVideoArgs("/in.mp4", "/out.mp4", 10, 30)
	want := []string{
		"-y", "-threads", "6",
		"-i", "/in.mp4", "-ss", "10",
		"-t", "20",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-crf", "18",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		"/out.mp4",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d; args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildCutVideoArgs_HybridSeek(t *testing.T) {
	args := buildCutVideoArgs("/in.mp4", "/out.mp4", 100, 120)
	want := []string{
		"-y", "-threads", "6",
		"-ss", "85", "-i", "/in.mp4", "-ss", "15",
		"-t", "20",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-crf", "18",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		"/out.mp4",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d; args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildCutVideoArgs_FFConcatForcesOutputSideSeek(t *testing.T) {
	args := buildCutVideoArgs(`docker\staging\source_local.ffconcat`, "/out.mp4", 270.17, 276.85)
	// 不得出现输入侧 -ss（会在 TS concat 上切错内容）。
	if args[2] == "-ss" {
		t.Fatalf("ffconcat must not use input-side -ss, got %v", args)
	}
	wantPrefix := []string{"-y", "-threads", "6", "-i", `docker\staging\source_local.ffconcat`, "-ss", "270.17"}
	for i := range wantPrefix {
		if args[i] != wantPrefix[i] {
			t.Fatalf("args[%d]=%q want %q; full=%v", i, args[i], wantPrefix[i], args)
		}
	}
}

func TestBuildCutVideoFastArgs_FFConcatOutputSideSeek(t *testing.T) {
	args := buildCutVideoFastArgs("source_local.ffconcat", "/out.mp4", 100, 120)
	// [-y, -threads, N, -i, file, -ss, ...]
	if len(args) < 6 || args[3] != "-i" || args[5] != "-ss" {
		t.Fatalf("want -i then -ss for ffconcat, got %v", args)
	}
}

func TestBuildCutVideoFastArgs(t *testing.T) {
	args := buildCutVideoFastArgs("/in.mp4", "/out.mp4", 10, 30)
	want := []string{
		"-y", "-threads", "6",
		"-ss", "10", "-i", "/in.mp4",
		"-t", "20",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-movflags", "+faststart",
		"/out.mp4",
	}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d; args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestFFmpegConverter_CutVideoSegment_InvalidRange(t *testing.T) {
	c := NewFFmpegConverter("")
	err := c.CutVideoSegment(context.Background(), "in.mp4", "out.mp4", 10, 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFFmpegConverter_CutVideoSegmentFast_InvalidRange(t *testing.T) {
	c := NewFFmpegConverter("")
	err := c.CutVideoSegmentFast(context.Background(), "in.mp4", "out.mp4", 5, 5)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFFmpegConverter_CutVideoSegment_Success(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "input.mp4")
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input mp4: %v, output=%s", err, output)
	}

	outputPath := filepath.Join(workDir, "clip.mp4")
	c := NewFFmpegConverter(ffmpegPath)
	if err := c.CutVideoSegment(context.Background(), inputPath, outputPath, 0.2, 1.0); err != nil {
		t.Fatalf("CutVideoSegment() error = %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output should not be empty")
	}
}

func TestFFmpegConverter_CutVideoSegmentFast_Success(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "input.mp4")
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input mp4: %v, output=%s", err, output)
	}

	outputPath := filepath.Join(workDir, "clip_fast.mp4")
	c := NewFFmpegConverter(ffmpegPath)
	if err := c.CutVideoSegmentFast(context.Background(), inputPath, outputPath, 0.2, 1.0); err != nil {
		t.Fatalf("CutVideoSegmentFast() error = %v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output should not be empty")
	}
}

func TestFFmpegConverter_ResolvedDefaults(t *testing.T) {
	c := &FFmpegConverter{}
	if c.resolvedSampleRate() != DefaultASRSampleRate {
		t.Errorf("sample rate = %d, want %d", c.resolvedSampleRate(), DefaultASRSampleRate)
	}
	if c.resolvedChannels() != DefaultASRChannels {
		t.Errorf("channels = %d, want %d", c.resolvedChannels(), DefaultASRChannels)
	}
	if c.resolvedMP3Bitrate() != DefaultASRMP3Bitrate {
		t.Errorf("bitrate = %q, want %q", c.resolvedMP3Bitrate(), DefaultASRMP3Bitrate)
	}
}

func TestFFmpegConverter_ConvertToASRMP3_InvalidInput(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	c := NewFFmpegConverter("")
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := c.ConvertToASRMP3(context.Background(), filepath.Join(t.TempDir(), "missing.mp4"), out)
	if err == nil {
		t.Fatal("expected error for missing input file")
	}
	if !strings.Contains(err.Error(), "ffmpeg 转 MP3 失败") {
		t.Errorf("error = %v, want ffmpeg conversion failure", err)
	}
}

func TestFFmpegConverter_ConvertToASRMP3_Success(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "input.wav")
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "1", inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input wav: %v, output=%s", err, output)
	}

	outputPath := filepath.Join(workDir, "asr.mp3")
	c := NewFFmpegConverter(ffmpegPath)
	if err := c.ConvertToASRMP3(context.Background(), inputPath, outputPath); err != nil {
		t.Fatalf("ConvertToASRMP3() error = %v", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output mp3 should not be empty")
	}
}

func TestFFmpegConverter_ConvertToASRMP3Aligned_AudioLate(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}

	workDir := t.TempDir()
	videoPath := filepath.Join(workDir, "video.mp4")
	audioPath := filepath.Join(workDir, "audio.wav")
	inputPath := filepath.Join(workDir, "offset.mp4")

	// 5s 视频 + 从 1s 开始的 4s 音频，模拟音轨晚于视频。
	genVideo := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25",
		"-t", "5", "-c:v", "libx264", "-pix_fmt", "yuv420p", videoPath,
	)
	if output, err := genVideo.CombinedOutput(); err != nil {
		t.Fatalf("generate video: %v, output=%s", err, output)
	}
	genAudio := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=44100",
		"-t", "4", audioPath,
	)
	if output, err := genAudio.CombinedOutput(); err != nil {
		t.Fatalf("generate audio: %v, output=%s", err, output)
	}
	mux := exec.Command(ffmpegPath,
		"-y", "-i", videoPath,
		"-itsoffset", "1", "-i", audioPath,
		"-map", "0:v:0", "-map", "1:a:0",
		"-c:v", "copy", "-c:a", "aac",
		"-shortest",
		inputPath,
	)
	if output, err := mux.CombinedOutput(); err != nil {
		t.Fatalf("mux offset av: %v, output=%s", err, output)
	}

	prober := NewFFprobeProber(ffprobePath)
	tl, err := prober.ProbeMediaTimeline(context.Background(), inputPath)
	if err != nil {
		t.Fatalf("ProbeMediaTimeline() error = %v", err)
	}
	align := tl.AlignOptions()
	if align.LeadPadMs < 800 {
		t.Fatalf("expected lead pad near 1000ms, got %d (audio_start=%v video_start=%v)", align.LeadPadMs, tl.AudioStartSec, tl.VideoStartSec)
	}

	outMP3 := filepath.Join(workDir, "aligned.mp3")
	c := NewFFmpegConverter(ffmpegPath)
	if err := c.ConvertToASRMP3Aligned(context.Background(), inputPath, outMP3, align); err != nil {
		t.Fatalf("ConvertToASRMP3Aligned() error = %v", err)
	}

	outTL, err := prober.ProbeMediaTimeline(context.Background(), outMP3)
	if err != nil {
		t.Fatalf("probe output mp3: %v", err)
	}
	outDur := outTL.FormatDurationSec
	if outDur <= 0 {
		outDur = outTL.AudioDurationSec
	}
	if math.Abs(outDur-align.TargetDurSec) > 0.35 {
		t.Errorf("output duration = %v, want ~%v", outDur, align.TargetDurSec)
	}
}

func TestConvertRangeToASRMP3Aligned_EqualDurationFromWindowMP4(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	win := filepath.Join("..", "..", "..", "docker", "html", "staging", "live_ingest", "1", "e1", "windows", "window_00000.mp4")
	if st, err := os.Stat(win); err != nil || st.Size() == 0 {
		t.Skip("fixture window_00000.mp4 not present")
	}
	prober := NewFFprobeProber("")
	tl, err := prober.ProbeMediaTimeline(context.Background(), win)
	if err != nil {
		t.Fatalf("probe window: %v", err)
	}
	target := tl.FormatDurationSec
	if target <= 0 {
		target = tl.VideoDurationSec
	}
	if target < 60 {
		t.Fatalf("unexpected window duration %v", target)
	}
	out := filepath.Join(t.TempDir(), "eq.mp3")
	c := NewFFmpegConverter(ffmpegPath)
	align := ASRAlignOptions{TargetDurSec: target}
	if err := c.ConvertRangeToASRMP3Aligned(context.Background(), win, out, 0, 0, align); err != nil {
		t.Fatalf("ConvertRangeToASRMP3Aligned: %v", err)
	}
	got, err := prober.ProbeDurationSec(context.Background(), out)
	if err != nil {
		t.Fatalf("probe mp3: %v", err)
	}
	if math.Abs(got-target) > asrMP3DurationSkewSec {
		t.Fatalf("mp3 duration = %v, want ~%v (window was known to decode to ~633s without async)", got, target)
	}
}

func TestBuildConcatMP4ContinuousArgs(t *testing.T) {
	args := buildConcatMP4ContinuousArgs([]string{"a.mp4", "b.mp4", "c.mp4"}, "out.mp4")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-filter_complex") {
		t.Fatalf("missing filter_complex: %v", args)
	}
	wantFC := "[0:v:0][0:a:0][1:v:0][1:a:0][2:v:0][2:a:0]concat=n=3:v=1:a=1[v][a]"
	found := false
	for i, a := range args {
		if a == "-filter_complex" && i+1 < len(args) && args[i+1] == wantFC {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("filter_complex want %q in %v", wantFC, args)
	}
	if args[len(args)-1] != "out.mp4" {
		t.Fatalf("output = %q", args[len(args)-1])
	}
	// Must not use demuxer bitstream copy path.
	for _, a := range args {
		if a == "copy" {
			t.Fatalf("continuous timeline concat must not bitstream-copy: %v", args)
		}
	}
}

func TestConcatMP4ContinuousTimeline_AVAligned(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	dir := t.TempDir()
	mk := func(name string, durSec float64) string {
		t.Helper()
		path := filepath.Join(dir, name)
		// Silent A/V with matching duration.
		args := []string{
			"-y", "-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=160x120:d=%g", durSec),
			"-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=44100:cl=stereo:d=%g", durSec),
			"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "64k", "-shortest", path,
		}
		cmd := exec.Command(ffmpegPath, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v (%s)", name, err, strings.TrimSpace(string(out)))
		}
		return path
	}
	a := mk("a.mp4", 1.2)
	b := mk("b.mp4", 1.5)
	out := filepath.Join(dir, "master.mp4")
	c := NewFFmpegConverter(ffmpegPath)
	if err := c.ConcatMP4ContinuousTimeline(context.Background(), []string{a, b}, out); err != nil {
		t.Fatalf("ConcatMP4ContinuousTimeline: %v", err)
	}
	tl, err := NewFFprobeProber("").ProbeMediaTimeline(context.Background(), out)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if tl.VideoDurationSec < 2.5 || tl.VideoDurationSec > 3.0 {
		t.Fatalf("video dur=%v want ~2.7", tl.VideoDurationSec)
	}
	skew := tl.AudioDurationSec - tl.VideoDurationSec
	if skew < 0 {
		skew = -skew
	}
	if skew > 0.15 {
		t.Fatalf("A/V skew too large: video=%v audio=%v", tl.VideoDurationSec, tl.AudioDurationSec)
	}
}
