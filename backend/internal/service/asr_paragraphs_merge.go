package service

import (
	"fmt"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"

	"go.uber.org/zap"
)

// SameSpeakerOverlapWarning 同一说话人相邻句时间重叠（数据异常）。
type SameSpeakerOverlapWarning struct {
	LeftIndex  int    `json:"left_index"`
	RightIndex int    `json:"right_index"`
	Speaker    string `json:"speaker"`
	LeftStart  int64  `json:"left_start"`
	LeftEnd    int64  `json:"left_end"`
	RightStart int64  `json:"right_start"`
	RightEnd   int64  `json:"right_end"`
	GapMs      int64  `json:"gap_ms"` // 负值表示重叠毫秒数
}

// BuildASRParagraphsByMinGap 按「同说话人 + 合并字数≤maxLen + 全局最小非负空隙」贪心合并分句为段落。
// 同一说话人相邻句若 gap<0（重叠），打强 Warn 日志并跳过该对，不把负 gap 当作更相邻。
// maxLen≤0 时使用 asrParagraphMaxRunes（200）。logger 可为 nil。
func BuildASRParagraphsByMinGap(utterances []asr.Utterance, maxLen int, logger *zap.Logger) ([]model.ASRParagraph, []SameSpeakerOverlapWarning) {
	if maxLen <= 0 {
		maxLen = asrParagraphMaxRunes
	}
	if len(utterances) == 0 {
		return []model.ASRParagraph{}, nil
	}

	segs := make([]mergeSeg, 0, len(utterances))
	for _, u := range utterances {
		words := make([]model.ClipWord, 0, len(u.Words))
		for _, w := range u.Words {
			words = append(words, model.ClipWord{
				Text:      w.Text,
				StartTime: w.StartTime,
				EndTime:   w.EndTime,
			})
		}
		segs = append(segs, mergeSeg{
			speaker: u.Speaker,
			text:    u.Text,
			start:   u.StartTime,
			end:     u.EndTime,
			words:   words,
		})
	}

	var warnings []SameSpeakerOverlapWarning
	warned := map[string]struct{}{}

	for {
		bestI := -1
		var bestGap int64
		for i := 0; i < len(segs)-1; i++ {
			a, b := segs[i], segs[i+1]
			if a.speaker != b.speaker {
				continue
			}
			gap := b.start - a.end
			if gap < 0 {
				key := fmt.Sprintf("%s|%d|%d|%d|%d", a.speaker, a.start, a.end, b.start, b.end)
				if _, ok := warned[key]; !ok {
					warned[key] = struct{}{}
					w := SameSpeakerOverlapWarning{
						LeftIndex:  i,
						RightIndex: i + 1,
						Speaker:    a.speaker,
						LeftStart:  a.start,
						LeftEnd:    a.end,
						RightStart: b.start,
						RightEnd:   b.end,
						GapMs:      gap,
					}
					warnings = append(warnings, w)
					if logger != nil {
						logger.Warn("【强提醒】同一说话人相邻语句时间重叠，禁止按负空隙合并",
							zap.Int("left_index", w.LeftIndex),
							zap.Int("right_index", w.RightIndex),
							zap.String("speaker", w.Speaker),
							zap.Int64("left_start", w.LeftStart),
							zap.Int64("left_end", w.LeftEnd),
							zap.Int64("right_start", w.RightStart),
							zap.Int64("right_end", w.RightEnd),
							zap.Int64("gap_ms", w.GapMs),
						)
					}
				}
				continue
			}
			mergedRunes := utf8.RuneCountInString(a.text) + utf8.RuneCountInString(b.text)
			if mergedRunes > maxLen {
				continue
			}
			// 并列取更小下标：从左扫，仅在 gap 严格更小时更新。
			if bestI < 0 || gap < bestGap {
				bestI = i
				bestGap = gap
			}
		}
		if bestI < 0 {
			break
		}
		merged := mergeSegPair(segs[bestI], segs[bestI+1])
		segs = append(segs[:bestI], append([]mergeSeg{merged}, segs[bestI+2:]...)...)
	}

	out := make([]model.ASRParagraph, 0, len(segs))
	for _, s := range segs {
		p := model.ASRParagraph{
			Speaker:   s.speaker,
			Text:      s.text,
			StartTime: s.start,
			EndTime:   s.end,
			Words:     s.words,
		}
		if !syncASRParagraphTimesFromWords(&p) {
			if p.EndTime < p.StartTime {
				p.StartTime, p.EndTime = p.EndTime, p.StartTime
			}
			if p.EndTime <= p.StartTime {
				p.EndTime = p.StartTime + 1
			}
		}
		out = append(out, p)
	}
	return out, warnings
}

type mergeSeg struct {
	speaker string
	text    string
	start   int64
	end     int64
	words   []model.ClipWord
}

func mergeSegPair(a, b mergeSeg) mergeSeg {
	start, end := a.start, a.end
	if b.start < start {
		start = b.start
	}
	if b.end > end {
		end = b.end
	}
	words := make([]model.ClipWord, 0, len(a.words)+len(b.words))
	words = append(words, a.words...)
	words = append(words, b.words...)
	return mergeSeg{
		speaker: a.speaker,
		text:    a.text + b.text,
		start:   start,
		end:     end,
		words:   words,
	}
}
