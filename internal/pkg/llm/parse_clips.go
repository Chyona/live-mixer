package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"live-mixer/internal/model"
)

// ClipSelectionResponse 大模型高光片段选取的期望 JSON 结构。
type ClipSelectionResponse struct {
	Clips []model.ClipRange `json:"clips"`
}

// ParseClipRanges 从 LLM 文本中解析高光片段时间段。
// 兼容：纯 JSON 对象、代码块包裹、或顶层 clips 数组。
func ParseClipRanges(content string) ([]model.ClipRange, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("LLM 返回内容为空")
	}

	// 去掉常见 markdown 代码块包裹。
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```JSON")
		content = strings.TrimPrefix(content, "```")
		if idx := strings.LastIndex(content, "```"); idx >= 0 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}

	// 优先解析 {"clips":[...]}
	var obj ClipSelectionResponse
	if err := json.Unmarshal([]byte(content), &obj); err == nil && len(obj.Clips) > 0 {
		return validateRanges(obj.Clips)
	}

	// 其次尝试直接解析为数组。
	var ranges []model.ClipRange
	if err := json.Unmarshal([]byte(content), &ranges); err == nil && len(ranges) > 0 {
		return validateRanges(ranges)
	}

	// 兜底：截取首个 JSON 对象或数组再解析。
	if extracted := extractJSONPayload(content); extracted != "" && extracted != content {
		return ParseClipRanges(extracted)
	}

	return nil, fmt.Errorf("无法解析 LLM 返回的片段 JSON: %s", truncate(content, 256))
}

func validateRanges(ranges []model.ClipRange) ([]model.ClipRange, error) {
	out := make([]model.ClipRange, 0, len(ranges))
	for i, r := range ranges {
		if r.StartTime < 0 || r.EndTime <= r.StartTime {
			return nil, fmt.Errorf("第 %d 个片段时间无效: start=%d end=%d", i, r.StartTime, r.EndTime)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("片段列表为空")
	}
	return out, nil
}

// extractJSONPayload 从文本中截取第一个完整 JSON 对象或数组。
func extractJSONPayload(s string) string {
	startObj := strings.Index(s, "{")
	startArr := strings.Index(s, "[")
	start := -1
	open, close := byte('{'), byte('}')
	if startObj >= 0 && (startArr < 0 || startObj < startArr) {
		start = startObj
	} else if startArr >= 0 {
		start = startArr
		open, close = '[', ']'
	}
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
