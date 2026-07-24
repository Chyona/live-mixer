package media

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildASRMP3Args_NoAlign(t *testing.T) {
	args := buildASRMP3Args(DefaultASRSampleRate, DefaultASRChannels, DefaultASRMP3Bitrate, "/in.mp4", "/out.mp3", ASRAlignOptions{})
	wantAF := "asetpts=PTS-STARTPTS,aformat=sample_rates=16000:channel_layouts=mono"
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
	wantAF := "atrim=start=0.5,asetpts=PTS-STARTPTS,adelay=delays=1000:all=1,aformat=sample_rates=16000:channel_layouts=mono,apad=whole_dur=5"
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

func TestBuildASRAlignAudioFilter_StereoLeadPad(t *testing.T) {
	got := buildASRAlignAudioFilter(16000, 2, ASRAlignOptions{LeadPadMs: 250})
	want := "asetpts=PTS-STARTPTS,adelay=delays=250:all=1,aformat=sample_rates=16000:channel_layouts=stereo"
	if got != want {
		t.Errorf("filter = %q, want %q", got, want)
	}
}

func TestBuildCutVideoArgs(t *testing.T) {
	args := buildCutVideoArgs("/in.mp4", "/out.mp4", 10, 30)
	want := []string{
		"-y", "-threads", "6",
		"-ss", "10", "-i", "/in.mp4",
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

func TestFFmpegConverter_CutVideoSegment_InvalidRange(t *testing.T) {
	c := NewFFmpegConverter("")
	err := c.CutVideoSegment(context.Background(), "in.mp4", "out.mp4", 10, 10)
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
	if math.Abs(outDur-align.TargetDurSec) > 0.2 {
		t.Errorf("output duration = %v, want ~%v", outDur, align.TargetDurSec)
	}
}
