package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"live-mixer/internal/model"
)

// normalizeVideoTitle 校验短视频标题：空表示未设置；非空须为 2～12 个字。
func normalizeVideoTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", nil
	}
	n := utf8.RuneCountInString(title)
	if n < model.VideoProjectTitleMinRunes || n > model.VideoProjectTitleMaxRunes {
		return "", fmt.Errorf("标题须为 %d～%d 个字", model.VideoProjectTitleMinRunes, model.VideoProjectTitleMaxRunes)
	}
	return title, nil
}

// normalizeVideoDescription 校验短视频描述：空表示未设置；非空须不超过 128 个字。
func normalizeVideoDescription(raw string) (string, error) {
	desc := strings.TrimSpace(raw)
	if desc == "" {
		return "", nil
	}
	if utf8.RuneCountInString(desc) > model.VideoProjectDescriptionMaxRunes {
		return "", fmt.Errorf("描述须在 %d 个字以内", model.VideoProjectDescriptionMaxRunes)
	}
	return desc, nil
}

// normalizeVideoTopics 校验短视频话题：空数组表示未设置；非空须 2～6 个，每个 2～12 个字。
func normalizeVideoTopics(raw []string) ([]string, error) {
	if raw == nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for i, item := range raw {
		topic, err := normalizeVideoTopic(item)
		if err != nil {
			return nil, fmt.Errorf("topics[%d] %w", i, err)
		}
		if topic == "" {
			return nil, fmt.Errorf("topics[%d] 不能为空", i)
		}
		if _, dup := seen[topic]; dup {
			continue
		}
		seen[topic] = struct{}{}
		out = append(out, topic)
	}
	if len(out) == 0 {
		return []string{}, nil
	}
	if len(out) < model.VideoProjectTopicsMinCount || len(out) > model.VideoProjectTopicsMaxCount {
		return nil, fmt.Errorf("话题须为 %d～%d 个", model.VideoProjectTopicsMinCount, model.VideoProjectTopicsMaxCount)
	}
	return out, nil
}

func normalizeVideoTopic(raw string) (string, error) {
	topic := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "#＃"))
	if topic == "" {
		return "", nil
	}
	n := utf8.RuneCountInString(topic)
	if n < model.VideoProjectTopicMinRunes || n > model.VideoProjectTopicMaxRunes {
		return "", fmt.Errorf("须为 %d～%d 个字", model.VideoProjectTopicMinRunes, model.VideoProjectTopicMaxRunes)
	}
	return topic, nil
}

// sanitizeVideoTitleFromLLM 将 LLM 标题裁剪到合法范围；过短则置空，不阻断切片。
func sanitizeVideoTitleFromLLM(raw string) string {
	title := strings.TrimSpace(raw)
	if title == "" {
		return ""
	}
	runes := []rune(title)
	if len(runes) > model.VideoProjectTitleMaxRunes {
		title = string(runes[:model.VideoProjectTitleMaxRunes])
	}
	if utf8.RuneCountInString(title) < model.VideoProjectTitleMinRunes {
		return ""
	}
	return title
}

// sanitizeVideoDescriptionFromLLM 将 LLM 描述裁剪到 128 字以内。
func sanitizeVideoDescriptionFromLLM(raw string) string {
	desc := strings.TrimSpace(raw)
	if desc == "" {
		return ""
	}
	runes := []rune(desc)
	if len(runes) > model.VideoProjectDescriptionMaxRunes {
		return string(runes[:model.VideoProjectDescriptionMaxRunes])
	}
	return desc
}

// sanitizeVideoTopicsFromLLM 规范化 LLM 话题：去 #、去重、按字数过滤，最多保留 6 个。
func sanitizeVideoTopicsFromLLM(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		topic := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(item), "#＃"))
		if topic == "" {
			continue
		}
		runes := []rune(topic)
		if len(runes) > model.VideoProjectTopicMaxRunes {
			topic = string(runes[:model.VideoProjectTopicMaxRunes])
		}
		if utf8.RuneCountInString(topic) < model.VideoProjectTopicMinRunes {
			continue
		}
		if _, dup := seen[topic]; dup {
			continue
		}
		seen[topic] = struct{}{}
		out = append(out, topic)
		if len(out) >= model.VideoProjectTopicsMaxCount {
			break
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}
