package prepare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

type countingDownloader struct {
	calls atomic.Int32
	delay time.Duration
	err   error
}

func (d *countingDownloader) Download(ctx context.Context, url, dest string) (string, error) {
	d.calls.Add(1)
	if d.delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(d.delay):
		}
	}
	if d.err != nil {
		return "", d.err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, []byte("cached-source"), 0o644)
}

func TestCachingDownloader_SharesSingleDownload(t *testing.T) {
	root := t.TempDir()
	inner := &countingDownloader{delay: 50 * time.Millisecond}
	d := NewCachingDownloader(inner, filepath.Join(root, SourceCacheSubDir), zap.NewNop())

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dest := filepath.Join(root, "task", fmt.Sprintf("%d", i), "source.mp4")
			_, err := d.Download(context.Background(), "https://example.com/live.mp4", dest)
			errs <- err
			if err == nil {
				if _, statErr := os.Stat(dest); statErr != nil {
					errs <- statErr
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Download() error = %v", err)
		}
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner downloads = %d, want 1", got)
	}
}

func TestCachingDownloader_HitCacheSkipsDownload(t *testing.T) {
	root := t.TempDir()
	inner := &countingDownloader{}
	cacheDir := filepath.Join(root, SourceCacheSubDir)
	d := NewCachingDownloader(inner, cacheDir, zap.NewNop())

	dest1 := filepath.Join(root, "t1", "source.mp4")
	if _, err := d.Download(context.Background(), "https://example.com/a.mp4", dest1); err != nil {
		t.Fatalf("first Download() error = %v", err)
	}
	// 人为把缓存目录改成更旧，命中后应刷新 mtime。
	key := sourceCacheKey("https://example.com/a.mp4")
	cacheEntry := filepath.Join(cacheDir, key)
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(cacheEntry, old, old); err != nil {
		t.Fatal(err)
	}

	dest2 := filepath.Join(root, "t2", "source.mp4")
	if _, err := d.Download(context.Background(), "https://example.com/a.mp4", dest2); err != nil {
		t.Fatalf("second Download() error = %v", err)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner downloads = %d, want 1", got)
	}
	info, err := os.Stat(cacheEntry)
	if err != nil {
		t.Fatal(err)
	}
	if info.ModTime().Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("cache entry mtime should be refreshed on hit, got %v", info.ModTime())
	}
}

func TestCachingDownloader_EmptyCacheDirPassthrough(t *testing.T) {
	inner := &countingDownloader{}
	d := NewCachingDownloader(inner, "", zap.NewNop())
	dest := filepath.Join(t.TempDir(), "source.mp4")
	if _, err := d.Download(context.Background(), "https://example.com/a.mp4", dest); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner downloads = %d, want 1", got)
	}
}
