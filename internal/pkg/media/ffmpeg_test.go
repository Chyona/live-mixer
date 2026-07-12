package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildASRWAVArgs(t *testing.T) {
	args := buildASRWAVArgs(DefaultASRSampleRate, DefaultASRChannels, "/in.mp4", "/out.wav")
	want := []string{"-y", "-i", "/in.mp4", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "/out.wav"}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d", len(args), len(want))
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
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
}

func TestFFmpegConverter_ConvertToASRWAV_InvalidInput(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	c := NewFFmpegConverter("")
	out := filepath.Join(t.TempDir(), "out.wav")
	err := c.ConvertToASRWAV(context.Background(), filepath.Join(t.TempDir(), "missing.mp4"), out)
	if err == nil {
		t.Fatal("expected error for missing input file")
	}
	if !strings.Contains(err.Error(), "ffmpeg 转 WAV 失败") {
		t.Errorf("error = %v, want ffmpeg conversion failure", err)
	}
}

func TestFFmpegConverter_ConvertToASRWAV_Success(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "input.wav")
	// 生成 1 秒静音 WAV，再转码验证流程可跑通。
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "1", inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input wav: %v, output=%s", err, output)
	}

	outputPath := filepath.Join(workDir, "asr.wav")
	c := NewFFmpegConverter(ffmpegPath)
	if err := c.ConvertToASRWAV(context.Background(), inputPath, outputPath); err != nil {
		t.Fatalf("ConvertToASRWAV() error = %v", err)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output wav should not be empty")
	}
}
