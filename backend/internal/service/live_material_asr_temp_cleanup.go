package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ASRTempDirName 是 ASR 临时文件目录名（相对进程工作目录），与音频预处理使用的 workDir 保持一致。
const ASRTempDirName = defaultTempDirName

// ASRTempCleanupMaxAge ASR 临时文件最长保留时间；超过后由定时任务删除。
const ASRTempCleanupMaxAge = 24 * time.Hour

// CleanupASRTempFiles 删除 dir 下最后修改时间超过 maxAge 的临时文件。
// 只处理普通文件，不递归；目录不存在时视为成功。
func CleanupASRTempFiles(dir string, maxAge time.Duration) (removed int, err error) {
	if strings.TrimSpace(dir) == "" {
		return 0, fmt.Errorf("dir 为空")
	}
	if maxAge <= 0 {
		return 0, fmt.Errorf("maxAge 必须为正：%s", maxAge)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取临时目录失败 %s: %w", dir, readErr)
	}

	deadline := time.Now().Add(-maxAge)
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("读取临时文件信息失败 %s: %w", entry.Name(), infoErr)
			}
			continue
		}
		// 跳过符号链接等非普通文件，避免误删链接目标。
		if !info.Mode().IsRegular() || !info.ModTime().Before(deadline) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if rmErr := os.Remove(path); rmErr != nil {
			if os.IsNotExist(rmErr) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("删除临时文件失败 %s: %w", path, rmErr)
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
