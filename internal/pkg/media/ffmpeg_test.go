package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildASRMP3Args(t *testing.T) {
	args := buildASRMP3Args(DefaultASRSampleRate, DefaultASRChannels, DefaultASRMP3Bitrate, "/in.mp4", "/out.mp3")
	want := []string{"-y", "-threads", "4", "-i", "/in.mp4", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "libmp3lame", "-b:a", "64k", "/out.mp3"}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d", len(args), len(want))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestBuildCutVideoArgs(t *testing.T) {
	args := buildCutVideoArgs("/in.mp4", "/out.mp4", 10, 30)
	want := []string{
		"-y", "-threads", "4", "-i", "/in.mp4",
		"-ss", "10", "-to", "30",
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
