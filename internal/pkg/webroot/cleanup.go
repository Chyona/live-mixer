package webroot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type stagingDir struct {
	path    string
	modTime time.Time
}

// CleanupStaging 将 rootDir/staging 下一级子目录按 mtime 降序保留最多 keep 个，删除更早的。
// staging 目录不存在时视为成功（removed=0）；单个子目录删除失败时跳过并继续，最终返回聚合错误。
func CleanupStaging(rootDir string, keep int) (removed int, err error) {
	if rootDir == "" {
		return 0, fmt.Errorf("rootDir 为空")
	}
	if keep <= 0 {
		return 0, fmt.Errorf("keep 必须为正：%d", keep)
	}

	stagingRoot := filepath.Join(rootDir, "staging")
	entries, readErr := os.ReadDir(stagingRoot)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取 staging 目录失败: %w", readErr)
	}

	dirs := make([]stagingDir, 0, len(entries))
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
		dirs = append(dirs, stagingDir{
			path:    filepath.Join(stagingRoot, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	if len(dirs) <= keep {
		return 0, firstErr
	}

	// 最新在前：保留前 keep 个，删除其余（mtime 更早的）
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].modTime.Equal(dirs[j].modTime) {
			return dirs[i].path > dirs[j].path
		}
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	for _, d := range dirs[keep:] {
		if rmErr := os.RemoveAll(d.path); rmErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("删除目录失败 %s: %w", d.path, rmErr)
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
