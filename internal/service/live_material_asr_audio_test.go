package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockFileDownloader struct {
	downloadFn func(url, dest string) (string, error)
}

func (m *mockFileDownloader) Download(url, dest string) (string, error) {
	return m.downloadFn(url, dest)
}

type mockAudioConverter struct {
	convertFn func(ctx context.Context, inputPath, outputPath string) error
}

func (m *mockAudioConverter) ConvertToASRWAV(ctx context.Context, inputPath, outputPath string) error {
	return m.convertFn(ctx, inputPath, outputPath)
}

type mockObjectUploader struct {
	uploadFn func(ctx context.Context, localPath, objectKey string) (string, error)
}

func (m *mockObjectUploader) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	return m.uploadFn(ctx, localPath, objectKey)
}

func TestGuessSourceExtension(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/live.mp4", ".mp4"},
		{"https://example.com/audio.mp3?token=1", ".mp3"},
		{"https://example.com/stream", ".mp4"},
	}
	for _, tt := range tests {
		if got := guessSourceExtension(tt.url); got != tt.want {
			t.Errorf("guessSourceExtension(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestBuildASRAudioObjectKey(t *testing.T) {
	key := buildASRAudioObjectKey("temp", 9)
	if !strings.HasPrefix(key, "temp/9/") {
		t.Errorf("object key = %q, want prefix temp/9/", key)
	}
	if !strings.HasSuffix(key, ".wav") {
		t.Errorf("object key = %q, want .wav suffix", key)
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_Success(t *testing.T) {
	var uploadedKey string
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				if err := os.WriteFile(dest, []byte("fake-media"), 0644); err != nil {
					return "", err
				}
				return dest, nil
			},
		},
		&mockAudioConverter{
			convertFn: func(ctx context.Context, inputPath, outputPath string) error {
				return os.WriteFile(outputPath, []byte("RIFFxxxxWAVE"), 0644)
			},
		},
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				uploadedKey = objectKey
				return "https://bucket.example.com/" + objectKey, nil
			},
		},
		t.TempDir(),
		nil,
	)

	var progresses []int16
	audioURL, cleanup, err := preparer.Prepare(context.Background(), 12, "https://example.com/live.mp4", func(p int16) {
		progresses = append(progresses, p)
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer cleanup()

	if audioURL == "" {
		t.Fatal("audioURL should not be empty")
	}
	if !strings.Contains(uploadedKey, "temp/12/") {
		t.Errorf("uploaded key = %q, want under temp/12/", uploadedKey)
	}
	if len(progresses) == 0 {
		t.Error("expected progress callbacks")
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_UploaderMissing(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{},
		&mockAudioConverter{},
		nil,
		t.TempDir(),
		nil,
	)
	_, _, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if err == nil || !strings.Contains(err.Error(), "对象存储未配置") {
		t.Fatalf("Prepare() error = %v, want storage not configured", err)
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_DownloadFailed(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				return "", context.DeadlineExceeded
			},
		},
		&mockAudioConverter{},
		&mockObjectUploader{},
		t.TempDir(),
		nil,
	)
	_, cleanup, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "下载直播素材失败") {
		t.Fatalf("Prepare() error = %v, want download failure", err)
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_ConvertFailed(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				return dest, os.WriteFile(dest, []byte("x"), 0644)
			},
		},
		&mockAudioConverter{
			convertFn: func(ctx context.Context, inputPath, outputPath string) error {
				return context.Canceled
			},
		},
		&mockObjectUploader{},
		t.TempDir(),
		nil,
	)
	_, cleanup, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "转码 WAV 失败") {
		t.Fatalf("Prepare() error = %v, want convert failure", err)
	}
}

func TestLiveMaterialASRAudioPreparer_WorkDirFallback(t *testing.T) {
	p := NewLiveMaterialASRAudioPreparer(nil, nil, &mockObjectUploader{}, "", nil).(*liveMaterialASRAudioPreparer)
	got, err := p.resolveWorkDir()
	if err != nil {
		t.Fatalf("resolveWorkDir() error = %v", err)
	}
	wantSuffix := filepath.Join(defaultTempDirName, defaultASRWorkSubDir)
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("work dir = %q, want suffix %q", got, wantSuffix)
	}
}

func TestDefaultASRWorkDir_UnderProcessDir(t *testing.T) {
	base, err := processBaseDir()
	if err != nil {
		t.Fatalf("processBaseDir() error = %v", err)
	}
	got, err := defaultASRWorkDir()
	if err != nil {
		t.Fatalf("defaultASRWorkDir() error = %v", err)
	}
	want := filepath.Join(base, defaultTempDirName, defaultASRWorkSubDir)
	if got != want {
		t.Errorf("defaultASRWorkDir() = %q, want %q", got, want)
	}
}

// TestLiveMaterialASRAudioPreparer_Prepare_DefaultWorkDir 验证未指定 workDir 时自动创建嵌套临时目录（Windows 兼容）。
func TestLiveMaterialASRAudioPreparer_Prepare_DefaultWorkDir(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				return dest, os.WriteFile(dest, []byte("fake"), 0644)
			},
		},
		&mockAudioConverter{
			convertFn: func(ctx context.Context, inputPath, outputPath string) error {
				return os.WriteFile(outputPath, []byte("wav"), 0644)
			},
		},
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				return "https://bucket.example.com/" + objectKey, nil
			},
		},
		"", // 使用进程目录下 temp/asr
		nil,
	)

	_, cleanup, err := preparer.Prepare(context.Background(), 99, "https://example.com/live.mp4", nil)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer cleanup()
}
