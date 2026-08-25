package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseIndices 从 LLM 文本中解析句段索引数组。
// 兼容：纯 JSON 数组、markdown 代码块包裹、前后夹杂说明文字。
// 例如：[2, 5, 9, 13]
func ParseIndices(content string) ([]int, error) {
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

	if indices, ok := tryUnmarshalIndices(content); ok {
		return indices, nil
	}

	// 兜底：截取首个 JSON 数组再解析。
	if extracted := extractJSONArray(content); extracted != "" && extracted != content {
		if indices, ok := tryUnmarshalIndices(extracted); ok {
			return indices, nil
		}
	}

	return nil, fmt.Errorf("无法解析 LLM 返回的索引 JSON: %s", truncate(content, 256))
}

// tryUnmarshalIndices 尝试将文本解析为整数数组；失败返回 ok=false。
func tryUnmarshalIndices(content string) ([]int, bool) {
	var indices []int
	if err := json.Unmarshal([]byte(content), &indices); err != nil {
		return nil, false
	}
	return indices, true
}

// extractJSONArray 从文本中截取第一个完整 JSON 数组。
func extractJSONArray(s string) string {
	start := strings.Index(s, "[")
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
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
