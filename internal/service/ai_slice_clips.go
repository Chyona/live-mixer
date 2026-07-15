package service

import (
	"encoding/json"
	"strings"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

// clipWithWords 带文本与词级时间戳的切片（对应 video_project.clips1 元素）。
type clipWithWords struct {
	Text      string     `json:"text"`
	StartTime int64      `json:"start_time"`
	EndTime   int64      `json:"end_time"`
	Words     []asr.Word `json:"words"`
}

// buildClips1 根据高光时间段与完整 ASR 分句组装 clips1。
// 将落在每个时间段内的词拼接为文本；无词级数据时回退使用分句文本。
func buildClips1(utterances []asr.Utterance, ranges []model.ClipRange) []clipWithWords {
	out := make([]clipWithWords, 0, len(ranges))
	for _, r := range ranges {
		words := collectWordsInRange(utterances, r.StartTime, r.EndTime)
		text := joinWordTexts(words)
		if text == "" {
			text = collectUtteranceTextInRange(utterances, r.StartTime, r.EndTime)
		}
		out = append(out, clipWithWords{
			Text:      text,
			StartTime: r.StartTime,
			EndTime:   r.EndTime,
			Words:     words,
		})
	}
	return out
}

// marshalClips1JSON 将 clips1 序列化为 JSON 字符串。
func marshalClips1JSON(clips []clipWithWords) (string, error) {
	if clips == nil {
		clips = []clipWithWords{}
	}
	raw, err := json.Marshal(clips)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// marshalClips0JSON 将 clips0 时间段序列化为 JSON 字符串。
func marshalClips0JSON(ranges []model.ClipRange) (string, error) {
	if ranges == nil {
		ranges = []model.ClipRange{}
	}
	raw, err := json.Marshal(ranges)
	if err != nil {
		return "", err
	}
	return string(raw), nil
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
