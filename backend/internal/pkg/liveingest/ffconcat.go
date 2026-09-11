package liveingest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GlobLocalSegments 按文件名排序返回目录下 seg_*.ts。
func GlobLocalSegments(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "seg_*.ts"))
	sort.Strings(matches)
	return matches
}

// WriteFFConcatList 写出 ffmpeg concat demuxer 清单（ffconcat version 1.0）。
// 路径使用绝对路径并 Quote，便于 Windows 反斜杠与空格。
func WriteFFConcatList(files []string, dest string) error {
	if len(files) == 0 {
		return fmt.Errorf("无分片可写入 concat 清单")
	}
	var b strings.Builder
	b.WriteString("ffconcat version 1.0\n")
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			abs = f
		}
		b.WriteString("file ")
		b.WriteString(strconv.Quote(abs))
		b.WriteString("\n")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(b.String()), 0o644)
}

// LiveIngestSegmentDir 本地跟播分片目录：{webRoot}/staging/live_ingest/{id}/e{epoch}。
func LiveIngestSegmentDir(webRoot string, materialID uint, ingestEpoch int64) string {
	root := strings.TrimSpace(webRoot)
	if root == "" {
		return ""
	}
	epoch := ingestEpoch
	if epoch < 1 {
		epoch = 1
	}
	return filepath.Join(root, "staging", "live_ingest", fmt.Sprintf("%d", materialID), fmt.Sprintf("e%d", epoch))
}
