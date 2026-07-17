package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"live-mixer/pkg/utils/urlrewrite"
)

func TestLoggingResumableDownloader_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("download-ok"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "ok.bin")
	logger := zaptest.NewLogger(t)
	downloader := NewFileDownloader(logger, nil)

	got, err := downloader.Download(server.URL, dest)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got != dest {
		t.Fatalf("path = %q, want %q", got, dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "download-ok" {
		t.Fatalf("content = %q, want download-ok", data)
	}
}

func TestLoggingResumableDownloader_RetryUsesResume(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "retry.bin")
	downloader := NewFileDownloader(zap.NewNop(), nil)

	_, err := downloader.Download(server.URL, dest)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	// 第一次返回 500 触发重试，第二次全量下载成功。
	if string(got) != "done" {
		t.Fatalf("content = %q, want done", got)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
}

type mockInnerDownloader struct {
	lastURL string
}

func (m *mockInnerDownloader) Download(url, dest string) (string, error) {
	m.lastURL = url
	return dest, nil
}

func TestRewritingDownloader_RewritesURL(t *testing.T) {
	inner := &mockInnerDownloader{}
	rewriter := urlrewrite.New(urlrewrite.Options{
		Exact: map[string]string{
			"arkclaw-wxbpd.tos-cn-shanghai.volces.com": "arkclaw-wxbpd.tos-cn-shanghai.ivolces.com",
		},
	})
	d := rewritingDownloader{inner: inner, rewriter: rewriter, logger: zap.NewNop()}

	raw := "https://arkclaw-wxbpd.tos-cn-shanghai.volces.com/live/source.mp4"
	dest := filepath.Join(t.TempDir(), "source.mp4")
	if _, err := d.Download(raw, dest); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	want := "https://arkclaw-wxbpd.tos-cn-shanghai.ivolces.com/live/source.mp4"
	if inner.lastURL != want {
		t.Fatalf("inner URL = %q, want %q", inner.lastURL, want)
	}
}
