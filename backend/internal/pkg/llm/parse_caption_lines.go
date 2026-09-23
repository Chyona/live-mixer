package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// captionLinesPayload 断句 LLM 输出的标准结构：{"lines": ["第一行", "第二行"]}。
type captionLinesPayload struct {
	Lines []string `json:"lines"`
}

// ParseCaptionLines 从 LLM 文本中解析字幕断句行数组。
// 兼容：{"lines": [...]}、裸字符串数组、markdown 代码块包裹、前后夹杂说明文字。
// 只做「文本 → 结构」的解析（顺带丢弃空行），不校验行与原文是否一致（那是 asr.ValidateCaptionLines 的职责）。
func ParseCaptionLines(content string) ([]string, error) {
	content = stripMarkdownFence(content)
	if content == "" {
		return nil, fmt.Errorf("LLM 返回内容为空")
	}

	if lines, ok := tryUnmarshalCaptionLines(content); ok {
		return lines, nil
	}
	if extracted := extractJSONPayload(content); extracted != "" && extracted != content {
		if lines, ok := tryUnmarshalCaptionLines(extracted); ok {
			return lines, nil
		}
	}

	return nil, fmt.Errorf("无法解析 LLM 返回的断句 JSON: %s", truncate(content, 256))
}

// tryUnmarshalCaptionLines 依次尝试 {"lines": [...]} 与裸数组两种形态；失败返回 ok=false。
func tryUnmarshalCaptionLines(content string) ([]string, bool) {
	var payload captionLinesPayload
	if err := json.Unmarshal([]byte(content), &payload); err == nil {
		if lines := normalizeCaptionLinesPayload(payload.Lines); len(lines) > 0 {
			return lines, true
		}
	}

	var lines []string
	if err := json.Unmarshal([]byte(content), &lines); err == nil {
		if normalized := normalizeCaptionLinesPayload(lines); len(normalized) > 0 {
			return normalized, true
		}
	}

	return nil, false
}

// normalizeCaptionLinesPayload 去掉每行首尾空白并丢弃空行；无有效行时返回 nil。
func normalizeCaptionLinesPayload(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
