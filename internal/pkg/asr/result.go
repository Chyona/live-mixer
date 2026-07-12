package asr

import "encoding/json"

// ParseDurationMs 从豆包 ASR 完整结果 JSON 解析音频时长（毫秒）。
func ParseDurationMs(raw json.RawMessage) int64 {
	var payload struct {
		AudioInfo struct {
			Duration int64 `json:"duration"`
		} `json:"audio_info"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	return payload.AudioInfo.Duration
}
