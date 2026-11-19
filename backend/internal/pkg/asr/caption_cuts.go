package asr

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// CaptionClause 标点断句后的一个子句：折行管线的第一级产物。
type CaptionClause struct {
	// Text 已剥掉首尾空白与断句标点的子句文本。
	Text string
	// Runes 子句的字数（rune 数，含西文字母与数字）。
	Runes int
}

// SplitCaptionClauses 第一级断句：按标点把文本切成子句，并剥掉每段的边缘标点。
//
// 这是「标点优先」这条产品规则的唯一入口：只有 Runes 超过行宽上限的子句才需要外部（LLM）给切点，
// 其余子句本身就是最终字幕行——不用问模型，也不会被后续步骤重新分组。
func SplitCaptionClauses(text string) []CaptionClause {
	raw := splitClauses(strings.TrimSpace(text))
	out := make([]CaptionClause, 0, len(raw))
	for _, clause := range raw {
		out = append(out, CaptionClause{Text: clause, Runes: utf8.RuneCountInString(clause)})
	}
	return out
}

// SplitLinesByCuts 把「标点断句后仍超长」的子句按外部（LLM）给出的切点位置切成字幕行。
//
// 切点语义：cuts[i] 是切点左侧最后一个字的 0 基下标，即「切在第 cuts[i] 个字之后」。
// 例：文本「最直接的解决方法是升级到修复该问题的版本」+ cuts=[8]，第 8 个字（0 起）是「是」，
// 第一行为「最直接的解决方法是」（9 字），第二行为「升级到修复该问题的版本」。
// 位置口径这样定，是为了让模型只报「第几个字」而不必复述文本：文本由代码切，
// 模型没有任何改写机会——这是本路径与「让模型产行」最本质的差别。
//
// 硬约束（全部不依赖模型听话）：
//   - 切点顺序不承载语义，先去重排序；越界（不在 0..字数-2）一律判 *CaptionLinesError 回退规则折行；
//   - 切点吸附到合法原子边界（captionAtomCuts：西文词/数字词/词表中文词不拆），与 SnapCaptionLines 同一套 DP；
//   - 每行不超过 max（只有单个比 max 还长的原子允许独占一行），排不出来同样回退规则折行。
//
// 返回的行经 normalizeCaptionLines 剥掉行首尾标点，因此与 SplitLinesByRule 的行形态完全一致：
// 行拼接 = 原文（标点按产品规则丢弃），时间分配读到的文本与规则路径同构，换切点不影响时间可达性。
func SplitLinesByCuts(text string, cuts []int, max int) ([]string, CaptionSnapStats, error) {
	var stats CaptionSnapStats
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, stats, &CaptionLinesError{Reason: CaptionLinesReasonEmpty, Detail: "原文为空"}
	}
	if max <= 0 {
		max = MaxCaptionRunes
	}

	runes := []rune(text)
	targets, err := cutOffsets(cuts, len(runes))
	if err != nil {
		return nil, stats, err
	}
	if len(targets) == 0 {
		// 模型一个切点也没给：等价于没有断句建议，交给规则折行。
		return nil, stats, &CaptionLinesError{
			Reason: CaptionLinesReasonUnsnappable,
			Detail: fmt.Sprintf("%d 个字没有给出切点", len(runes)),
		}
	}
	stats.CutCount = len(targets)

	snapped, ok := snapCuts(captionAtomCuts(runes, nil), targets, len(runes), max)
	if !ok {
		return nil, stats, &CaptionLinesError{
			Reason: CaptionLinesReasonUnsnappable,
			Detail: fmt.Sprintf("%d 个字切 %d 刀时词边界不够用", len(runes), len(targets)),
		}
	}
	for i, target := range targets {
		if d := snapped[i+1] - target; d != 0 {
			stats.MovedCuts++
			if d < 0 {
				d = -d
			}
			stats.Displaced += d
		}
	}

	out := make([]string, 0, len(snapped)-1)
	for i := 0; i+1 < len(snapped); i++ {
		out = append(out, string(runes[snapped[i]:snapped[i+1]]))
	}
	return normalizeCaptionLines(out, max), stats, nil
}

// cutOffsets 把模型给的切点位置换算成行边界偏移（0 基、左闭右开）并做硬校验。
// 顺序与重复不承载语义（去重排序）；位置必须落在 0..n-2：切点之后要还有字，
// n-1 会切出空行、n 及以上越界，都属于模型输出不可用。
func cutOffsets(cuts []int, n int) ([]int, error) {
	if len(cuts) == 0 {
		return nil, nil
	}
	sorted := make([]int, len(cuts))
	copy(sorted, cuts)
	sort.Ints(sorted)

	out := make([]int, 0, len(sorted))
	for _, c := range sorted {
		if c < 0 || c > n-2 {
			return nil, &CaptionLinesError{
				Reason: CaptionLinesReasonCutOutOfRange,
				Detail: fmt.Sprintf("切点 %d 越界（%d 个字的合法范围是 0..%d）", c, n, n-2),
			}
		}
		if len(out) > 0 && out[len(out)-1] == c+1 {
			continue // 同一位置切两次：后一个是空行，丢掉。
		}
		out = append(out, c+1)
	}
	return out, nil
}
