package asr

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxCaptionRunes 单条字幕最大字数（按 Unicode rune 计，含标点与英文字母）。
const MaxCaptionRunes = 12

// TimedSegment 断句后的字幕片段（源时间轴，毫秒）。
type TimedSegment struct {
	Text      string
	StartTime int64
	EndTime   int64
}

type captionAtom struct {
	text       string
	keepIntact bool // 英文词 / 数字词 / 中文词典词整段不拆
	runes      int
}

// SplitUtteranceForCaptions 将一句 ASR 按标点断句，超长则按最少行数折行（英文/数字/中文词典词不拆），并切分时间。
// 成片每行会剥离首尾断句标点，保证行首行尾均非标点。
func SplitUtteranceForCaptions(u Utterance) []TimedSegment {
	lines := splitLinesForText(strings.TrimSpace(u.Text), buildExtraCJKWords(u.Words), MaxCaptionRunes)
	if len(lines) == 0 {
		return nil
	}
	return TimedSegmentsForLines(u, lines)
}

// SplitLinesByRule 按产品折行规则把文本切成字幕行：标点断句 → 剥离首尾标点 → 超长按最少行数折行，
// 静态词表里的中文词、西文词与数字词整段不拆。
// 供外部断句（如 LLM 折行）的兜底与超长行修正复用，不涉及时间。
func SplitLinesByRule(text string) []string {
	return splitLinesForText(strings.TrimSpace(text), nil, MaxCaptionRunes)
}

// SplitLinesByRuleMax 与 SplitLinesByRule 同规则，行宽上限由调用方指定。
func SplitLinesByRuleMax(text string, max int) []string {
	if max <= 0 {
		max = MaxCaptionRunes
	}
	return splitLinesForText(strings.TrimSpace(text), nil, max)
}

// TimedSegmentsForLines 为已切好的字幕行分配时间：词级对齐优先，失败整条回退比例分配。
// 行必须是原文的连续切分（字符与顺序与原文一致），否则词级对齐必然失败并整体降级为比例分配——
// 这也意味着「换一种断句」不影响时间分配的可达性，只是分组不同。
func TimedSegmentsForLines(u Utterance, lines []string) []TimedSegment {
	if len(lines) == 0 {
		return nil
	}
	if segs, ok := assignTimesWithWords(u, lines); ok {
		return segs
	}
	return assignTimesProportional(u, lines)
}

// splitLinesForText 折行管线：标点断句 → 边缘清理 → 超长均分 → 再清理。
// extra 为当次动态补充的中文词（来自 ASR Words），nil 表示只用静态词表。
func splitLinesForText(text string, extra map[string]struct{}, max int) []string {
	if text == "" {
		return nil
	}
	var lines []string
	for _, clause := range splitByPunctuation(text) {
		clause = trimCaptionEdgePunct(clause)
		if clause == "" {
			continue
		}
		for _, line := range splitBalancedPreferIntact(clause, max, extra) {
			line = trimCaptionEdgePunct(line)
			if line == "" {
				continue
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// trimCaptionEdgePunct 去掉首尾空白与断句标点（含 … / ...），使成片行首行尾非标点。
func trimCaptionEdgePunct(s string) string {
	runes := []rune(strings.TrimSpace(s))
	for len(runes) > 0 {
		if unicode.IsSpace(runes[0]) {
			runes = runes[1:]
			continue
		}
		if len(runes) >= 3 && runes[0] == '.' && runes[1] == '.' && runes[2] == '.' {
			runes = runes[3:]
			continue
		}
		if runes[0] == '…' || isBreakPunctRune(runes[0]) {
			runes = runes[1:]
			continue
		}
		break
	}
	for len(runes) > 0 {
		last := len(runes) - 1
		if unicode.IsSpace(runes[last]) {
			runes = runes[:last]
			continue
		}
		if len(runes) >= 3 && runes[last] == '.' && runes[last-1] == '.' && runes[last-2] == '.' {
			runes = runes[:last-2]
			continue
		}
		if runes[last] == '…' || isBreakPunctRune(runes[last]) {
			runes = runes[:last]
			continue
		}
		break
	}
	return string(runes)
}

func splitByPunctuation(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	var out []string
	var cur []rune
	for i := 0; i < len(runes); {
		// 英文省略号 ...
		if i+2 < len(runes) && runes[i] == '.' && runes[i+1] == '.' && runes[i+2] == '.' {
			cur = append(cur, '.', '.', '.')
			out = append(out, string(cur))
			cur = cur[:0]
			i += 3
			continue
		}
		r := runes[i]
		cur = append(cur, r)
		i++
		if r == '…' || isBreakPunctRune(r) {
			// 小数点夹在数字之间不断句（如 3.14）
			if isDecimalPoint(r) && len(cur) >= 2 && i < len(runes) &&
				unicode.IsDigit(cur[len(cur)-2]) && unicode.IsDigit(runes[i]) {
				continue
			}
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func isBreakPunctRune(r rune) bool {
	switch r {
	case '，', '。', '！', '？', '；', '：', '、',
		',', '.', '!', '?', ';', ':':
		return true
	default:
		return false
	}
}

func tokenizeCaptionAtoms(text string, extra map[string]struct{}) []captionAtom {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	out := make([]captionAtom, 0, len(runes))
	for i := 0; i < len(runes); {
		if end := matchNumericToken(runes, i); end > i {
			s := string(runes[i:end])
			out = append(out, captionAtom{text: s, keepIntact: true, runes: end - i})
			i = end
			continue
		}
		if isLatinLetter(runes[i]) {
			j := i + 1
			for j < len(runes) && isLatinLetter(runes[j]) {
				j++
			}
			s := string(runes[i:j])
			out = append(out, captionAtom{text: s, keepIntact: true, runes: j - i})
			i = j
			continue
		}
		if end := matchCJKWord(runes, i, extra); end > i {
			s := string(runes[i:end])
			out = append(out, captionAtom{text: s, keepIntact: true, runes: end - i})
			i = end
			continue
		}
		out = append(out, captionAtom{text: string(runes[i]), keepIntact: false, runes: 1})
		i++
	}
	return out
}

// matchNumericToken 从 i 起匹配数字词：可选货币前缀 + 数字(含小数) + 可选单位后缀。
// 未匹配时返回 i。
func matchNumericToken(runes []rune, i int) int {
	if i >= len(runes) {
		return i
	}
	start := i
	if isCurrencyPrefix(runes[i]) {
		if i+1 >= len(runes) || !unicode.IsDigit(runes[i+1]) {
			return start
		}
		i++
	}
	if i >= len(runes) || !unicode.IsDigit(runes[i]) {
		return start
	}
	for i < len(runes) {
		if unicode.IsDigit(runes[i]) {
			i++
			continue
		}
		if isDecimalPoint(runes[i]) && i+1 < len(runes) && unicode.IsDigit(runes[i+1]) {
			i++
			continue
		}
		break
	}
	if i < len(runes) && isNumericSuffix(runes[i]) {
		i++
	}
	return i
}

func isCurrencyPrefix(r rune) bool {
	switch r {
	case '¥', '$', '€', '£', '￥':
		return true
	default:
		return false
	}
}

func isNumericSuffix(r rune) bool {
	switch r {
	case '%', '％', '‰', '°', '℃':
		return true
	default:
		return false
	}
}

func isDecimalPoint(r rune) bool {
	return r == '.' || r == '．'
}

func isLatinLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// splitBalancedPreferIntact 超长时按最少行数折行：行边界只能落在原子边界上
// （英文词/数字词/词典中文词整段不可切），每行切点取离均分目标最近的那个。
//
// 与「累加到超过均分目标就封口」的旧写法相比有两处差异：
//  1. 切点从「位置驱动」改成「在目标位附近搜合法边界」。旧写法在原子偏大时会提前很远封口，
//     留下 1~2 个字的孤行（实测占超长小句的近四分之一）。
//  2. 行数由「剩余文本最少还要几行」的可行解推出，而不是硬取 ceil(n/max)。后者在标点/词原子
//     挡住目标位时无解，只能挤出一个超宽行。
func splitBalancedPreferIntact(text string, max int, extra map[string]struct{}) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	n := len(runes)
	if n <= max {
		return []string{text}
	}

	// cuts 为升序合法切点（含 0 与 n）；行边界只能落在原子边界上。
	atoms := tokenizeCaptionAtoms(text, extra)
	cuts := make([]int, 1, len(atoms)+1)
	off := 0
	for _, atom := range atoms {
		off += atom.runes
		cuts = append(cuts, off)
	}

	// minLines[i]：从 cuts[i] 起到文本末尾，每行不超过 max 折行所需的最少行数；-1 表示放不下。
	minLines := make([]int, len(cuts))
	for i := len(cuts) - 1; i >= 0; i-- {
		if cuts[i] == n {
			minLines[i] = 0
			continue
		}
		minLines[i] = -1
		for j := i + 1; j < len(cuts); j++ {
			if cuts[j]-cuts[i] > max {
				// 超宽只允许发生在「一个原子比 max 还长」时：让它独占一行。
				if j == i+1 && minLines[j] >= 0 {
					minLines[i] = minLines[j] + 1
				}
				break
			}
			if minLines[j] < 0 {
				continue
			}
			if minLines[i] < 0 || minLines[j]+1 < minLines[i] {
				minLines[i] = minLines[j] + 1
			}
		}
	}

	parts := minLines[0]
	if parts <= 0 {
		return []string{text}
	}

	lines := make([]string, 0, parts)
	at := 0
	for k := 0; k < parts-1; k++ {
		rest := parts - 1 - k // 本行之后还剩几行
		ideal := balancedCut(n, parts, k+1)
		best, bestDist := -1, 0
		for j := at + 1; j < len(cuts); j++ {
			if cuts[j]-cuts[at] > max {
				// 只能吃掉一个超长原子（与 minLines 的判定保持一致）。
				if j == at+1 && minLines[j] == rest {
					best = j
				}
				break
			}
			if minLines[j] != rest {
				continue
			}
			dist := cuts[j] - ideal
			if dist < 0 {
				dist = -dist
			}
			// 距离相同取靠前的切点：原子边界本身可能是分词误合并（如 上市|公司 之于「上市公司」），
			// 往前切能把它整体留给下一行，不会拦腰截断。
			if best < 0 || dist < bestDist {
				best, bestDist = j, dist
			}
		}
		if best < 0 {
			break
		}
		lines = append(lines, string(runes[cuts[at]:cuts[best]]))
		at = best
	}
	if at < len(cuts)-1 {
		lines = append(lines, string(runes[cuts[at]:]))
	}
	return lines
}

// balancedCut 返回把 n 个字均分到 parts 行时，第 idx 行末尾的名义切点（idx 从 1 起）：
// 前 n%parts 行各多一个字。
func balancedCut(n, parts, idx int) int {
	base, rem := n/parts, n%parts
	if idx < rem {
		return base*idx + idx
	}
	return base*idx + rem
}

// splitBalancedPreferLatin 保留旧名，供既有测试与调用兼容。
func splitBalancedPreferLatin(text string, max int) []string {
	return splitBalancedPreferIntact(text, max, nil)
}

func stripBreakPunct(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); {
		if i+2 < len(runes) && runes[i] == '.' && runes[i+1] == '.' && runes[i+2] == '.' {
			i += 3
			continue
		}
		r := runes[i]
		i++
		if r == '…' || isBreakPunctRune(r) {
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func assignTimesWithWords(u Utterance, lines []string) ([]TimedSegment, bool) {
	if len(u.Words) == 0 {
		return nil, false
	}
	cur := wordCursor{}
	out := make([]TimedSegment, 0, len(lines))
	for i, line := range lines {
		need := stripBreakPunct(line)
		start, end, ok := cur.consume(need, u.Words)
		if !ok {
			return nil, false
		}
		if need == "" {
			// 纯标点：贴在当前游标位置，尽量给出极短有效区间。
			start, end = cur.peekTime(u.Words, u.StartTime, u.EndTime)
		}
		if end <= start {
			if i == len(lines)-1 {
				end = u.EndTime
			}
			if end <= start {
				end = start + 1
			}
		}
		out = append(out, TimedSegment{Text: line, StartTime: start, EndTime: end})
	}
	return out, true
}

type wordCursor struct {
	idx        int
	runeOffset int
}

func (c *wordCursor) peekTime(words []Word, fallbackStart, fallbackEnd int64) (int64, int64) {
	if c.idx < len(words) {
		w := words[c.idx]
		if c.runeOffset == 0 {
			return w.StartTime, w.StartTime + 1
		}
		wr := []rune(w.Text)
		if len(wr) == 0 {
			return w.StartTime, w.EndTime
		}
		t := interpolateWordTime(w, c.runeOffset, len(wr))
		return t, t + 1
	}
	if len(words) > 0 {
		t := words[len(words)-1].EndTime
		return t, t + 1
	}
	return fallbackStart, fallbackEnd
}

func (c *wordCursor) consume(need string, words []Word) (startTime, endTime int64, ok bool) {
	needRunes := []rune(need)
	if len(needRunes) == 0 {
		return 0, 0, true
	}
	first := true
	for _, nr := range needRunes {
		matched := false
		for c.idx < len(words) {
			wr := []rune(words[c.idx].Text)
			if len(wr) == 0 {
				c.idx++
				c.runeOffset = 0
				continue
			}
			if c.runeOffset >= len(wr) {
				c.idx++
				c.runeOffset = 0
				continue
			}
			if wr[c.runeOffset] != nr {
				return 0, 0, false
			}
			w := words[c.idx]
			t := interpolateWordTime(w, c.runeOffset, len(wr))
			tEnd := interpolateWordTimeEnd(w, c.runeOffset, len(wr))
			if first {
				startTime = t
				first = false
			}
			endTime = tEnd
			c.runeOffset++
			matched = true
			break
		}
		if !matched {
			return 0, 0, false
		}
	}
	return startTime, endTime, true
}

func interpolateWordTime(w Word, runeIdx, runeCount int) int64 {
	if runeCount <= 1 {
		return w.StartTime
	}
	dur := w.EndTime - w.StartTime
	return w.StartTime + dur*int64(runeIdx)/int64(runeCount)
}

func interpolateWordTimeEnd(w Word, runeIdx, runeCount int) int64 {
	if runeCount <= 0 {
		return w.EndTime
	}
	if runeIdx >= runeCount-1 {
		return w.EndTime
	}
	dur := w.EndTime - w.StartTime
	return w.StartTime + dur*int64(runeIdx+1)/int64(runeCount)
}

func assignTimesProportional(u Utterance, lines []string) []TimedSegment {
	total := 0
	lens := make([]int, len(lines))
	for i, l := range lines {
		n := utf8.RuneCountInString(l)
		if n == 0 {
			n = 1
		}
		lens[i] = n
		total += n
	}
	if total == 0 {
		return nil
	}
	dur := u.EndTime - u.StartTime
	if dur < 0 {
		dur = 0
	}
	out := make([]TimedSegment, 0, len(lines))
	cum := 0
	for i, line := range lines {
		start := u.StartTime + dur*int64(cum)/int64(total)
		cum += lens[i]
		end := u.StartTime + dur*int64(cum)/int64(total)
		if i == len(lines)-1 {
			end = u.EndTime
		}
		if end <= start {
			end = start + 1
		}
		out = append(out, TimedSegment{Text: line, StartTime: start, EndTime: end})
	}
	return out
}
