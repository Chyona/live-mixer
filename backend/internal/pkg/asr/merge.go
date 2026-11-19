package asr

import (
	"encoding/json"
	"fmt"
)

type liveASRPayload struct {
	AudioInfo struct {
		Duration int64 `json:"duration"`
	} `json:"audio_info"`
	Result struct {
		Utterances []json.RawMessage `json:"utterances"`
	} `json:"result"`
}

type utteranceTimes struct {
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
}

// MergeWindowASR 将窗口识别结果按 offsetMS 平移后追加到已有 live_asr JSON。
// skipBeforeMS > 0 时丢弃平移后 start_time < skipBeforeMS 的分句，避免与上一窗尾部重叠重转。
func MergeWindowASR(existing string, window json.RawMessage, offsetMS, skipBeforeMS int64) (merged string, newDuration int64, err error) {
	base := liveASRPayload{}
	if existing != "" && existing != "{}" {
		if err := json.Unmarshal([]byte(existing), &base); err != nil {
			base = liveASRPayload{}
		}
	}
	win := liveASRPayload{}
	if len(window) > 0 {
		if err := json.Unmarshal(window, &win); err != nil {
			return "", 0, fmt.Errorf("解析窗口 ASR 失败: %w", err)
		}
	}
	for _, raw := range win.Result.Utterances {
		shifted, startMS, shiftErr := shiftUtteranceRaw(raw, offsetMS)
		if shiftErr != nil {
			continue
		}
		if skipBeforeMS > 0 && startMS < skipBeforeMS {
			continue
		}
		base.Result.Utterances = append(base.Result.Utterances, shifted)
	}
	dur := ParseDurationMs(window)
	total := offsetMS + dur
	if total > base.AudioInfo.Duration {
		base.AudioInfo.Duration = total
	}
	out, err := json.Marshal(base)
	if err != nil {
		return "", 0, err
	}
	return string(out), base.AudioInfo.Duration, nil
}

func shiftUtteranceRaw(raw json.RawMessage, offsetMS int64) (json.RawMessage, int64, error) {
	var u utteranceTimes
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, 0, err
	}
	u.StartTime += offsetMS
	u.EndTime += offsetMS
	for i := range u.Words {
		u.Words[i].StartTime += offsetMS
		u.Words[i].EndTime += offsetMS
	}
	out, err := json.Marshal(u)
	if err != nil {
		return nil, 0, err
	}
	return out, u.StartTime, nil
}
