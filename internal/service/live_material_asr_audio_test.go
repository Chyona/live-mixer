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

func (m *mockAudioConverter) ConvertToASRMP3(ctx context.Context, inputPath, outputPath string) error {
	return m.convertFn(ctx, inputPath, outputPath)
}

type mockObjectUploader struct {
	uploadFn func(ctx context.Context, localPath, objectKey string) (string, error)
}

func (m *mockObjectUploader) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	return m.uploadFn(ctx, localPath, objectKey)
}


type mockVideoProber struct {
	probeFn func(ctx context.Context, inputPath string) (width, height int, err error)
}

func (m *mockVideoProber) ProbeVideoSize(ctx context.Context, inputPath string) (width, height int, err error) {
	if m.probeFn != nil {
		return m.probeFn(ctx, inputPath)
	}
	return 0, 0, nil
}

func defaultMockConverter() *mockAudioConverter {
	return &mockAudioConverter{
		convertFn: func(ctx context.Context, inputPath, outputPath string) error {
			return os.WriteFile(outputPath, []byte("fake-mp3"), 0644)
		},
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_Success(t *testing.T) {
	const sessionID = "test-session-id"
	oldNewASRSessionID := newASRSessionID
	newASRSessionID = func() string { return sessionID }
	t.Cleanup(func() { newASRSessionID = oldNewASRSessionID })

	tempDir := t.TempDir()
	var (
		downloadDest string
		convertInput string
		convertOut   string
		uploadedKey  string
		uploadedPath string
	)

	var probedPath string
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				downloadDest = dest
				if err := os.WriteFile(dest, []byte("fake-media"), 0644); err != nil {
					return "", err
				}
				return dest, nil
			},
		},
		&mockAudioConverter{
			convertFn: func(ctx context.Context, inputPath, outputPath string) error {
				convertInput = inputPath
				convertOut = outputPath
				return os.WriteFile(outputPath, []byte("fake-mp3"), 0644)
			},
		},
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				uploadedPath = localPath
				uploadedKey = objectKey
				return "https://bucket.example.com/" + objectKey, nil
			},
		},
		tempDir,
		nil,
		&mockVideoProber{
			probeFn: func(ctx context.Context, inputPath string) (int, int, error) {
				probedPath = inputPath
				return 1920, 1080, nil
			},
		},
	)

	var progresses []int16
	result, err := preparer.Prepare(context.Background(), 12, "https://example.com/live.mp4", func(p int16) {
		progresses = append(progresses, p)
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer result.Cleanup()
	audioURL := result.AudioURL

	if probedPath == "" {
		t.Fatal("expected ProbeVideoSize to be called")
	}
	if result.Width != 1920 || result.Height != 1080 {
		t.Errorf("Width/Height = %d/%d, want 1920x1080", result.Width, result.Height)
	}
	wantSource := buildASRSourceLocalPath(tempDir, sessionID)
	if downloadDest != wantSource {
		t.Errorf("download dest = %q, want %q", downloadDest, wantSource)
	}
	wantMP3 := buildASRLocalPath(tempDir, sessionID)
	if convertInput != wantSource {
		t.Errorf("convert input = %q, want %q", convertInput, wantSource)
	}
	if convertOut != wantMP3 {
		t.Errorf("convert output = %q, want %q", convertOut, wantMP3)
	}
	if uploadedPath != wantMP3 {
		t.Errorf("upload local path = %q, want %q", uploadedPath, wantMP3)
	}
	wantKey := buildASRObjectKey("temp", sessionID)
	if uploadedKey != wantKey {
		t.Errorf("uploaded key = %q, want %q", uploadedKey, wantKey)
	}
	if audioURL == "" {
		t.Fatal("audioURL should not be empty")
	}
	if len(progresses) == 0 {
		t.Error("expected progress callbacks")
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_UploaderMissing(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{},
		defaultMockConverter(),
		nil,
		t.TempDir(),
		nil,
		nil,
	)
	_, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
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
		defaultMockConverter(),
		&mockObjectUploader{},
		t.TempDir(),
		nil,
		nil,
	)
	result, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if result.Cleanup != nil {
		defer result.Cleanup()
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
		nil,
	)
	result, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if result.Cleanup != nil {
		defer result.Cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "转码 ASR MP3 失败") {
		t.Fatalf("Prepare() error = %v, want convert failure", err)
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_UploadFailed(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				return dest, os.WriteFile(dest, []byte("x"), 0644)
			},
		},
		defaultMockConverter(),
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				return "", context.Canceled
			},
		},
		t.TempDir(),
		nil,
		nil,
	)
	result, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if result.Cleanup != nil {
		defer result.Cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "上传 ASR 音频失败") {
		t.Fatalf("Prepare() error = %v, want upload failure", err)
	}
}

func TestLiveMaterialASRAudioPreparer_TempDirFallback(t *testing.T) {
	p := NewLiveMaterialASRAudioPreparer(nil, nil, &mockObjectUploader{}, "", nil, nil).(*liveMaterialASRAudioPreparer)
	got, err := p.resolveTempDir()
	if err != nil {
		t.Fatalf("resolveTempDir() error = %v", err)
	}
	if !strings.HasSuffix(got, defaultTempDirName) {
		t.Errorf("temp dir = %q, want suffix %q", got, defaultTempDirName)
	}
}

func TestDefaultTempDir_UnderProcessDir(t *testing.T) {
	base, err := processBaseDir()
	if err != nil {
		t.Fatalf("processBaseDir() error = %v", err)
	}
	got, err := defaultTempDir()
	if err != nil {
		t.Fatalf("defaultTempDir() error = %v", err)
	}
	want := filepath.Join(base, defaultTempDirName)
	if got != want {
		t.Errorf("defaultTempDir() = %q, want %q", got, want)
	}
}

func TestLiveMaterialASRAudioPreparer_Prepare_DefaultTempDir(t *testing.T) {
	const sessionID = "default-temp-session"
	oldNewASRSessionID := newASRSessionID
	newASRSessionID = func() string { return sessionID }
	t.Cleanup(func() { newASRSessionID = oldNewASRSessionID })

	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				return dest, os.WriteFile(dest, []byte("fake"), 0644)
			},
		},
		defaultMockConverter(),
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				return "https://bucket.example.com/" + objectKey, nil
			},
		},
		"",
		nil,
		nil,
	)

	result, err := preparer.Prepare(context.Background(), 99, "https://example.com/live.mp4", nil)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer result.Cleanup()
}
