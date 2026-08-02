package repository

import (
	"strings"

	"gorm.io/gorm"
)

// KeywordGroups 关键词表达式分组：外层各组为「或」，组内各词为「与」。
// 例："游戏,周末|发布会" => [["游戏","周末"],["发布会"]]。
type KeywordGroups [][]string

// KeywordGroupsEmpty 判断是否无可筛选词。
func KeywordGroupsEmpty(groups KeywordGroups) bool {
	for _, g := range groups {
		if len(g) > 0 {
			return false
		}
	}
	return true
}

// applyKeywordGroups 将关键词表达式应用到查询：组内 AND、组间 OR。
// termClause 为单个词的匹配 SQL（可含多个 ?），argsPerTerm 为每个词需要绑定的参数个数（通常等于 ? 个数）；
// 每个 ? 都会绑定同一 pattern "%kw%"。
func applyKeywordGroups(query *gorm.DB, groups KeywordGroups, termClause string, argsPerTerm int) *gorm.DB {
	if KeywordGroupsEmpty(groups) || termClause == "" || argsPerTerm <= 0 {
		return query
	}
	var orParts []string
	args := make([]interface{}, 0)
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		andParts := make([]string, 0, len(group))
		for _, kw := range group {
			andParts = append(andParts, "("+termClause+")")
			pattern := "%" + kw + "%"
			for i := 0; i < argsPerTerm; i++ {
				args = append(args, pattern)
			}
		}
		orParts = append(orParts, "("+strings.Join(andParts, " AND ")+")")
	}
	if len(orParts) == 0 {
		return query
	}
	return query.Where(strings.Join(orParts, " OR "), args...)
}
