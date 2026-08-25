package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

// newTestTOSProvider 创建指向 mock HTTP 服务的 TOS 后端，用于单元测试。
func newTestTOSProvider(t *testing.T, handler http.HandlerFunc) *tosProvider {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := tos.NewClientV2(server.URL,
		tos.WithRegion("cn-beijing"),
		tos.WithCredentials(tos.NewStaticCredentials("test-access-key-id", "test-access-key-secret")),
		tos.WithEnableCRC(false),
	)
	if err != nil {
		t.Fatalf("tos.NewClientV2() error = %v", err)
	}

	endpoint := strings.TrimPrefix(server.URL, "http://")
	return newTOSProviderWithClient(client, "test-bucket", endpoint, "cn-beijing", UploadOptions{
		PartSizeBytes:     5 * 1024 * 1024,
		Concurrency:       1,
		DisableCheckpoint: true,
	}, 0)
}

// writeTOSUploadFile 写入待上传的临时文件。
// TOS SDK 在 Windows 上可能延迟释放文件句柄，因此不使用 t.TempDir() 以避免清理失败。
func writeTOSUploadFile(t *testing.T, name string, size int) string {
	t.Helper()

	f, err := os.CreateTemp("", "tos-upload-*-"+name)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	path := f.Name()

	data := make([]byte, size)
	for i := range data {
		data[i] = byte('a' + (i % 26))
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// tosObjectKey 从请求路径中提取对象键名（path-style：/bucket/object）。
func tosObjectKey(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "test-bucket/")
	return path
}

// tosMockHandler 模拟 TOS 分片上传与简单上传接口（JSON 响应格式）。
func tosMockHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		key := tosObjectKey(r)

		// 初始化分片上传
		if r.Method == http.MethodPost && query.Has("uploads") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"Bucket":"test-bucket","Key":"%s","UploadId":"test-upload-id"}`, key)
			return
		}

		// 上传分片
		if r.Method == http.MethodPut && query.Has("uploadId") {
			io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", fmt.Sprintf(`"etag-part-%s"`, query.Get("partNumber")))
			w.WriteHeader(http.StatusOK)
			return
		}

		// 简单上传（PutObject）
		if r.Method == http.MethodPut {
			io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"simple-etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}

		// 完成分片上传
		if r.Method == http.MethodPost && query.Has("uploadId") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"Bucket":"test-bucket","Key":"done","ETag":"\"etag\"","Location":"https://test-bucket.tos.local/done"}`)
			return
		}

		t.Errorf("unexpected TOS request: %s %s", r.Method, r.URL.String())
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestTOSProvider_UploadFile_Simple(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))
	localPath := writeTOSUploadFile(t, "small.txt", 512)

	url, err := provider.UploadFile(context.Background(), localPath, "small.txt")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	assertPresignedURL(t, url, "small.txt")
}

func TestTOSProvider_UploadFile_Multipart(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))
	localPath := writeTOSUploadFile(t, "large.bin", 12*1024*1024)

	url, err := provider.UploadFile(context.Background(), localPath, "large.bin")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	assertPresignedURL(t, url, "large.bin")
}

func TestTOSProvider_UploadFile_FileNotFound(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))

	_, err := provider.UploadFile(context.Background(), filepath.Join(t.TempDir(), "missing.txt"), "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestTOSProvider_UploadReader_Small(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))

	url, err := provider.UploadReader(context.Background(), strings.NewReader("hello tos"), "reader.txt", 9)
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	assertPresignedURL(t, url, "reader.txt")
}

func TestTOSProvider_UploadReader_LargeUsesMultipart(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))
	data := strings.Repeat("x", 6*1024*1024)

	url, err := provider.UploadReader(context.Background(), strings.NewReader(data), "large-reader.bin", int64(len(data)))
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	assertPresignedURL(t, url, "large-reader.bin")
}

func TestTOSProvider_Type(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))
	if provider.Type() != ProviderTOS {
		t.Errorf("Type() = %q, want %q", provider.Type(), ProviderTOS)
	}
}

func TestTOSCheckpointDir(t *testing.T) {
	customDir := t.TempDir()
	got := tosCheckpointDir(UploadOptions{CheckpointDir: customDir})
	want := filepath.Join(customDir, "tos-checkpoint")
	if got != want {
		t.Errorf("tosCheckpointDir() = %q, want %q", got, want)
	}
}

func TestTOSProvider_UploadFile_CancelledContext(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))
	localPath := writeTOSUploadFile(t, "cancel.txt", 128)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.UploadFile(ctx, localPath, "cancel.txt")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestTOSProvider_objectURL(t *testing.T) {
	provider := &tosProvider{bucketName: "my-bucket", endpoint: "tos-cn-beijing.volces.com"}
	got := provider.objectURL("path/to/file.jpg")
	want := "https://my-bucket.tos-cn-beijing.volces.com/path/to/file.jpg"
	if got != want {
		t.Errorf("objectURL() = %q, want %q", got, want)
	}
}

func TestResolveTOSEndpoint(t *testing.T) {
	tests := []struct {
		name string
		cfg  TOSConfig
		want string
	}{
		{
			name: "显式 Endpoint",
			cfg: TOSConfig{Endpoint: "custom.tos.example.com", Region: "cn-beijing"},
			want: "https://custom.tos.example.com",
		},
		{
			name: "按地域生成默认 Endpoint",
			cfg:  TOSConfig{Region: "cn-shanghai"},
			want: "https://tos-cn-shanghai.volces.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTOSEndpoint(tt.cfg); got != tt.want {
				t.Errorf("resolveTOSEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeTOSHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://tos-cn-beijing.volces.com", "tos-cn-beijing.volces.com"},
		{"http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"tos-cn-guangzhou.volces.com", "tos-cn-guangzhou.volces.com"},
	}

	for _, tt := range tests {
		if got := normalizeTOSHost(tt.input); got != tt.want {
			t.Errorf("normalizeTOSHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTOSProvider_uploadFileInput_CheckpointEnabled(t *testing.T) {
	provider := &tosProvider{
		bucketName: "bucket",
		opts:       UploadOptions{Concurrency: 3, CheckpointDir: t.TempDir()},
	}
	input := provider.uploadFileInput("/tmp/file", "key")
	if !input.EnableCheckpoint {
		t.Fatal("expected checkpoint enabled")
	}
	if input.CheckpointFile == "" {
		t.Fatal("expected checkpoint file path")
	}
}

func TestTOSProvider_uploadFileInput_CheckpointDisabled(t *testing.T) {
	provider := &tosProvider{
		bucketName: "bucket",
		opts:       UploadOptions{Concurrency: 3, DisableCheckpoint: true},
	}
	input := provider.uploadFileInput("/tmp/file", "key")
	if input.EnableCheckpoint {
		t.Fatal("expected checkpoint disabled")
	}
}

func TestTOSProvider_UploadFile_InvalidBucket(t *testing.T) {
	provider := newTestTOSProvider(t, tosMockHandler(t))
	provider.bucketName = ""
	localPath := writeTOSUploadFile(t, "bad.txt", 64)

	_, err := provider.UploadFile(context.Background(), localPath, "bad.txt")
	if err == nil {
		t.Fatal("expected error for empty bucket name")
	}
}

func TestWriteTempFileHelperTOS(t *testing.T) {
	path := writeTempFile(t, "helper-tos.txt", 10)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat temp file: %v", err)
	}
	if info.Size() != 10 {
		t.Errorf("file size = %d, want 10", info.Size())
	}
}
