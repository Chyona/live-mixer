package asr

import (
	"fmt"
	"strings"
)

// SubtitleParagraph 字幕分段（与 live_material.asr_paragraphs 一一对应）。
type SubtitleParagraph struct {
	Speaker   string
	Text      string
	StartTime int64
}

// BuildSubtitleTXT 生成 ASR 字幕 TXT。
// 格式：关键词行 + 空行 +「文字记录」+ 各段落「说话人{speaker} {MM:SS|H:MM:SS}」与文本块（分段顺序与内容与 asr_paragraphs 一致）。
func BuildSubtitleTXT(titles []string, paragraphs []SubtitleParagraph) string {
	var b strings.Builder
	b.WriteString("关键词\n")
	keywords := make([]string, 0, len(titles))
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		keywords = append(keywords, t)
	}
	b.WriteString(strings.Join(keywords, " "))
	b.WriteString("\n\n文字记录\n")

	first := true
	for _, p := range paragraphs {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		fmt.Fprintf(&b, "说话人%s %s\n%s\n", p.Speaker, formatASRClock(p.StartTime), p.Text)
	}
	return b.String()
}

// formatASRClock 将毫秒格式化为 MM:SS；满 1 小时为 H:MM:SS（小时不补零）。
func formatASRClock(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSec := ms / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
