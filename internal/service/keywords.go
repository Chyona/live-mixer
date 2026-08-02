package service

import (
	"strings"

	"live-mixer/internal/repository"
)

// parseKeywordExpr 解析关键词表达式：
//   - "," 分隔的词为「与」(AND)
//   - "|" 分隔的组为「或」(OR)
//
// 例："游戏,周末|发布会,2026" => [["游戏","周末"],["发布会","2026"]]。
// 空白与空段会被丢弃；词统一转小写。
func parseKeywordExpr(raw string) repository.KeywordGroups {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	orParts := strings.Split(raw, "|")
	groups := make(repository.KeywordGroups, 0, len(orParts))
	for _, orPart := range orParts {
		orPart = strings.TrimSpace(orPart)
		if orPart == "" {
			continue
		}
		andParts := strings.Split(orPart, ",")
		group := make([]string, 0, len(andParts))
		for _, part := range andParts {
			kw := strings.ToLower(strings.TrimSpace(part))
			if kw != "" {
				group = append(group, kw)
			}
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	if len(groups) == 0 {
		return nil
	}
	return groups
}
