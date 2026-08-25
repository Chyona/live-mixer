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

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// newTestOSSProvider 创建指向 mock HTTP 服务的 OSS 后端，用于单元测试。
func newTestOSSProvider(t *testing.T, handler http.HandlerFunc) *ossProvider {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	endpoint := strings.TrimPrefix(server.URL, "http://")
	client, err := oss.New(endpoint, "test-access-key-id", "test-access-key-secret")
	if err != nil {
		t.Fatalf("oss.New() error = %v", err)
	}

	bucket, err := client.Bucket("test-bucket")
	if err != nil {
		t.Fatalf("client.Bucket() error = %v", err)
	}

	return newOSSProviderWithBucket(bucket, "test-bucket", endpoint, UploadOptions{
		PartSizeBytes:     100 * 1024,
		Concurrency:       2,
		CheckpointDir:     t.TempDir(),
		DisableCheckpoint: true,
	}, 0)
}

// ossObjectKey 从请求路径中提取对象键名（兼容 path-style：/bucket/object）。
func ossObjectKey(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "test-bucket/")
	return path
}

// ossMockHandler 模拟 OSS 分片上传接口。
func ossMockHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		key := ossObjectKey(r)

		// 初始化分片上传
		if r.Method == http.MethodPost && query.Has("uploads") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult>
    <Bucket>test-bucket</Bucket>
    <Key>%s</Key>
    <UploadId>test-upload-id</UploadId>
</InitiateMultipartUploadResult>`, key)
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
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult>
    <Location>https://test-bucket.oss.local/done</Location>
    <Bucket>test-bucket</Bucket>
    <Key>done</Key>
    <ETag>"etag"</ETag>
</CompleteMultipartUploadResult>`)
			return
		}

		t.Errorf("unexpected OSS request: %s %s", r.Method, r.URL.String())
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestOSSProvider_UploadFile_Simple(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))
	localPath := writeTempFile(t, "small.txt", 512)

	url, err := provider.UploadFile(context.Background(), localPath, "small.txt")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	assertPresignedURL(t, url, "small.txt")
}

func TestOSSProvider_UploadFile_Multipart(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))
	localPath := writeTempFile(t, "large.bin", 300*1024)

	url, err := provider.UploadFile(context.Background(), localPath, "large.bin")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	assertPresignedURL(t, url, "large.bin")
}

func TestOSSProvider_UploadFile_FileNotFound(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))

	_, err := provider.UploadFile(context.Background(), filepath.Join(t.TempDir(), "missing.txt"), "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestOSSProvider_UploadReader_Small(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))

	url, err := provider.UploadReader(context.Background(), strings.NewReader("hello oss"), "reader.txt", 9)
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	assertPresignedURL(t, url, "reader.txt")
}

func TestOSSProvider_UploadReader_LargeUsesMultipart(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))
	data := strings.Repeat("x", 300*1024)

	url, err := provider.UploadReader(context.Background(), strings.NewReader(data), "large-reader.bin", int64(len(data)))
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	assertPresignedURL(t, url, "large-reader.bin")
}

func TestOSSProvider_Type(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))
	if provider.Type() != ProviderOSS {
		t.Errorf("Type() = %q, want %q", provider.Type(), ProviderOSS)
	}
}

func TestOSSCheckpointDir(t *testing.T) {
	customDir := t.TempDir()
	got := ossCheckpointDir(UploadOptions{CheckpointDir: customDir})
	want := filepath.Join(customDir, "oss-checkpoint")
	if got != want {
		t.Errorf("ossCheckpointDir() = %q, want %q", got, want)
	}
}

func TestOSSProvider_UploadFile_CancelledContext(t *testing.T) {
	provider := newTestOSSProvider(t, ossMockHandler(t))
	localPath := writeTempFile(t, "cancel.txt", 128)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.UploadFile(ctx, localPath, "cancel.txt")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestNewOSSProvider_EmptyBucket(t *testing.T) {
	_, err := newOSSProvider(OSSConfig{
		AccessKeyID:     "id",
		AccessKeySecret: "secret",
		BucketName:      "",
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
	}, UploadOptions{}, 0)
	if err == nil {
		t.Fatal("expected error for empty bucket name")
	}
}

func TestOSSProvider_objectURL(t *testing.T) {
	provider := &ossProvider{bucketName: "my-bucket", endpoint: "oss-cn-hangzhou.aliyuncs.com"}
	got := provider.objectURL("path/to/file.jpg")
	want := "https://my-bucket.oss-cn-hangzhou.aliyuncs.com/path/to/file.jpg"
	if got != want {
		t.Errorf("objectURL() = %q, want %q", got, want)
	}
}

func TestOSSProvider_uploadOptions_CheckpointEnabled(t *testing.T) {
	provider := &ossProvider{opts: UploadOptions{Concurrency: 3}}
	opts := provider.uploadOptions()
	if len(opts) != 2 {
		t.Fatalf("expected 2 options with checkpoint enabled, got %d", len(opts))
	}
}

func TestOSSProvider_uploadOptions_CheckpointDisabled(t *testing.T) {
	provider := &ossProvider{opts: UploadOptions{Concurrency: 3, DisableCheckpoint: true}}
	opts := provider.uploadOptions()
	if len(opts) != 1 {
		t.Fatalf("expected 1 option with checkpoint disabled, got %d", len(opts))
	}
}

func TestWriteTempFileHelper(t *testing.T) {
	path := writeTempFile(t, "helper.txt", 10)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat temp file: %v", err)
	}
	if info.Size() != 10 {
		t.Errorf("file size = %d, want 10", info.Size())
	}
}
