package asr

import (
	"encoding/json"
	"fmt"
	"math"
)

const (
	// DefaultScaleSkewThresholdMS：厂商时长与媒体探针差超过该值才考虑线性缩放。
	// 秒级偏差常见于 audio_info.duration 元数据偏短/末尾静音未计入，不应拉伸词级时间戳。
	DefaultScaleSkewThresholdMS int64 = 5000
)

// MaxUtteranceEndMS 返回结果中分句/词的最大 end_time（毫秒）；无则 0。
func MaxUtteranceEndMS(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var payload liveASRPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	var max int64
	for _, item := range payload.Result.Utterances {
		var u utteranceTimes
		if err := json.Unmarshal(item, &u); err != nil {
			continue
		}
		if u.EndTime > max {
			max = u.EndTime
		}
		for _, w := range u.Words {
			if w.EndTime > max {
				max = w.EndTime
			}
		}
	}
	return max
}

// MinUtteranceStartMS 返回结果中分句/词的最小 start_time（毫秒）；无则 0。
func MinUtteranceStartMS(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var payload liveASRPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0
	}
	var min int64 = -1
	for _, item := range payload.Result.Utterances {
		var u utteranceTimes
		if err := json.Unmarshal(item, &u); err != nil {
			continue
		}
		if u.StartTime >= 0 && (min < 0 || u.StartTime < min) {
			min = u.StartTime
		}
		for _, w := range u.Words {
			if w.StartTime >= 0 && (min < 0 || w.StartTime < min) {
				min = w.StartTime
			}
		}
	}
	if min < 0 {
		return 0
	}
	return min
}

// SetAudioInfoDuration 只改写 audio_info.duration，不改动词级时间戳。
func SetAudioInfoDuration(raw json.RawMessage, durationMS int64) (json.RawMessage, error) {
	if len(raw) == 0 || durationMS <= 0 {
		return raw, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析 ASR JSON: %w", err)
	}
	info := map[string]interface{}{"duration": durationMS}
	if infoRaw, ok := payload["audio_info"]; ok && len(infoRaw) > 0 {
		_ = json.Unmarshal(infoRaw, &info)
		info["duration"] = durationMS
	}
	b, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	payload["audio_info"] = b
	return json.Marshal(payload)
}

// ScaleTimestampsToDuration 将 ASR 结果内时间戳线性缩放到 targetDurationMS，
// 并改写 audio_info.duration。用于对齐「送转写媒体真实时长」与「厂商 audio_info」。
// 返回缩放后的 JSON、scale 因子（target/vendor）；无需缩放时原样返回 scale=1。
func ScaleTimestampsToDuration(raw json.RawMessage, targetDurationMS int64) (json.RawMessage, float64, error) {
	if len(raw) == 0 || targetDurationMS <= 0 {
		return raw, 1, nil
	}
	vendor := ParseDurationMs(raw)
	if vendor <= 0 {
		return raw, 1, nil
	}
	scale := float64(targetDurationMS) / float64(vendor)
	if math.Abs(scale-1) < 1e-9 {
		return raw, 1, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, 0, fmt.Errorf("解析 ASR JSON: %w", err)
	}

	if infoRaw, ok := payload["audio_info"]; ok && len(infoRaw) > 0 {
		var info map[string]interface{}
		if err := json.Unmarshal(infoRaw, &info); err == nil {
			info["duration"] = targetDurationMS
			if b, err := json.Marshal(info); err == nil {
				payload["audio_info"] = b
			}
		}
	}

	if resultRaw, ok := payload["result"]; ok && len(resultRaw) > 0 {
		var result map[string]json.RawMessage
		if err := json.Unmarshal(resultRaw, &result); err == nil {
			if uttRaw, ok := result["utterances"]; ok && len(uttRaw) > 0 {
				scaled, err := scaleUtterancesRaw(uttRaw, scale)
				if err != nil {
					return nil, 0, err
				}
				result["utterances"] = scaled
				if b, err := json.Marshal(result); err == nil {
					payload["result"] = b
				}
			}
		}
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	return out, scale, nil
}

func scaleUtterancesRaw(raw json.RawMessage, scale float64) (json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		scaled, err := scaleUtteranceRaw(item, scale)
		if err != nil {
			continue
		}
		out = append(out, scaled)
	}
	return json.Marshal(out)
}

func scaleUtteranceRaw(raw json.RawMessage, scale float64) (json.RawMessage, error) {
	var u utteranceTimes
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, err
	}
	u.StartTime = scaleMS(u.StartTime, scale)
	u.EndTime = scaleMS(u.EndTime, scale)
	for i := range u.Words {
		u.Words[i].StartTime = scaleMS(u.Words[i].StartTime, scale)
		u.Words[i].EndTime = scaleMS(u.Words[i].EndTime, scale)
	}
	return json.Marshal(u)
}

func scaleMS(v int64, scale float64) int64 {
	if v <= 0 {
		return v
	}
	return int64(math.Round(float64(v) * scale))
}

// ShouldScaleTimestamps 判断厂商时长与媒体时长是否偏差过大（仅元数据门槛）。
func ShouldScaleTimestamps(vendorMS, mediaMS, thresholdMS int64) bool {
	if vendorMS <= 0 || mediaMS <= 0 {
		return false
	}
	if thresholdMS <= 0 {
		thresholdMS = DefaultScaleSkewThresholdMS
	}
	d := vendorMS - mediaMS
	if d < 0 {
		d = -d
	}
	return d >= thresholdMS
}

// ShouldScaleUtteranceTimestamps 判断是否应线性拉伸词级时间戳。
// 若末句已落在媒体时长内，视为 audio_info.duration 元数据偏短，禁止缩放（否则后段字幕会越拉越偏）。
func ShouldScaleUtteranceTimestamps(vendorMS, mediaMS, maxUttEndMS, thresholdMS int64) bool {
	if !ShouldScaleTimestamps(vendorMS, mediaMS, thresholdMS) {
		return false
	}
	if thresholdMS <= 0 {
		thresholdMS = DefaultScaleSkewThresholdMS
	}
	if maxUttEndMS > 0 && maxUttEndMS <= mediaMS+thresholdMS {
		return false
	}
	return true
}
