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

func TestBuildProbeMediaTimelineArgs(t *testing.T) {
	args := buildProbeMediaTimelineArgs("/in.mp4")
	want := []string{
		"-v", "error",
		"-show_entries", "stream=codec_type,width,height,start_time,duration:format=duration",
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

func TestParseProbeMediaTimelineJSON(t *testing.T) {
	raw := []byte(`{
		"streams":[
			{"codec_type":"video","width":1280,"height":720,"start_time":"0.000000","duration":"5.000000"},
			{"codec_type":"audio","start_time":"1.000000","duration":"4.000000"}
		],
		"format":{"duration":"5.000000"}
	}`)
	tl, err := parseProbeMediaTimelineJSON(raw)
	if err != nil {
		t.Fatalf("parseProbeMediaTimelineJSON() error = %v", err)
	}
	if !tl.HasVideo || !tl.HasAudio {
		t.Fatalf("HasVideo/HasAudio = %v/%v", tl.HasVideo, tl.HasAudio)
	}
	if tl.Width != 1280 || tl.Height != 720 {
		t.Errorf("size = %dx%d, want 1280x720", tl.Width, tl.Height)
	}
	if tl.VideoStartSec != 0 || tl.VideoDurationSec != 5 {
		t.Errorf("video start/dur = %v/%v", tl.VideoStartSec, tl.VideoDurationSec)
	}
	if tl.AudioStartSec != 1 || tl.AudioDurationSec != 4 {
		t.Errorf("audio start/dur = %v/%v", tl.AudioStartSec, tl.AudioDurationSec)
	}
	if tl.FormatDurationSec != 5 {
		t.Errorf("format dur = %v, want 5", tl.FormatDurationSec)
	}
}

func TestMediaTimeline_AlignOptions(t *testing.T) {
	t.Run("audio late", func(t *testing.T) {
		opts := MediaTimeline{
			HasVideo: true, VideoStartSec: 0, VideoDurationSec: 5,
			HasAudio: true, AudioStartSec: 1.0, AudioDurationSec: 4,
		}.AlignOptions()
		if opts.LeadPadMs != 1000 {
			t.Errorf("LeadPadMs = %d, want 1000", opts.LeadPadMs)
		}
		if opts.TrimStartSec != 0 {
			t.Errorf("TrimStartSec = %v, want 0", opts.TrimStartSec)
		}
		if opts.TargetDurSec != 5 {
			t.Errorf("TargetDurSec = %v, want 5", opts.TargetDurSec)
		}
	})
	t.Run("audio early", func(t *testing.T) {
		opts := MediaTimeline{
			HasVideo: true, VideoStartSec: 0.5, VideoDurationSec: 10,
			HasAudio: true, AudioStartSec: 0, AudioDurationSec: 10.5,
		}.AlignOptions()
		if opts.LeadPadMs != 0 {
			t.Errorf("LeadPadMs = %d, want 0", opts.LeadPadMs)
		}
		if opts.TrimStartSec != 0.5 {
			t.Errorf("TrimStartSec = %v, want 0.5", opts.TrimStartSec)
		}
		if opts.TargetDurSec != 10 {
			t.Errorf("TargetDurSec = %v, want 10", opts.TargetDurSec)
		}
	})
	t.Run("audio only falls back to format then audio dur", func(t *testing.T) {
		opts := MediaTimeline{
			HasAudio: true, AudioDurationSec: 3, FormatDurationSec: 3.2,
		}.AlignOptions()
		if opts.TargetDurSec != 3.2 {
			t.Errorf("TargetDurSec = %v, want 3.2", opts.TargetDurSec)
		}
	})
}

func TestFFprobeProber_ProbeMediaTimeline_Success(t *testing.T) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}

	workDir := t.TempDir()
	inputPath := filepath.Join(workDir, "av.mp4")
	genCmd := exec.Command(ffmpegPath,
		"-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100",
		"-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		inputPath,
	)
	if output, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate input mp4: %v, output=%s", err, output)
	}

	prober := NewFFprobeProber(ffprobePath)
	tl, err := prober.ProbeMediaTimeline(context.Background(), inputPath)
	if err != nil {
		t.Fatalf("ProbeMediaTimeline() error = %v", err)
	}
	if !tl.HasVideo || !tl.HasAudio {
		t.Fatalf("HasVideo/HasAudio = %v/%v", tl.HasVideo, tl.HasAudio)
	}
	if tl.Width != 320 || tl.Height != 240 {
		t.Errorf("size = %dx%d, want 320x240", tl.Width, tl.Height)
	}
	if tl.VideoDurationSec < 1.5 || tl.AudioDurationSec < 1.5 {
		t.Errorf("durations too short: video=%v audio=%v", tl.VideoDurationSec, tl.AudioDurationSec)
	}
}
