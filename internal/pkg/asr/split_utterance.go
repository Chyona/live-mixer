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
	keepIntact bool // 英文词 / 数字词整段不拆
	runes      int
}

// SplitUtteranceForCaptions 将一句 ASR 按标点断句，超长则均分（英文词与数字词不拆），并切分时间。
// 成片每行会剥离首尾断句标点，保证行首行尾均非标点。
func SplitUtteranceForCaptions(u Utterance) []TimedSegment {
	text := strings.TrimSpace(u.Text)
	if text == "" {
		return nil
	}

	var lines []string
	for _, clause := range splitByPunctuation(text) {
		clause = trimCaptionEdgePunct(clause)
		if clause == "" {
			continue
		}
		for _, line := range splitBalancedPreferIntact(clause, MaxCaptionRunes) {
			line = trimCaptionEdgePunct(line)
			if line == "" {
				continue
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}

	if segs, ok := assignTimesWithWords(u, lines); ok {
		return segs
	}
	return assignTimesProportional(u, lines)
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

func tokenizeCaptionAtoms(text string) []captionAtom {
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

// splitBalancedPreferIntact 超长时按最少行数均分；英文词与数字词整段不拆。
func splitBalancedPreferIntact(text string, max int) []string {
	if text == "" {
		return nil
	}
	n := utf8.RuneCountInString(text)
	if n <= max {
		return []string{text}
	}

	atoms := tokenizeCaptionAtoms(text)
	parts := (n + max - 1) / max
	base, rem := n/parts, n%parts
	targets := make([]int, parts)
	for i := 0; i < parts; i++ {
		targets[i] = base
		if i < rem {
			targets[i]++
		}
	}

	targetOf := func(idx int) int {
		if idx < len(targets) {
			return targets[idx]
		}
		return max
	}

	var lines []string
	var cur strings.Builder
	curLen := 0
	targetIdx := 0

	flush := func() {
		if curLen == 0 {
			return
		}
		lines = append(lines, cur.String())
		cur.Reset()
		curLen = 0
		targetIdx++
	}

	for _, atom := range atoms {
		if curLen == 0 {
			cur.WriteString(atom.text)
			curLen = atom.runes
			continue
		}
		next := curLen + atom.runes
		tgt := targetOf(targetIdx)
		if next <= tgt {
			cur.WriteString(atom.text)
			curLen = next
			continue
		}
		// 超过当前行目标。
		if next <= max {
			if atom.keepIntact {
				// 英文/数字整词优先换行，避免为凑目标而跨行切开。
				flush()
				cur.WriteString(atom.text)
				curLen = atom.runes
				continue
			}
			// 单字 Other：封口后起新行，保证纯中文均分与 targets 一致。
			flush()
			cur.WriteString(atom.text)
			curLen = atom.runes
			continue
		}
		// 超过上限：封口，整原子起新行（超长 keepIntact 允许该行 > max）。
		flush()
		cur.WriteString(atom.text)
		curLen = atom.runes
	}
	flush()
	return lines
}

// splitBalancedPreferLatin 保留旧名，供既有测试与调用兼容。
func splitBalancedPreferLatin(text string, max int) []string {
	return splitBalancedPreferIntact(text, max)
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
