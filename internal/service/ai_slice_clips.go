package service

import (
	"fmt"
	"strings"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

// filterUtterancesByClips0 按 video_project.clips0 时间段筛选 ASR 分句。
// 分句与任一 clips0 区间有时间重叠即纳入；保持原 ASR 顺序，同一分句只出现一次。
func filterUtterancesByClips0(utterances []asr.Utterance, clips0 []model.ClipRange) []asr.Utterance {
	if len(utterances) == 0 || len(clips0) == 0 {
		return nil
	}
	out := make([]asr.Utterance, 0)
	for _, u := range utterances {
		for _, r := range clips0 {
			// 分句与区间有重叠即可纳入待分析列表。
			if u.EndTime > r.StartTime && u.StartTime < r.EndTime {
				out = append(out, u)
				break
			}
		}
	}
	return out
}

// buildClips1FromIndices 根据 LLM 返回的句段下标，从筛选后的 ASR 列表组装 clips1。
// 下标越界或为负数时跳过，不中断整体流程。
func buildClips1FromIndices(segments []asr.Utterance, indices []int) []model.ClipWithText {
	if len(segments) == 0 || len(indices) == 0 {
		return []model.ClipWithText{}
	}
	out := make([]model.ClipWithText, 0, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= len(segments) {
			continue
		}
		out = append(out, utteranceToClipWithText(segments[idx]))
	}
	return out
}

// utteranceToClipWithText 将单条 ASR 分句转为 clips1 条目（含词级时间戳）。
func utteranceToClipWithText(u asr.Utterance) model.ClipWithText {
	return model.ClipWithText{
		Text:      u.Text,
		StartTime: u.StartTime,
		EndTime:   u.EndTime,
		Words:     toClipWords(u.Words),
	}
}

func toClipWords(words []asr.Word) []model.ClipWord {
	if len(words) == 0 {
		return nil
	}
	out := make([]model.ClipWord, len(words))
	for i, w := range words {
		out[i] = model.ClipWord{Text: w.Text, StartTime: w.StartTime, EndTime: w.EndTime}
	}
	return out
}

// formatASRSegmentLine 格式化单条 ASR 行：`[i] (开始秒 - 结束秒) 文本`。
// 时间由毫秒转为秒，保留两位小数，与内置用户提示词示例一致。
func formatASRSegmentLine(index int, u asr.Utterance) string {
	startSec := float64(u.StartTime) / 1000.0
	endSec := float64(u.EndTime) / 1000.0
	return fmt.Sprintf("[%d] (%.2f - %.2f) %s", index, startSec, endSec, strings.TrimSpace(u.Text))
}
