// Package webroot 负责任务暂存目录与「本地路径 ↔ 公网 URL」互转。
// capcut-mate 只能拉取 HTTP(S) URL，因此切片落地到 WEB_ROOT_DIR 后需映射为 WEB_ROOT_URL。
package webroot

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// Config WEB 静态根目录与对应访问 URL。
type Config struct {
	// RootDir 本地静态文件根目录，例如 D:\code\GitHub\live-mixer\docker\html
	RootDir string
	// RootURL 对外访问前缀，例如 http://192.168.3.219:81
	RootURL string
}

// StagingDir 返回某任务的暂存目录：{RootDir}/staging/{taskID}。
func (c Config) StagingDir(taskID string) string {
	return filepath.Join(c.RootDir, "staging", taskID)
}

// CapCutMateRecordDir 返回 capcut-mate 请求/响应落盘目录。
func (c Config) CapCutMateRecordDir(taskID string) string {
	return filepath.Join(c.StagingDir(taskID), "capcut_mate")
}

// LocalPathToURL 将 WEB_ROOT_DIR 下的本地绝对/相对路径转为可被 capcut-mate 访问的 URL。
// 例如 RootDir=.../html、文件=.../html/staging/1/a.mp4 → {RootURL}/staging/1/a.mp4
func (c Config) LocalPathToURL(localPath string) (string, error) {
	rootDir := strings.TrimSpace(c.RootDir)
	rootURL := strings.TrimRight(strings.TrimSpace(c.RootURL), "/")
	if rootDir == "" {
		return "", fmt.Errorf("WEB_ROOT_DIR 未配置")
	}
	if rootURL == "" {
		return "", fmt.Errorf("WEB_ROOT_URL 未配置")
	}

	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return "", fmt.Errorf("解析本地路径失败: %w", err)
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("解析 WEB_ROOT_DIR 失败: %w", err)
	}

	rel, err := filepath.Rel(absRoot, absLocal)
	if err != nil {
		return "", fmt.Errorf("计算相对路径失败: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("文件不在 WEB_ROOT_DIR 内: %s", localPath)
	}

	// URL 路径一律使用正斜杠；并对每段做 PathEscape，避免空格等特殊字符。
	parts := strings.Split(filepath.ToSlash(rel), "/")
	escaped := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		escaped = append(escaped, url.PathEscape(p))
	}
	return rootURL + "/" + path.Join(escaped...), nil
}
