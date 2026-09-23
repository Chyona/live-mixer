package asr

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 断句结果不可用的分类原因：调用方据此回退规则折行，并写入诊断产物归因。
const (
	// CaptionLinesReasonEmpty 无有效行（原文为空或无断句行）。
	CaptionLinesReasonEmpty = "empty"
	// CaptionLinesReasonBlankLine 存在空白行。
	CaptionLinesReasonBlankLine = "blank_line"
	// CaptionLinesReasonMismatch 行拼接与原文不一致（被改写、增删字或漏字）。
	CaptionLinesReasonMismatch = "mismatch"
	// CaptionLinesReasonTooFine 行数超过「每行满宽」所需上限（切得过碎）。
	CaptionLinesReasonTooFine = "too_fine"
	// CaptionLinesReasonTokenCut 切点落在西文词/数字词/词表中文词内部。
	CaptionLinesReasonTokenCut = "token_cut"
)

// ErrCaptionLines 断句结果不可用的哨兵错误；调用方用 errors.Is 判定后回退 SplitLinesByRule。
var ErrCaptionLines = errors.New("字幕断句结果不可用")

// CaptionLinesError 带分类原因的断句校验错误。
type CaptionLinesError struct {
	// Reason 取值见 CaptionLinesReason* 常量。
	Reason string
	// Detail 现场描述，仅用于日志与诊断。
	Detail string
}

// Error 实现 error。
func (e *CaptionLinesError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s: %s", ErrCaptionLines, e.Reason)
	}
	return fmt.Sprintf("%s: %s (%s)", ErrCaptionLines, e.Reason, e.Detail)
}

// Unwrap 支持 errors.Is(err, ErrCaptionLines)。
func (e *CaptionLinesError) Unwrap() error { return ErrCaptionLines }

// ValidateCaptionLines 校验外部（LLM）给出的字幕行是否来自原文，并施加硬约束。
//
// 四项校验：内容一致（每行剥掉首尾标点后必须是原文的连续子串、顺序一致，见 locateCaptionLines）、
// 无空白行、行数不超过 ceil(字数/max)+1（防切碎）、切点不落在西文词/数字词/静态词表中文词内部。
// 校验通过后施加两条产品规则（与规则折行一致）：超长行按 SplitLinesByRuleMax 就地再切、
// 剥离行首尾断句标点——行长上限与「行首行尾非标点」由代码保证，不依赖模型听话。
// 失败返回 *CaptionLinesError（errors.Is(err, ErrCaptionLines) 为真），调用方应回退 SplitLinesByRule。
func ValidateCaptionLines(text string, lines []string, max int) ([]string, error) {
	if max <= 0 {
		max = MaxCaptionRunes
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, &CaptionLinesError{Reason: CaptionLinesReasonEmpty, Detail: "原文为空"}
	}
	if len(lines) == 0 {
		return nil, &CaptionLinesError{Reason: CaptionLinesReasonEmpty, Detail: "无断句行"}
	}
	insideToken, boundaries := captionTokenLayout(text)
	spans, err := locateCaptionLines(text, lines, insideToken)
	if err != nil {
		return nil, err
	}
	if limit := (utf8.RuneCountInString(text)+max-1)/max + 1; len(lines) > limit {
		return nil, &CaptionLinesError{
			Reason: CaptionLinesReasonTooFine,
			Detail: fmt.Sprintf("行数 %d 超过上限 %d", len(lines), limit),
		}
	}
	if off, ok := firstCutInsideToken(spans, boundaries); !ok {
		return nil, &CaptionLinesError{
			Reason: CaptionLinesReasonTokenCut,
			Detail: fmt.Sprintf("第 %d 个字后的切点落在词/数字/英文内部", off),
		}
	}
	return normalizeCaptionLines(lines, max), nil
}

// captionLineSpan 单行内容在原文中的位置（rune 下标，左闭右开）。
type captionLineSpan struct {
	Start int
	End   int
}

// locateCaptionLines 逐行在原文中定位：先跳过行首允许丢掉的标点/空白，再要求该行（剥掉首尾标点后）
// 与原文逐字相同；全部行定位完，原文尾部也只允许剩标点/空白。
//
// 允许「行边界丢标点」是照着实测模型行为定的：模型会输出「这款产品原价是￥199」「现在直播间下单只要98折」
// 这种不带边界标点的结果，而产品规则本来就要剥掉行首行尾标点——这类丢标点不算改动原文。
// 但内容字符一律不许动：增删改重排都会让「连续子串 + 顺序一致」这两个条件之一失败，
// 数字词/西文词/词表词内部的字符（如 3.14 的小数点）也不允许被行边界丢掉。
func locateCaptionLines(text string, lines []string, insideToken []bool) ([]captionLineSpan, error) {
	runes := []rune(text)
	spans := make([]captionLineSpan, 0, len(lines))
	pos := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return nil, &CaptionLinesError{
				Reason: CaptionLinesReasonBlankLine,
				Detail: fmt.Sprintf("第 %d 行为空白", i+1),
			}
		}
		core := []rune(trimCaptionEdgePunct(trimmed))
		if len(core) == 0 {
			return nil, &CaptionLinesError{
				Reason: CaptionLinesReasonMismatch,
				Detail: fmt.Sprintf("第 %d 行剥掉标点后为空", i+1),
			}
		}
		pos = skipDroppableRunAtLineEdge(runes, pos, insideToken)
		if !hasRunesAt(runes, pos, core) {
			return nil, &CaptionLinesError{
				Reason: CaptionLinesReasonMismatch,
				Detail: fmt.Sprintf("第 %d 行（%s）与原文第 %d 个字起的内容不符", i+1, truncateRunes(core, 20), pos+1),
			}
		}
		spans = append(spans, captionLineSpan{Start: pos, End: pos + len(core)})
		pos += len(core)
	}
	if tail := skipDroppableRunAtLineEdge(runes, pos, insideToken); tail != len(runes) {
		return nil, &CaptionLinesError{
			Reason: CaptionLinesReasonMismatch,
			Detail: fmt.Sprintf("原文第 %d 个字起的内容没出现在任何行里", tail+1),
		}
	}
	return spans, nil
}

// skipDroppableRunAtLineEdge 从 at 起跳过连续的「行边界可丢字符」并返回新位置。
// 可丢 = 空白/断句标点（含 …）且不落在不可拆原子内部：前者本来就会被剥掉，后者是原文内容。
func skipDroppableRunAtLineEdge(runes []rune, at int, insideToken []bool) int {
	for at < len(runes) && isCaptionEdgeDroppableRune(runes[at]) && !insideToken[at] {
		at++
	}
	return at
}

// hasRunesAt 判断 runes[at:] 是否以 want 开头。
func hasRunesAt(runes []rune, at int, want []rune) bool {
	if at < 0 || at+len(want) > len(runes) {
		return false
	}
	for i, r := range want {
		if runes[at+i] != r {
			return false
		}
	}
	return true
}

// isCaptionEdgeDroppableRune 行边界允许丢掉的字符：空白与断句标点（含 …）。
// 与 trimCaptionEdgePunct 的剥离集合一致——这些字符本来就不会出现在成片字幕里。
func isCaptionEdgeDroppableRune(r rune) bool {
	return unicode.IsSpace(r) || r == '…' || isBreakPunctRune(r)
}

// truncateRunes 截断用于错误详情的行文本，避免日志被长行灌爆。
func truncateRunes(runes []rune, max int) string {
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return string(runes)
}

// captionTokenLayout 计算文本的原子布局：
// mask 标记落在「不可拆原子」（西文词/数字词/词表中文词）内部的字符位置——这些字符是原文内容的一部分，
// 既不允许被行边界悄悄丢掉，也不允许作为切点；
// boundaries 是所有合法切点（原子边界）的集合。
func captionTokenLayout(text string) (mask []bool, boundaries map[int]struct{}) {
	runes := []rune(text)
	mask = make([]bool, len(runes))
	boundaries = make(map[int]struct{}, len(runes)+1)
	boundaries[0] = struct{}{}
	off := 0
	for _, atom := range tokenizeCaptionAtoms(text, nil) {
		if atom.keepIntact {
			for p := off + 1; p < off+atom.runes; p++ {
				mask[p] = true
			}
		}
		off += atom.runes
		boundaries[off] = struct{}{}
	}
	return mask, boundaries
}

// normalizeCaptionLines 施加两条产品规则：超长行按规则再切、剥离行首尾断句标点。
// 与 SplitUtteranceForCaptions 同规则，因此返回行可能不再与原文逐字相等（标点按产品要求丢弃）；
// 时间映射读的也是剥标点后的行，与规则路径同构。
func normalizeCaptionLines(lines []string, max int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := []string{line}
		if utf8.RuneCountInString(line) > max {
			if re := SplitLinesByRuleMax(line, max); len(re) > 0 {
				parts = re
			}
		}
		for _, part := range parts {
			if trimmed := trimCaptionEdgePunct(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

// firstCutInsideToken 检查每个行边界是否都落在原子边界上（西文词、数字词、词表中文词整段不可切）。
// 只能判定「切在某个词内部」；词与词之间的组合该不该拆（如「人工/智能」）需要语义，由断句方负责。
// 返回首个越界切点的偏移（rune 数）与是否全部合法。
func firstCutInsideToken(spans []captionLineSpan, boundaries map[int]struct{}) (int, bool) {
	for _, span := range spans[:len(spans)-1] {
		if _, ok := boundaries[span.End]; !ok {
			return span.End, false
		}
	}
	return 0, true
}
