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
)

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

// DownloadFile 从 url 下载文件到 dest，不限制文件大小。
// dest 为目录（已存在或以路径分隔符结尾）时自动生成唯一文件名；
// 否则将内容写入 dest 指定的文件路径。返回实际保存路径。
func DownloadFile(urlStr, dest string) (string, error) {
	client := defaultDownloadClient()

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
