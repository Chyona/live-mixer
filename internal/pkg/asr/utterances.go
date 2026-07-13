package asr

import "encoding/json"

// Utterance API 返回的 ASR 分句结构。
type Utterance struct {
	Speaker   string `json:"speaker"`
	EndTime   int64  `json:"end_time"`
	StartTime int64  `json:"start_time"`
	Text      string `json:"text"`
	Words     []Word `json:"words"`
}

// Word API 返回的 ASR 字级时间戳。
type Word struct {
	EndTime   int64  `json:"end_time"`
	StartTime int64  `json:"start_time"`
	Text      string `json:"text"`
}

// FormatUtterancesForAPI 将数据库 live_asr 原始 JSON 转为 API 分句数组。
// 无识别结果或解析失败时返回空数组。
func FormatUtterancesForAPI(liveASRJSON string) []Utterance {
	if liveASRJSON == "" || liveASRJSON == "{}" {
		return []Utterance{}
	}

	var payload struct {
		Result struct {
			Utterances []struct {
				Additions struct {
					Speaker string `json:"speaker"`
				} `json:"additions"`
				EndTime   int64  `json:"end_time"`
				StartTime int64  `json:"start_time"`
				Text      string `json:"text"`
				Words     []struct {
					EndTime   int64  `json:"end_time"`
					StartTime int64  `json:"start_time"`
					Text      string `json:"text"`
				} `json:"words"`
			} `json:"utterances"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(liveASRJSON), &payload); err != nil {
		return []Utterance{}
	}

	utterances := make([]Utterance, 0, len(payload.Result.Utterances))
	for _, item := range payload.Result.Utterances {
		words := make([]Word, 0, len(item.Words))
		for _, w := range item.Words {
			words = append(words, Word{
				EndTime:   w.EndTime,
				StartTime: w.StartTime,
				Text:      w.Text,
			})
		}
		utterances = append(utterances, Utterance{
			Speaker:   item.Additions.Speaker,
			EndTime:   item.EndTime,
			StartTime: item.StartTime,
			Text:      item.Text,
			Words:     words,
		})
	}
	return utterances
}
