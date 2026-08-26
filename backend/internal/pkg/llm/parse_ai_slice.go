package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AISliceResult 为 AI 选片 LLM 输出：句段索引 + 短视频标题/描述/话题。
type AISliceResult struct {
	Indices     []int    `json:"indices"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
}

type aiSliceLLMObject struct {
	Indices     []int    `json:"indices"`
	Indexes     []int    `json:"indexes"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Topics      []string `json:"topics"`
}

// ParseAISliceResult 从 LLM 文本解析选片结果。
// 优先解析 JSON 对象；兼容旧格式纯索引数组（title/description/topics 为空）。
func ParseAISliceResult(content string) (AISliceResult, error) {
	content = stripMarkdownFence(content)
	if content == "" {
		return AISliceResult{}, fmt.Errorf("LLM 返回内容为空")
	}

	if result, ok := tryUnmarshalAISliceObject(content); ok {
		return result, nil
	}

	if extracted := extractJSONPayload(content); extracted != "" && extracted != content {
		if result, ok := tryUnmarshalAISliceObject(extracted); ok {
			return result, nil
		}
	}

	// 兼容旧输出：仅索引数组。
	if indices, err := ParseIndices(content); err == nil {
		return AISliceResult{Indices: indices, Topics: []string{}}, nil
	}

	return AISliceResult{}, fmt.Errorf("无法解析 LLM 返回的选片 JSON: %s", truncate(content, 256))
}

func tryUnmarshalAISliceObject(content string) (AISliceResult, bool) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &probe); err != nil {
		return AISliceResult{}, false
	}
	_, hasIndices := probe["indices"]
	_, hasIndexes := probe["indexes"]
	if !hasIndices && !hasIndexes {
		return AISliceResult{}, false
	}

	var obj aiSliceLLMObject
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return AISliceResult{}, false
	}
	indices := obj.Indices
	if len(indices) == 0 && len(obj.Indexes) > 0 {
		indices = obj.Indexes
	}
	if indices == nil {
		indices = []int{}
	}
	topics := obj.Topics
	if topics == nil {
		topics = []string{}
	}
	return AISliceResult{
		Indices:     indices,
		Title:       obj.Title,
		Description: obj.Description,
		Topics:      topics,
	}, true
}

func stripMarkdownFence(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```JSON")
		content = strings.TrimPrefix(content, "```")
		if idx := strings.LastIndex(content, "```"); idx >= 0 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}
	return content
}
