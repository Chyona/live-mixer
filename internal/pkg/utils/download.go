package utils

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DownloadTimeout 单次下载请求超时（0 表示不限制，适用于大文件）。
	DownloadTimeout = 0
	// DefaultDownloadMaxRetries 下载失败后额外重试次数（不含首次请求）。
	DefaultDownloadMaxRetries = 3
)

// DownloadConfig 控制断点续传下载与重试行为。
type DownloadConfig struct {
	// MaxRetries 失败后额外重试次数；小于 0 时按 0 处理。
	MaxRetries int
	// OnRetry 每次失败后、下一次重试前回调；resumeOffset 为本地已保留的已下载字节数。
	OnRetry func(attempt int, maxAttempts int, err error, resumeOffset int64)
	// Client 自定义 HTTP 客户端，主要用于单元测试注入 mock 传输层。
	Client *http.Client
}

func (cfg DownloadConfig) downloadClient() *http.Client {
	if cfg.Client != nil {
		return cfg.Client
	}
	return defaultDownloadClient()
}

// 常用文件类型的魔数签名
var magicNumbers = map[string]string{
	"\xff\xd8\xff":      ".jpg",  // JPEG
	"\x89PNG\r\n\x1a\n": ".png",  // PNG
	"GIF87a":            ".gif",  // GIF 87a
	"GIF89a":            ".gif",  // GIF 89a
	"RIFF....WEBPVP8 ":  ".webp", // WebP
	"\x1a\x45\xdf\xa3":  ".webm", // WebM
	"\x00\x00\x00 ftyp": ".mp4",  // MP4
	"ID3":               ".mp3",  // MP3
	"OggS":              ".ogg",  // OGG
	"FLV":               ".flv",  // FLV
}

// 常用 Content-Type 到扩展名的映射
var contentTypeToExt = map[string]string{
	"image/jpeg":         ".jpg",
	"image/png":          ".png",
	"image/gif":          ".gif",
	"image/webp":         ".webp",
	"video/mp4":          ".mp4",
	"video/webm":         ".webm",
	"video/quicktime":    ".mov",
	"audio/mpeg":         ".mp3",
	"audio/ogg":          ".ogg",
	"audio/wav":          ".wav",
	"audio/webm":         ".weba",
	"application/pdf":    ".pdf",
	"application/zip":    ".zip",
	"application/x-rar":  ".rar",
	"application/x-tar":  ".tar",
	"application/x-gzip": ".gz",
}

func GetFileSizeFromURL(url string) (int64, error) {
	resp, err := http.Head(url)
	if err != nil {
		return 0, fmt.Errorf("get file info from %s failed, err: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("get file info from %s failed, status code: %d", url, resp.StatusCode)
	}

	contentLength := resp.Header.Get("Content-Length")
	if contentLength == "" {
		return 0, fmt.Errorf("get file size from %s failed, Content-Length header not found", url)
	}

	fileSize, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse file size from %s failed, err: %v", url, err)
	}

	return fileSize, nil
}

func defaultDownloadClient() *http.Client {
	return &http.Client{Timeout: DownloadTimeout}
}

// DownloadFile 从 url 下载文件到 dest，支持断点续传与失败重试。
// dest 为目录（已存在或以路径分隔符结尾）时自动生成唯一文件名；
// 否则将内容写入 dest 指定的文件路径。返回实际保存路径。
func DownloadFile(urlStr, dest string) (string, error) {
	return DownloadFileWithConfig(urlStr, dest, DownloadConfig{
		MaxRetries: DefaultDownloadMaxRetries,
	})
}

// DownloadFileWithRetry 使用指定重试次数下载文件，默认启用断点续传。
func DownloadFileWithRetry(urlStr, dest string, maxRetries int) (string, error) {
	return DownloadFileWithConfig(urlStr, dest, DownloadConfig{MaxRetries: maxRetries})
}

// DownloadFileWithConfig 按配置下载文件；固定文件路径时支持断点续传，目录模式每次失败后全量重试。
func DownloadFileWithConfig(urlStr, dest string, cfg DownloadConfig) (string, error) {
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	maxAttempts := 1 + maxRetries

	if isSaveDir(dest) {
		return downloadToDirectoryWithRetry(urlStr, dest, maxAttempts, cfg)
	}
	return downloadToFileWithRetry(urlStr, dest, maxAttempts, cfg)
}

// downloadToFileWithRetry 下载到固定文件路径，失败后保留局部文件并从断点续传。
func downloadToFileWithRetry(urlStr, savePath string, maxAttempts int, cfg DownloadConfig) (string, error) {
	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return "", fmt.Errorf("create directory failed: %v", err)
	}

	client := cfg.downloadClient()
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = downloadOnceToFile(client, urlStr, savePath)
		if lastErr == nil {
			return savePath, nil
		}
		if attempt < maxAttempts {
			offset, _ := existingFileSize(savePath)
			if cfg.OnRetry != nil {
				cfg.OnRetry(attempt, maxAttempts, lastErr, offset)
			}
		}
	}
	return "", fmt.Errorf("download from %s failed after %d attempts: %w", urlStr, maxAttempts, lastErr)
}

// downloadOnceToFile 执行一次下载尝试；若本地已有部分数据则通过 Range 请求续传。
func downloadOnceToFile(client *http.Client, urlStr, savePath string) error {
	offset, err := existingFileSize(savePath)
	if err != nil {
		return err
	}

	for restartFull := 0; restartFull < 2; restartFull++ {
		req, err := http.NewRequest(http.MethodGet, urlStr, nil)
		if err != nil {
			return fmt.Errorf("create request failed: %v", err)
		}
		req.Header.Set("User-Agent", "jcaigc/1.0")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download from %s failed: %v", urlStr, err)
		}

		err = writeDownloadResponse(resp, savePath, offset)
		resp.Body.Close()
		if err == nil {
			return nil
		}
		if err == errRestartFullDownload {
			offset = 0
			continue
		}
		return err
	}
	return fmt.Errorf("download from %s failed: server does not support resume", urlStr)
}

var errRestartFullDownload = fmt.Errorf("restart full download")

// writeDownloadResponse 根据响应状态将内容写入本地文件，并在需要时触发全量重下。
func writeDownloadResponse(resp *http.Response, savePath string, offset int64) error {
	switch resp.StatusCode {
	case http.StatusOK:
		if offset > 0 {
			_ = os.Remove(savePath)
			return errRestartFullDownload
		}
	case http.StatusPartialContent:
		// 断点续传成功
	case http.StatusRequestedRangeNotSatisfiable:
		if offset > 0 {
			return nil
		}
		return fmt.Errorf("download failed, status code: %d", resp.StatusCode)
	default:
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("download failed, status code: %d", resp.StatusCode)
		}
	}

	file, err := openDownloadFile(savePath, offset)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err = io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write file %s failed: %v", savePath, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync file %s failed: %v", savePath, err)
	}
	return nil
}

// openDownloadFile 打开或创建下载目标文件；offset 为 0 时截断重建，否则在末尾追加。
func openDownloadFile(savePath string, offset int64) (*os.File, error) {
	if offset == 0 {
		file, err := os.Create(savePath)
		if err != nil {
			return nil, fmt.Errorf("create file %s failed: %v", savePath, err)
		}
		return file, nil
	}

	file, err := os.OpenFile(savePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open file %s failed: %v", savePath, err)
	}
	return file, nil
}

// existingFileSize 返回本地已存在文件的大小；文件不存在时返回 0。
func existingFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat file %s failed: %v", path, err)
	}
	if info.IsDir() {
		return 0, fmt.Errorf("path %s is a directory", path)
	}
	return info.Size(), nil
}

// downloadToDirectoryWithRetry 下载到目录；目录模式无法稳定断点续传，失败后删除半成品并全量重试。
func downloadToDirectoryWithRetry(urlStr, dest string, maxAttempts int, cfg DownloadConfig) (string, error) {
	var (
		savePath string
		lastErr  error
	)
	client := cfg.downloadClient()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if savePath != "" {
			_ = os.Remove(savePath)
		}
		savePath, lastErr = downloadOnceToDirectory(client, urlStr, dest)
		if lastErr == nil {
			return savePath, nil
		}
		if attempt < maxAttempts && cfg.OnRetry != nil {
			cfg.OnRetry(attempt, maxAttempts, lastErr, 0)
		}
	}
	return "", fmt.Errorf("download from %s failed after %d attempts: %w", urlStr, maxAttempts, lastErr)
}

// downloadOnceToDirectory 执行一次目录下载（非断点续传）。
func downloadOnceToDirectory(client *http.Client, urlStr, dest string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("User-Agent", "jcaigc/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download from %s failed: %v", urlStr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download from %s failed, status code: %d", urlStr, resp.StatusCode)
	}

	peekBytes := make([]byte, 512)
	n, _ := io.ReadFull(resp.Body, peekBytes)
	peekBytes = peekBytes[:n]
	respBody := io.MultiReader(bytes.NewReader(peekBytes), resp.Body)

	savePath, err := resolveSavePath(dest, urlStr, resp.Header.Get("Content-Type"), peekBytes)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		return "", fmt.Errorf("create directory failed: %v", err)
	}

	file, err := os.Create(savePath)
	if err != nil {
		return "", fmt.Errorf("create file %s failed: %v", savePath, err)
	}
	defer file.Close()

	if _, err = io.Copy(file, respBody); err != nil {
		os.Remove(savePath)
		return "", fmt.Errorf("write file %s failed: %v", savePath, err)
	}

	if err := file.Sync(); err != nil {
		os.Remove(savePath)
		return "", fmt.Errorf("sync file %s failed: %v", savePath, err)
	}

	return savePath, nil
}

func resolveSavePath(dest, urlStr, contentType string, peekBytes []byte) (string, error) {
	if isSaveDir(dest) {
		ext, err := getFileExtension(contentType, urlStr, peekBytes)
		if err != nil {
			return "", fmt.Errorf("determine file extension failed: %v", err)
		}
		return filepath.Join(dest, generateUniqueFileName(ext)), nil
	}
	return dest, nil
}

func isSaveDir(dest string) bool {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return false
	}
	if strings.HasSuffix(dest, string(os.PathSeparator)) || strings.HasSuffix(dest, "/") {
		return true
	}
	info, err := os.Stat(dest)
	return err == nil && info.IsDir()
}

func generateUniqueFileName(ext string) string {
	now := time.Now().UnixNano()
	randBytes := make([]byte, 6)
	rand.Read(randBytes)
	return fmt.Sprintf("%d_%s%s", now, hex.EncodeToString(randBytes), ext)
}

func getFileExtension(contentType, urlStr string, peekBytes []byte) (string, error) {
	for magic, ext := range magicNumbers {
		if len(peekBytes) >= len(magic) && bytes.HasPrefix(peekBytes, []byte(magic)) {
			return ext, nil
		}
	}

	if contentType != "" {
		if i := strings.Index(contentType, ";"); i != -1 {
			contentType = contentType[:i]
		}

		if ext, ok := contentTypeToExt[contentType]; ok {
			return ext, nil
		}

		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			for _, ext := range exts {
				if ext == ".jpeg" {
					return ".jpg", nil
				}
				if ext == ".mpeg" {
					return ".mp3", nil
				}
				if len(ext) == 4 && ext[0] == '.' {
					return ext, nil
				}
			}
			return exts[0], nil
		}
	}

	if parsed, err := url.Parse(urlStr); err == nil {
		if urlExt := filepath.Ext(parsed.Path); urlExt != "" {
			urlExt = strings.ToLower(urlExt)
			if len(urlExt) > 4 {
				urlExt = urlExt[:4]
			}
			return urlExt, nil
		}
	}

	if len(peekBytes) > 0 {
		switch {
		case bytes.HasPrefix(peekBytes, []byte("\xff\xd8\xff")):
			return ".jpg", nil
		case bytes.HasPrefix(peekBytes, []byte("\x89PNG")):
			return ".png", nil
		case bytes.HasPrefix(peekBytes, []byte("GIF")):
			return ".gif", nil
		case bytes.HasPrefix(peekBytes, []byte("RIFF")):
			return ".webp", nil
		case bytes.HasPrefix(peekBytes, []byte("\x1a\x45")):
			return ".webm", nil
		case bytes.HasPrefix(peekBytes, []byte("ID3")):
			return ".mp3", nil
		}
	}

	return ".bin", nil
}
