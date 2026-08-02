package asr

import (
	"fmt"
	"strings"
	"time"
)

// BuildSubtitleTXT 生成 ASR 字幕 TXT。
// 关键词：titles 空格拼接；文字记录：连续相同说话人合并，显示为「说话人${speaker} MM:SS」。
func BuildSubtitleTXT(titles []string, utterances []Utterance) string {
	var b strings.Builder
	b.WriteString("关键词\n")
	cleaned := make([]string, 0, len(titles))
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) > 0 {
		b.WriteString(strings.Join(cleaned, " "))
	}
	b.WriteString("\n\n")

	b.WriteString("文字记录\n")
	blocks := mergeUtterancesBySpeaker(utterances)
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "说话人%s %s\n%s\n", block.Speaker, formatASRClock(block.StartTime), block.Text)
	}
	return b.String()
}

type subtitleBlock struct {
	Speaker   string
	StartTime int64
	Text      string
}

// mergeUtterancesBySpeaker 合并连续相同说话人的分句；文本直接拼接，时间取首句 start_time。
func mergeUtterancesBySpeaker(utterances []Utterance) []subtitleBlock {
	out := make([]subtitleBlock, 0)
	for _, u := range utterances {
		text := strings.TrimSpace(u.Text)
		if text == "" {
			continue
		}
		speaker := strings.TrimSpace(u.Speaker)
		n := len(out)
		if n > 0 && out[n-1].Speaker == speaker {
			out[n-1].Text += text
			continue
		}
		out = append(out, subtitleBlock{
			Speaker:   speaker,
			StartTime: u.StartTime,
			Text:      text,
		})
	}
	return out
}

// formatASRClock 将毫秒转为可读时钟：不足 1 小时为 MM:SS，否则 H:MM:SS。
func formatASRClock(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	d := time.Duration(ms) * time.Millisecond
	totalSec := int64(d.Seconds())
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
