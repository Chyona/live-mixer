package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// newTestCOSProvider 创建指向 mock HTTP 服务的 COS 后端，用于单元测试。
func newTestCOSProvider(t *testing.T, handler http.HandlerFunc) *cosProvider {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	bucketURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  "test-secret-id",
			SecretKey: "test-secret-key",
		},
	})
	client.Conf.EnableCRC = false

	return newCOSProviderWithClient(client, "test-bucket", "ap-guangzhou", UploadOptions{
		PartSizeMB:        1,
		Concurrency:       2,
		CheckpointDir:     t.TempDir(),
		DisableCheckpoint: true,
	}, 0)
}

// cosMockHandler 模拟 COS 简单上传与分片上传接口。
func cosMockHandler(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		// 查询未完成的分片上传（断点续传检查）
		if r.Method == http.MethodGet && query.Has("uploads") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListMultipartUploadsResult>
    <Bucket>test-bucket</Bucket>
    <Encoding-Type>url</Encoding-Type>
</ListMultipartUploadsResult>`)
			return
		}

		// 简单上传
		if r.Method == http.MethodPut && query.Get("uploads") == "" && query.Get("uploadId") == "" {
			w.Header().Set("ETag", `"simple-etag"`)
			w.WriteHeader(http.StatusOK)
			return
		}

		// 初始化分片上传
		if r.Method == http.MethodPost && query.Has("uploads") {
			key := strings.TrimPrefix(r.URL.Path, "/")
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
		if r.Method == http.MethodPut && query.Get("uploadId") != "" {
			w.Header().Set("ETag", fmt.Sprintf(`"etag-part-%s"`, query.Get("partNumber")))
			w.WriteHeader(http.StatusOK)
			return
		}

		// 完成分片上传
		if r.Method == http.MethodPost && query.Get("uploadId") != "" {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult>
    <Location>https://test-bucket.cos.ap-guangzhou.myqcloud.com/done</Location>
    <Bucket>test-bucket</Bucket>
    <Key>done</Key>
    <ETag>"etag"</ETag>
</CompleteMultipartUploadResult>`)
			return
		}

		t.Errorf("unexpected COS request: %s %s", r.Method, r.URL.String())
		w.WriteHeader(http.StatusNotFound)
	}
}

func writeTempFile(t *testing.T, name string, size int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	data := make([]byte, size)
	for i := range data {
		data[i] = byte('a' + (i % 26))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func assertPresignedURL(t *testing.T, got, objectKey string) {
	t.Helper()
	if !strings.Contains(got, objectKey) {
		t.Errorf("url = %q, want to contain object key %q", got, objectKey)
	}
	if !strings.Contains(got, "?") {
		t.Errorf("url = %q, want presigned URL with query string", got)
	}
}

func TestCOSProvider_UploadFile_Simple(t *testing.T) {
	provider := newTestCOSProvider(t, cosMockHandler(t))
	localPath := writeTempFile(t, "small.txt", 1024)

	url, err := provider.UploadFile(context.Background(), localPath, "small.txt")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	assertPresignedURL(t, url, "small.txt")
}

func TestCOSProvider_UploadFile_Multipart(t *testing.T) {
	provider := newTestCOSProvider(t, cosMockHandler(t))
	// 2MB 文件，配合 1MB 分片大小触发分片上传
	localPath := writeTempFile(t, "large.bin", 2*1024*1024)

	url, err := provider.UploadFile(context.Background(), localPath, "large.bin")
	if err != nil {
		t.Fatalf("UploadFile() error = %v", err)
	}
	assertPresignedURL(t, url, "large.bin")
}

func TestCOSProvider_UploadFile_FileNotFound(t *testing.T) {
	provider := newTestCOSProvider(t, cosMockHandler(t))

	_, err := provider.UploadFile(context.Background(), filepath.Join(t.TempDir(), "missing.txt"), "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCOSProvider_UploadReader(t *testing.T) {
	provider := newTestCOSProvider(t, cosMockHandler(t))

	url, err := provider.UploadReader(context.Background(), strings.NewReader("hello cos"), "reader.txt", 9)
	if err != nil {
		t.Fatalf("UploadReader() error = %v", err)
	}
	assertPresignedURL(t, url, "reader.txt")
}

func TestCOSProvider_Type(t *testing.T) {
	provider := newTestCOSProvider(t, cosMockHandler(t))
	if provider.Type() != ProviderCOS {
		t.Errorf("Type() = %q, want %q", provider.Type(), ProviderCOS)
	}
}

func TestCOSProvider_objectURL(t *testing.T) {
	provider := &cosProvider{bucketName: "my-bucket", region: "ap-beijing"}
	got := provider.objectURL("path/to/file.jpg")
	want := "https://my-bucket.cos.ap-beijing.myqcloud.com/path/to/file.jpg"
	if got != want {
		t.Errorf("objectURL() = %q, want %q", got, want)
	}
}
