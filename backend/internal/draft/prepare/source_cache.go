package prepare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// SourceCacheSubDir staging 下直播源共享缓存目录名；CleanupStaging 会跳过它。
const SourceCacheSubDir = "source_cache"

// CachingDownloader 按 live_url 共享下载大文件：同 URL 只下载一次，再硬链接/拷贝到任务目录。
// 避免一键成片批量任务并发重复下载同一场 10GB+ 直播源，导致带宽打满、90 分钟超时卡死。
type CachingDownloader struct {
	Inner    FileDownloader
	CacheDir string
	Logger   *zap.Logger
	group    singleflight.Group
}

// NewCachingDownloader 包装底层下载器；cacheDir 为空时退化为直接下载到目标路径。
func NewCachingDownloader(inner FileDownloader, cacheDir string, logger *zap.Logger) *CachingDownloader {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &CachingDownloader{
		Inner:    inner,
		CacheDir: strings.TrimSpace(cacheDir),
		Logger:   logger,
	}
}

// Download 先确保缓存中有完整源文件，再落到 dest。
func (d *CachingDownloader) Download(ctx context.Context, url, dest string) (string, error) {
	if d == nil || d.Inner == nil {
		return "", fmt.Errorf("下载器未配置")
	}
	if d.CacheDir == "" {
		return d.Inner.Download(ctx, url, dest)
	}

	cachePath, err := d.ensureCached(ctx, url)
	if err != nil {
		return "", err
	}
	if err := materializeCachedSource(cachePath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func (d *CachingDownloader) ensureCached(ctx context.Context, url string) (string, error) {
	key := sourceCacheKey(url)
	cachePath := filepath.Join(d.CacheDir, key, "source.mp4")
	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
		d.Logger.Info("命中直播源缓存",
			zap.String("url", url),
			zap.String("cache_path", cachePath),
			zap.Int64("size", st.Size()),
		)
		return cachePath, nil
	}

	v, err, _ := d.group.Do(key, func() (any, error) {
		if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
			return cachePath, nil
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			return "", fmt.Errorf("创建源缓存目录失败: %w", err)
		}
		// 下载到临时文件再原子改名，避免并发读到半成品。
		tmpPath := cachePath + ".partial"
		_ = os.Remove(tmpPath)
		d.Logger.Info("开始下载直播源到共享缓存",
			zap.String("url", url),
			zap.String("cache_path", cachePath),
		)
		if _, err := d.Inner.Download(ctx, url, tmpPath); err != nil {
			_ = os.Remove(tmpPath)
			return "", err
		}
		if err := os.Rename(tmpPath, cachePath); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("提交源缓存失败: %w", err)
		}
		st, _ := os.Stat(cachePath)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		d.Logger.Info("直播源已写入共享缓存",
			zap.String("url", url),
			zap.String("cache_path", cachePath),
			zap.Int64("size", size),
		)
		return cachePath, nil
	})
	if err != nil {
		return "", err
	}
	path, ok := v.(string)
	if !ok || path == "" {
		return "", fmt.Errorf("源缓存结果异常")
	}
	// 等待 singleflight 期间 ctx 可能已取消；仍返回已就绪的缓存路径供调用方决定。
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return path, nil
}

func sourceCacheKey(url string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(url)))
	return hex.EncodeToString(sum[:16])
}

// materializeCachedSource 优先硬链接，失败则拷贝到任务目录。
func materializeCachedSource(cachePath, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("创建任务暂存目录失败: %w", err)
	}
	_ = os.Remove(dest)
	if err := os.Link(cachePath, dest); err == nil {
		return nil
	}
	src, err := os.Open(cachePath)
	if err != nil {
		return fmt.Errorf("打开源缓存失败: %w", err)
	}
	defer src.Close()

	tmp := dest + ".copying"
	_ = os.Remove(tmp)
	dst, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建任务源文件失败: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("拷贝源缓存失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("关闭任务源文件失败: %w", closeErr)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("提交任务源文件失败: %w", err)
	}
	return nil
}
