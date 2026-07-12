package util

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

	"github.com/gogf/gf/v2/os/glog"
)

const (
	// 定义文件大小限制
	MaxFileSize     = 100 * 1024 * 1024
	DownloadTimeout = 30 * time.Minute
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
	// 发起 HTTP GET 请求（使用 HEAD 方法获取文件信息）
	resp, err := http.Head(url)
	if err != nil {
		return 0, fmt.Errorf("get file info from %s failed, err: %v", url, err)
	}
	defer resp.Body.Close()

	// 检查 HTTP 响应状态码
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("get file info from %s failed, status code: %d", url, resp.StatusCode)
	}

	// 获取文件大小
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

// 从网络下载文件并保存到指定路径
func DownloadFileFromURL(url string, savePath string) error {
	fileSize, err := GetFileSizeFromURL(url)
	if err != nil {
		glog.Warningf(nil, "get file size from %s failed, err: %v", url, err)
		fileSize = 0 // 如果获取不到文件大小，就默认为文件合格，不检查大小
	}

	// 检查文件大小是否超过限制
	if fileSize > MaxFileSize {
		return fmt.Errorf("file from %s is too large, size: %d bytes, exceeding %dMB limit", url, fileSize, MaxFileSize/(1024*1024))
	}

	// 如果文件大小检查通过，再发起 GET 请求下载文件
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download from %s failed, err: %v", url, err)
	}
	defer resp.Body.Close()

	// 检查 HTTP 响应状态码
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download from %s failed, status code: %d", url, resp.StatusCode)
	}

	// 创建文件保存路径
	file, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file failed, err: %v", err)
	}
	defer file.Close()

	// 将响应体的内容写入文件
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("write file %s failed, err: %v", savePath, err)
	}

	return nil
}

func DownloadFile(url string, savePath string) error {
	// 创建带超时的HTTP客户端
	client := &http.Client{Timeout: DownloadTimeout}

	// 创建请求并设置 User-Agent
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("User-Agent", "jcaigc/1.0")

	// 发起请求
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download from %s failed: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download from %s failed, status code: %d", url, resp.StatusCode)
	}

	// 创建文件（如果文件已存在会被覆盖）
	file, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("create file %s failed: %v", savePath, err)
	}

	// 使用defer确保文件关闭
	defer file.Close()

	// 将响应体内容写入文件
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		// 删除不完整的文件
		os.Remove(savePath)
		return fmt.Errorf("write file %s failed: %v", savePath, err)
	}

	// 确保文件内容刷新到磁盘
	if err := file.Sync(); err != nil {
		// 删除可能不完整的文件
		os.Remove(savePath)
		return fmt.Errorf("sync file %s failed: %v", savePath, err)
	}

	return nil
}

// @brief 下载文件
// @param urlStr 下载链接
// @param saveDir 保存目录
// @return 保存路径, 扩展名（示例：.jpg）, 错误
func DownloadFilePlus(urlStr string, saveDir string) (string, string, error) {
	// 创建带超时的HTTP客户端
	client := &http.Client{Timeout: DownloadTimeout}

	// 创建请求并设置 User-Agent
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request failed: %v", err)
	}
	req.Header.Set("User-Agent", "jcaigc/1.0")

	// 发起请求
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download from %s failed: %v", urlStr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download failed, status code: %d", resp.StatusCode)
	}

	// 读取前512字节用于文件类型检测
	peekBytes := make([]byte, 512)
	n, _ := io.ReadFull(resp.Body, peekBytes)
	peekBytes = peekBytes[:n]

	// 创建可以重新读取前512字节的响应体
	respBody := io.MultiReader(bytes.NewReader(peekBytes), resp.Body)

	// 智能获取文件扩展名
	ext, err := getFileExtension(resp.Header.Get("Content-Type"), urlStr, peekBytes)
	if err != nil {
		return "", "", fmt.Errorf("determine file extension failed: %v", err)
	}

	// 生成唯一文件名
	fileName := generateUniqueFileName(ext)

	// 确保目录存在
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", "", fmt.Errorf("create directory failed: %v", err)
	}

	// 构建完整保存路径
	savePath := filepath.Join(saveDir, fileName)

	// 创建文件
	file, err := os.Create(savePath)
	if err != nil {
		return "", "", fmt.Errorf("create file %s failed: %v", savePath, err)
	}
	defer file.Close()

	// 将响应体内容写入文件
	_, err = io.Copy(file, respBody)
	if err != nil {
		os.Remove(savePath)
		return "", "", fmt.Errorf("write file failed: %v", err)
	}

	// 确保文件内容刷新到磁盘
	if err := file.Sync(); err != nil {
		os.Remove(savePath)
		return "", "", fmt.Errorf("sync file failed: %v", err)
	}

	return savePath, ext, nil
}

// 生成唯一文件名（时间戳+随机数）
func generateUniqueFileName(ext string) string {
	// 获取当前时间（精确到纳秒）
	now := time.Now().UnixNano()

	// 生成6字节的随机数
	randBytes := make([]byte, 6)
	rand.Read(randBytes)

	// 组合成唯一文件名：时间戳_随机数.扩展名
	return fmt.Sprintf("%d_%s%s", now, hex.EncodeToString(randBytes), ext)
}

// 智能获取文件扩展名
func getFileExtension(contentType, urlStr string, peekBytes []byte) (string, error) {
	// 1. 首先尝试从魔数签名检测文件类型
	for magic, ext := range magicNumbers {
		if len(peekBytes) >= len(magic) && bytes.HasPrefix(peekBytes, []byte(magic)) {
			return ext, nil
		}
	}

	// 2. 尝试从Content-Type获取扩展名
	if contentType != "" {
		// 清理Content-Type参数
		if i := strings.Index(contentType, ";"); i != -1 {
			contentType = contentType[:i]
		}

		// 使用自定义映射
		if ext, ok := contentTypeToExt[contentType]; ok {
			return ext, nil
		}

		// 使用标准库作为备选
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			// 优先选择常用扩展名
			for _, ext := range exts {
				if ext == ".jpeg" {
					return ".jpg", nil
				}
				if ext == ".mpeg" {
					return ".mp3", nil
				}
				if len(ext) == 4 && ext[0] == '.' { // .mp3, .mp4 等
					return ext, nil
				}
			}
			return exts[0], nil
		}
	}

	// 3. 从URL路径中提取扩展名
	if parsed, err := url.Parse(urlStr); err == nil {
		if urlExt := filepath.Ext(parsed.Path); urlExt != "" {
			// 规范化扩展名
			urlExt = strings.ToLower(urlExt)
			if len(urlExt) > 4 { // 限制过长扩展名
				urlExt = urlExt[:4]
			}
			return urlExt, nil
		}
	}

	// 4. 根据内容特征猜测类型
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

	// 5. 最终回退
	return ".bin", nil
}
