package service

import (
	"strings"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

// buildClips1 根据高光时间段与完整 ASR 分句组装 clips1。
// 将落在每个时间段内的词拼接为文本；无词级数据时回退使用分句文本。
func buildClips1(utterances []asr.Utterance, ranges []model.ClipRange) []model.ClipWithText {
	out := make([]model.ClipWithText, 0, len(ranges))
	for _, r := range ranges {
		words := collectWordsInRange(utterances, r.StartTime, r.EndTime)
		text := joinWordTexts(words)
		if text == "" {
			text = collectUtteranceTextInRange(utterances, r.StartTime, r.EndTime)
		}
		out = append(out, model.ClipWithText{
			Text:      text,
			StartTime: r.StartTime,
			EndTime:   r.EndTime,
			Words:     toClipWords(words),
		})
	}
	return out
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

func collectWordsInRange(utterances []asr.Utterance, start, end int64) []asr.Word {
	var words []asr.Word
	for _, u := range utterances {
		for _, w := range u.Words {
			// 词与区间有重叠即可纳入该高光片段。
			if w.EndTime > start && w.StartTime < end {
				words = append(words, w)
			}
		}
	}
	return words
}

func collectUtteranceTextInRange(utterances []asr.Utterance, start, end int64) string {
	var parts []string
	for _, u := range utterances {
		if u.EndTime > start && u.StartTime < end && strings.TrimSpace(u.Text) != "" {
			parts = append(parts, strings.TrimSpace(u.Text))
		}
	}
	return strings.Join(parts, "")
}

func joinWordTexts(words []asr.Word) string {
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	for _, w := range words {
		b.WriteString(w.Text)
	}
	return b.String()
}
