package utils

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDownloadFile_ToDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network download in short mode")
	}

	saveDir := t.TempDir()
	file, err := DownloadFile("https://gogoshine.com/min.mp4", saveDir)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	defer os.Remove(file)

	if !strings.HasPrefix(file, saveDir) {
		t.Errorf("saved path = %q, want under %q", file, saveDir)
	}
	if filepath.Ext(file) != ".mp4" {
		t.Errorf("ext = %q, want .mp4", filepath.Ext(file))
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("downloaded file size is 0")
	}
}

func TestDownloadFile_ToFilePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network download in short mode")
	}

	savePath := filepath.Join(t.TempDir(), "output.mp4")
	file, err := DownloadFile("https://gogoshine.com/min.mp4", savePath)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	defer os.Remove(file)

	if file != savePath {
		t.Errorf("saved path = %q, want %q", file, savePath)
	}
}

func TestIsSaveDir(t *testing.T) {
	dir := t.TempDir()
	if !isSaveDir(dir) {
		t.Errorf("isSaveDir(%q) = false, want true", dir)
	}
	if isSaveDir(filepath.Join(dir, "file.mp4")) {
		t.Error("file path should not be treated as directory")
	}
	if !isSaveDir(dir + string(os.PathSeparator)) {
		t.Errorf("trailing separator path should be treated as directory")
	}
}

func TestExistingFileSize(t *testing.T) {
	dir := t.TempDir()
	missing, err := existingFileSize(filepath.Join(dir, "missing.bin"))
	if err != nil || missing != 0 {
		t.Fatalf("existingFileSize(missing) = (%d, %v), want (0, nil)", missing, err)
	}

	path := filepath.Join(dir, "part.bin")
	if err := os.WriteFile(path, []byte("12345"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	size, err := existingFileSize(path)
	if err != nil || size != 5 {
		t.Fatalf("existingFileSize() = (%d, %v), want (5, nil)", size, err)
	}
}

func TestDownloadToFile_ResumeFromBreakpoint(t *testing.T) {
	content := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRangeResponse(w, r, content)
	}))
	defer server.Close()

	savePath := filepath.Join(t.TempDir(), "resume.bin")
	if err := os.WriteFile(savePath, content[:10], 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	gotPath, err := DownloadFileWithRetry(server.URL, savePath, 0)
	if err != nil {
		t.Fatalf("DownloadFileWithRetry() error = %v", err)
	}
	if gotPath != savePath {
		t.Fatalf("saved path = %q, want %q", gotPath, savePath)
	}

	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestDownloadToFile_RetryAfterWriteFailure(t *testing.T) {
	content := []byte("retry-download-content-bytes")
	var attempts atomic.Int32

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			attempt := attempts.Add(1)
			offset := parseRangeOffset(req)

			if attempt == 1 && offset == 0 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(&failAfterReader{data: content[:16], err: fmt.Errorf("tls: bad record MAC")}),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}

			body := content[offset:]
			header := make(http.Header)
			status := http.StatusOK
			if offset > 0 {
				status = http.StatusPartialContent
				header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     header,
				Request:    req,
			}, nil
		}),
	}

	savePath := filepath.Join(t.TempDir(), "retry.bin")
	_, err := DownloadFileWithConfig("http://example.com/file", savePath, DownloadConfig{
		MaxRetries: 3,
		Client:     client,
	})
	if err != nil {
		t.Fatalf("DownloadFileWithConfig() error = %v", err)
	}

	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
	if attempts.Load() < 2 {
		t.Fatalf("attempts = %d, want >= 2", attempts.Load())
	}
}

func TestDownloadToFile_MaxRetriesExceeded(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := DownloadFileWithRetry(server.URL, filepath.Join(t.TempDir(), "fail.bin"), 2)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
}

func TestDownloadOnRetryCallback(t *testing.T) {
	var (
		serverAttempts atomic.Int32
		retryCallbacks atomic.Int32
		lastOffset     int64
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serverAttempts.Add(1) < 2 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	savePath := filepath.Join(t.TempDir(), "callback.bin")
	_, err := DownloadFileWithConfig(server.URL, savePath, DownloadConfig{
		MaxRetries: 2,
		OnRetry: func(attempt, maxAttempts int, err error, resumeOffset int64) {
			retryCallbacks.Add(1)
			lastOffset = resumeOffset
		},
	})
	if err != nil {
		t.Fatalf("DownloadFileWithConfig() error = %v", err)
	}
	if retryCallbacks.Load() != 1 {
		t.Fatalf("retry callbacks = %d, want 1", retryCallbacks.Load())
	}
	if lastOffset != 0 {
		t.Fatalf("resume offset = %d, want 0", lastOffset)
	}
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type failAfterReader struct {
	data []byte
	off  int
	err  error
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	if r.off >= len(r.data) {
		return n, r.err
	}
	return n, nil
}

func writeRangeResponse(w http.ResponseWriter, r *http.Request, content []byte) {
	offset := parseRangeOffset(r)
	if offset == 0 && r.Header.Get("Range") == "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
		return
	}
	if offset >= len(content) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(content)-1, len(content)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(content[offset:])
}

func parseRangeOffset(r *http.Request) int {
	rangeHeader := r.Header.Get("Range")
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0
	}
	startStr := strings.TrimPrefix(rangeHeader, "bytes=")
	startStr = strings.TrimSuffix(startStr, "-")
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 0 {
		return 0
	}
	return start
}
