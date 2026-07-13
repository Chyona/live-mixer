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

func TestLiveMaterialASRAudioPreparer_Prepare_Success(t *testing.T) {
	const sessionID = "test-session-id"
	oldNewASRSessionID := newASRSessionID
	newASRSessionID = func() string { return sessionID }
	t.Cleanup(func() { newASRSessionID = oldNewASRSessionID })

	tempDir := t.TempDir()
	var (
		downloadDest string
		uploadedKey  string
		uploadedPath string
	)

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
		nil,
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				uploadedPath = localPath
				uploadedKey = objectKey
				return "https://bucket.example.com/" + objectKey, nil
			},
		},
		tempDir,
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

	wantLocal := buildASRLocalPath(tempDir, sessionID)
	if downloadDest != wantLocal {
		t.Errorf("download dest = %q, want %q", downloadDest, wantLocal)
	}
	if uploadedPath != wantLocal {
		t.Errorf("upload local path = %q, want %q", uploadedPath, wantLocal)
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
		nil,
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
		nil,
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

func TestLiveMaterialASRAudioPreparer_Prepare_UploadFailed(t *testing.T) {
	preparer := NewLiveMaterialASRAudioPreparer(
		&mockFileDownloader{
			downloadFn: func(url, dest string) (string, error) {
				return dest, os.WriteFile(dest, []byte("x"), 0644)
			},
		},
		nil,
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				return "", context.Canceled
			},
		},
		t.TempDir(),
		nil,
	)
	_, cleanup, err := preparer.Prepare(context.Background(), 1, "https://example.com/a.mp4", nil)
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "上传 ASR 音频失败") {
		t.Fatalf("Prepare() error = %v, want upload failure", err)
	}
}

func TestLiveMaterialASRAudioPreparer_TempDirFallback(t *testing.T) {
	p := NewLiveMaterialASRAudioPreparer(nil, nil, &mockObjectUploader{}, "", nil).(*liveMaterialASRAudioPreparer)
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
		nil,
		&mockObjectUploader{
			uploadFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				return "https://bucket.example.com/" + objectKey, nil
			},
		},
		"",
		nil,
	)

	_, cleanup, err := preparer.Prepare(context.Background(), 99, "https://example.com/live.mp4", nil)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	defer cleanup()
}
