package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// mockStorageProvider 用于测试 Client 与 storageProvider 的解耦。
type mockStorageProvider struct {
	providerType ProviderType
	uploadFileFn func(ctx context.Context, localPath, objectKey string) (string, error)
	uploadReaderFn func(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error)
}

func (m *mockStorageProvider) Type() ProviderType {
	return m.providerType
}

func (m *mockStorageProvider) UploadFile(ctx context.Context, localPath, objectKey string) (string, error) {
	if m.uploadFileFn != nil {
		return m.uploadFileFn(ctx, localPath, objectKey)
	}
	return "https://mock.example.com/" + objectKey, nil
}

func (m *mockStorageProvider) UploadReader(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
	if m.uploadReaderFn != nil {
		return m.uploadReaderFn(ctx, r, objectKey, size)
	}
	return "https://mock.example.com/" + objectKey, nil
}

func TestClient_UploadFile(t *testing.T) {
	called := false
	client := &Client{
		provider: &mockStorageProvider{
			providerType: ProviderCOS,
			uploadFileFn: func(ctx context.Context, localPath, objectKey string) (string, error) {
				called = true
				if localPath != "/tmp/test.txt" {
					t.Errorf("localPath = %q, want /tmp/test.txt", localPath)
				}
				if objectKey != "uploads/test.txt" {
					t.Errorf("objectKey = %q, want uploads/test.txt", objectKey)
				}
				return "https://cdn.example.com/uploads/test.txt", nil
			},
		},
	}

	url, err := client.UploadFile(context.Background(), "/tmp/test.txt", "uploads/test.txt")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	if !called {
		t.Fatal("expected provider UploadFile to be called")
	}
	if url != "https://cdn.example.com/uploads/test.txt" {
		t.Errorf("url = %q, want https://cdn.example.com/uploads/test.txt", url)
	}
}

func TestClient_UploadFile_Validation(t *testing.T) {
	client := &Client{provider: &mockStorageProvider{providerType: ProviderCOS}}

	tests := []struct {
		name      string
		localPath string
		objectKey string
	}{
		{"空本地路径", "", "key"},
		{"空对象键", "/tmp/a", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UploadFile(context.Background(), tt.localPath, tt.objectKey)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestClient_UploadReader(t *testing.T) {
	client := &Client{
		provider: &mockStorageProvider{
			providerType: ProviderOSS,
			uploadReaderFn: func(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
				data, err := io.ReadAll(r)
				if err != nil {
					return "", err
				}
				if string(data) != "hello" {
					t.Errorf("data = %q, want hello", string(data))
				}
				if size != 5 {
					t.Errorf("size = %d, want 5", size)
				}
				return "https://oss.example.com/" + objectKey, nil
			},
		},
	}

	url, err := client.UploadReader(context.Background(), strings.NewReader("hello"), "a.txt", 5)
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	if url != "https://oss.example.com/a.txt" {
		t.Errorf("url = %q, want https://oss.example.com/a.txt", url)
	}
}

func TestClient_UploadReader_ProviderError(t *testing.T) {
	client := &Client{
		provider: &mockStorageProvider{
			providerType: ProviderOSS,
			uploadReaderFn: func(ctx context.Context, r io.Reader, objectKey string, size int64) (string, error) {
				return "", errors.New("upload failed")
			},
		},
	}

	_, err := client.UploadReader(context.Background(), strings.NewReader("x"), "a.txt", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUploadOptions_Defaults(t *testing.T) {
	opts := UploadOptions{}

	if got := opts.cosPartSizeMB(); got != defaultPartSizeMB {
		t.Errorf("cosPartSizeMB() = %d, want %d", got, defaultPartSizeMB)
	}
	if got := opts.ossPartSizeBytes(); got != defaultPartSizeBytes {
		t.Errorf("ossPartSizeBytes() = %d, want %d", got, defaultPartSizeBytes)
	}
	if got := opts.tosPartSizeBytes(); got != defaultPartSizeBytes {
		t.Errorf("tosPartSizeBytes() = %d, want %d", got, defaultPartSizeBytes)
	}
	if got := opts.concurrency(); got != defaultConcurrency {
		t.Errorf("concurrency() = %d, want %d", got, defaultConcurrency)
	}
}

func TestUploadOptions_CustomValues(t *testing.T) {
	opts := UploadOptions{
		PartSizeMB:    10,
		PartSizeBytes: 10 * 1024 * 1024,
		Concurrency:   5,
	}

	if got := opts.cosPartSizeMB(); got != 10 {
		t.Errorf("cosPartSizeMB() = %d, want 10", got)
	}
	if got := opts.ossPartSizeBytes(); got != 10*1024*1024 {
		t.Errorf("ossPartSizeBytes() = %d, want %d", got, 10*1024*1024)
	}
	if got := opts.tosPartSizeBytes(); got != 10*1024*1024 {
		t.Errorf("tosPartSizeBytes() = %d, want %d", got, 10*1024*1024)
	}
	if got := opts.concurrency(); got != 5 {
		t.Errorf("concurrency() = %d, want 5", got)
	}
}
