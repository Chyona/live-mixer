package webroot

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CleanupStaging 删除 rootDir/staging 下修改时间早于 now-maxAge 的一级子目录。
// staging 目录不存在时视为成功（removed=0）；单个子目录删除失败时跳过并继续，最终返回聚合错误。
func CleanupStaging(rootDir string, maxAge time.Duration, now time.Time) (removed int, err error) {
	if rootDir == "" {
		return 0, fmt.Errorf("rootDir 为空")
	}
	if maxAge <= 0 {
		return 0, fmt.Errorf("maxAge 必须为正：%v", maxAge)
	}

	stagingRoot := filepath.Join(rootDir, "staging")
	entries, readErr := os.ReadDir(stagingRoot)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取 staging 目录失败: %w", readErr)
	}

	cutoff := now.Add(-maxAge)
	var firstErr error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("读取目录信息失败 %s: %w", entry.Name(), infoErr)
			}
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(stagingRoot, entry.Name())
		if rmErr := os.RemoveAll(path); rmErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("删除目录失败 %s: %w", path, rmErr)
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
