package liveingest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GlobLocalSegments 按文件名排序返回目录下 seg_*.ts。
func GlobLocalSegments(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "seg_*.ts"))
	sort.Strings(matches)
	return matches
}

// quoteFFConcatPath 按 ffmpeg concat 脚本规则引用路径：正斜杠 + 单引号。
// 勿用 strconv.Quote：Go 双引号与 \\ 转义会被 ffmpeg 当成文件名的一部分，Windows 上报
// Impossible to open '"E:\\...\\seg.ts"'。
func quoteFFConcatPath(abs string) string {
	p := filepath.ToSlash(abs)
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range p {
		if r == '\'' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// WriteFFConcatList 写出 ffmpeg concat demuxer 清单（ffconcat version 1.0）。
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
		b.WriteString(quoteFFConcatPath(abs))
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
