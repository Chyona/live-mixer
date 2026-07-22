// Package webroot 负责任务本地暂存目录路径（切片落盘、capcut-mate 请求记录）。
// 切片对外访问 URL 由对象存储上传提供，不再依赖 WEB_ROOT_URL 映射。
package webroot

import (
	"path/filepath"
)

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
