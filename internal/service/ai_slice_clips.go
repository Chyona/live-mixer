package service

import (
	"fmt"
	"sort"
	"strings"

	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

// sortAndMergeOverlappingClipRanges 对 clips0 做 AI 选片预处理：
// 1. 按 start_time 升序（相同则按 end_time）；
// 2. 重叠或端点相接（next.Start <= cur.End）的区间取并集合并。
// 返回新切片，不修改入参；空入参返回 nil。
func sortAndMergeOverlappingClipRanges(clips []model.ClipRange) []model.ClipRange {
	if len(clips) == 0 {
		return nil
	}
	sorted := make([]model.ClipRange, len(clips))
	copy(sorted, clips)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].StartTime != sorted[j].StartTime {
			return sorted[i].StartTime < sorted[j].StartTime
		}
		return sorted[i].EndTime < sorted[j].EndTime
	})

	out := make([]model.ClipRange, 0, len(sorted))
	cur := sorted[0]
	for i := 1; i < len(sorted); i++ {
		next := sorted[i]
		// 重叠或端点相接 → 并集；有间隙则新开一段。
		if next.StartTime <= cur.EndTime {
			if next.EndTime > cur.EndTime {
				cur.EndTime = next.EndTime
			}
			continue
		}
		out = append(out, cur)
		cur = next
	}
	out = append(out, cur)
	return out
}

// filterUtterancesByClips0 按 clips0 时间段筛选 ASR 分句。
// 分句与任一区间有时间重叠即纳入；保持原 ASR 顺序，同一分句只出现一次。
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
// 下标越界或为负数时跳过；组装后按时间相邻规则合并（间隙 ≤ ClipMergeGapMS），再写入 clips1。
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
	return mergeAdjacentClips1(out, prepare.ClipMergeGapMS)
}

// mergeAdjacentClips1 按列表顺序合并相邻 clips1：规则与草稿侧 MergeAdjacentClipRanges 一致。
// 当 next.Start >= cur.Start 且 gap=next.Start-cur.End ≤ maxGapMS（含重叠）时合并；
// 合并后 text / words 按顺序拼接，时间取 [cur.Start, max(cur.End, next.End)]。
func mergeAdjacentClips1(clips []model.ClipWithText, maxGapMS int64) []model.ClipWithText {
	if len(clips) == 0 {
		return []model.ClipWithText{}
	}
	if len(clips) == 1 {
		return []model.ClipWithText{cloneClipWithText(clips[0])}
	}

	out := make([]model.ClipWithText, 0, len(clips))
	cur := cloneClipWithText(clips[0])
	for i := 1; i < len(clips); i++ {
		next := clips[i]
		gap := next.StartTime - cur.EndTime
		if next.StartTime >= cur.StartTime && gap <= maxGapMS {
			cur.Text += next.Text
			if next.EndTime > cur.EndTime {
				cur.EndTime = next.EndTime
			}
			if len(next.Words) > 0 {
				cur.Words = append(cur.Words, append([]model.ClipWord(nil), next.Words...)...)
			}
			continue
		}
		out = append(out, cur)
		cur = cloneClipWithText(next)
	}
	out = append(out, cur)
	return out
}

func cloneClipWithText(c model.ClipWithText) model.ClipWithText {
	out := model.ClipWithText{
		Text:      c.Text,
		StartTime: c.StartTime,
		EndTime:   c.EndTime,
	}
	if len(c.Words) > 0 {
		out.Words = append([]model.ClipWord(nil), c.Words...)
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
