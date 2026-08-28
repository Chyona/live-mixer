// Package webroot 负责任务本地暂存目录路径（切片落盘、capcut-mate 请求记录、ASR 调试落盘）。
// 切片对外访问 URL 由对象存储上传提供，不再依赖 WEB_ROOT_URL 映射。
package webroot

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ASRStagingSubDir staging 下 ASR 调试落盘的保留子目录名；CleanupStaging 会跳过它。
const ASRStagingSubDir = "asr"

// SourceCacheSubDir staging 下直播源共享缓存目录名；CleanupStaging 会跳过它。
const SourceCacheSubDir = "source_cache"

// Config 本地暂存根目录配置。
type Config struct {
	// RootDir 本地暂存根目录，例如 D:\code\GitHub\live-mixer\docker\html
	RootDir string
}

// StagingDir 返回某任务的暂存目录：{RootDir}/staging/{taskID}。
func (c Config) StagingDir(taskID string) string {
	return filepath.Join(c.RootDir, "staging", taskID)
}

// CapCutMateRecordDir 返回 capcut-mate 请求/响应落盘目录。
func (c Config) CapCutMateRecordDir(taskID string) string {
	return filepath.Join(c.StagingDir(taskID), "capcut_mate")
}

// ASRStagingDir 返回某次 ASR 运行的调试落盘目录：{RootDir}/staging/asr/{materialID}-v{asrVersion}。
// RootDir 为空时返回空字符串（调用方应视为 noop）。
func (c Config) ASRStagingDir(materialID uint, asrVersion int64) string {
	if strings.TrimSpace(c.RootDir) == "" {
		return ""
	}
	return filepath.Join(
		c.RootDir,
		"staging",
		ASRStagingSubDir,
		fmt.Sprintf("%d-v%d", materialID, asrVersion),
	)
}

// SourceCacheDir 返回直播源共享缓存根目录：{RootDir}/staging/source_cache。
// RootDir 为空时返回空字符串（调用方应退化为直接下载）。
func (c Config) SourceCacheDir() string {
	if strings.TrimSpace(c.RootDir) == "" {
		return ""
	}
	return filepath.Join(c.RootDir, "staging", SourceCacheSubDir)
}
