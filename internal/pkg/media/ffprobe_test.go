package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProbeVideoSizeArgs(t *testing.T) {
	args := buildProbeVideoSizeArgs("/in.mp4")
	want := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "json",
		"/in.mp4",
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

func TestParseProbeVideoSizeJSON_WithVideoStream(t *testing.T) {
	raw := []byte(`{"programs":[],"streams":[{"width":1920,"height":1080}]}`)
	w, h, err := parseProbeVideoSizeJSON(raw)
	if err != nil {
		t.Fatalf("parseProbeVideoSizeJSON() error = %v", err)
	}
	if w != 1920 || h != 1080 {
		t.Errorf("got %dx%d, want 1920x1080", w, h)
	}
}

func TestParseProbeVideoSizeJSON_NoVideoStream(t *testing.T) {
	raw := []byte(`{"programs":[],"streams":[]}`)
	w, h, err := parseProbeVideoSizeJSON(raw)
	if err != nil {
		t.Fatalf("parseProbeVideoSizeJSON() error = %v", err)
	}
	if w != 0 || h != 0 {
		t.Errorf("got %dx%d, want 0x0", w, h)
	}
}

func TestParseProbeVideoSizeJSON_InvalidJSON(t *testing.T) {
	_, _, err := parseProbeVideoSizeJSON([]byte(`not-json`))
	if err == nil || !strings.Contains(err.Error(), "解析 ffprobe JSON 失败") {
		t.Fatalf("error = %v, want JSON parse failure", err)
	}
}

func TestFFprobeProber_ProbeVideoSize_Success(t *testing.T) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "input.mp4")
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25",
		"-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input mp4: %v, output=%s", err, output)
	}

	prober := NewFFprobeProber(ffprobePath)
	w, h, err := prober.ProbeVideoSize(context.Background(), inputPath)
	if err != nil {
		t.Fatalf("ProbeVideoSize() error = %v", err)
	}
	if w != 640 || h != 360 {
		t.Errorf("got %dx%d, want 640x360", w, h)
	}
}

func TestFFprobeProber_ProbeVideoSize_MissingFile(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	prober := NewFFprobeProber("")
	_, _, err := prober.ProbeVideoSize(context.Background(), filepath.Join(t.TempDir(), "missing.mp4"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "ffprobe 探测分辨率失败") {
		t.Errorf("error = %v, want probe failure", err)
	}
}

func TestFFprobeProber_ProbeVideoSize_AudioOnly(t *testing.T) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "audio.mp3")
	genCmd := exec.Command(ffmpegPath,
		"-y", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "1", "-c:a", "libmp3lame", inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input mp3: %v, output=%s", err, output)
	}
	if _, err := os.Stat(inputPath); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	prober := NewFFprobeProber(ffprobePath)
	w, h, err := prober.ProbeVideoSize(context.Background(), inputPath)
	if err != nil {
		t.Fatalf("ProbeVideoSize() error = %v", err)
	}
	if w != 0 || h != 0 {
		t.Errorf("audio-only got %dx%d, want 0x0", w, h)
	}
}
