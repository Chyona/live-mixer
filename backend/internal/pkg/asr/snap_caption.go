package asr

import (
	"fmt"
	"math"
)

// CaptionLinesReasonUnsnappable 吸附失败：LLM 给的行数在原文本的合法词边界上排不出来
// （行数偏少、或原子太大放不下），只能整条回退规则折行。
const CaptionLinesReasonUnsnappable = "unsnappable"

const (
	// captionSnapMinLineRunes 孤行的行宽下限：窄于它的行只加软惩罚，不做硬约束。
	captionSnapMinLineRunes = 3
	// captionSnapOrphanPenalty 孤行折算的位移代价（字）：只有当挪开它省下的位移更多时才会挪。
	captionSnapOrphanPenalty = 3.0
)

// CaptionSnapStats 吸附统计，供调用方打点：切点被挪动的比例是「提示词要不要喂词边界」的依据。
type CaptionSnapStats struct {
	// CutCount 切点总数（行数 - 1）。
	CutCount int
	// MovedCuts 被挪到别的合法边界的切点数。
	MovedCuts int
	// Displaced 所有切点位移的绝对值之和（字）。
	Displaced int
}

// SnapCaptionLines 校验外部（LLM）断句并把它「吸附」到最近的合法词边界上。
//
// 与 ValidateCaptionLines 只差最后一步：切点落在词内部时不再整条判死，而是在**保持行数不变**的
// 前提下把所有切点一起挪到最近的合法边界（见 snapCuts）。这样模型选的行数/语义分段仍被尊重，
// 只在位置上纠偏；而整条拒绝的代价是白花一次调用、且退回不看语义的规则折行。
//
// 硬约束与 ValidateCaptionLines 一致，且同为硬性（不依赖模型听话）：
// 内容必须逐字来自原文（locateCaptionLines，允许行边界丢标点）、不得有空白行、
// 行数不超过 ceil(字数/max)+1、每行不超过 max（单个比 max 还长的原子独占一行）。
// 校验通过后施加同样的产品规则：超长行按 SplitLinesByRuleMax 就地再切、剥离行首尾标点。
//
// 合法边界取折行路径的原子边界（captionAtomCuts：西文词/数字词/频率词表中文词的接缝），
// 与规则折行共用同一套定义，因此吸附后的切点与规则折行同样「不切词」。
// 吸附不碰字符，只挪行边界，内容一致性不受影响；对已经合法的切点是恒等操作（可重复执行）。
//
// 排不出合法切点时返回 *CaptionLinesError（Reason=CaptionLinesReasonUnsnappable），
// 调用方应回退 SplitLinesByRule；其余失败原因与 ValidateCaptionLines 相同。
func SnapCaptionLines(text string, lines []string, max int) ([]string, CaptionSnapStats, error) {
	var stats CaptionSnapStats
	text, max, err := normalizeCaptionInput(text, lines, max)
	if err != nil {
		return nil, stats, err
	}
	check, err := checkCaptionContent(text, lines, max)
	if err != nil {
		return nil, stats, err
	}

	// 目标切点 = 每行内容的末尾。行边界标点按产品规则会被剥掉，所以目标取剥掉标点后的末尾，
	// 吸附结果里这些标点要么被同一行的首尾清掉，要么留在行内（与规则折行一样是允许的）。
	targets := make([]int, 0, len(check.spans)-1)
	for _, span := range check.spans[:len(check.spans)-1] {
		targets = append(targets, span.End)
	}
	stats.CutCount = len(targets)

	snapped, ok := snapCuts(captionAtomCuts(check.runes, nil), targets, len(check.runes), max)
	if !ok {
		return nil, stats, &CaptionLinesError{
			Reason: CaptionLinesReasonUnsnappable,
			Detail: fmt.Sprintf("%d 个字排成 %d 行时词边界不够用", len(check.runes), len(lines)),
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
		out = append(out, string(check.runes[snapped[i]:snapped[i+1]]))
	}
	return normalizeCaptionLines(out, max), stats, nil
}

// snapCuts 在合法切点 cuts（升序，含 0 与 n）里挑一组切点：个数与 targets 相同（= 行数 - 1），
// 使「切点位移绝对值之和 + 孤行惩罚」最小，且每行不超过 max（单个比 max 长的原子独占一行）。
// 返回切点位置（含首尾的 0 与 n，长度为 len(targets)+2）；排不出来返回 ok=false。
//
// 用 DP 而不是「每个切点各自就近取合法边界」：合法切点必须严格递增、且不能把相邻行挤爆行宽，
// 逐点就近会互相撞车（前一个切点挪远，后一行就超过 max 了）。
func snapCuts(cuts, targets []int, n, max int) ([]int, bool) {
	k := len(targets)
	out := make([]int, k+2)
	out[0], out[k+1] = 0, n
	if k == 0 {
		// 只给了一行：没有切点可吸附。超长时交给 normalizeCaptionLines 再切（与旧行为一致）。
		return out, true
	}
	m := len(cuts)
	if m-2 < k {
		// 原子比切点还少，排不出 k 个内部切点。
		return nil, false
	}

	// prev[j]：上一层（第 i-1 个内部切点）落在 cuts[j] 时的最小代价；choice[i][j] 记录来路。
	prev := make([]float64, m)
	for j := range prev {
		prev[j] = math.Inf(1)
	}
	prev[0] = 0
	choice := make([][]int, k+1)
	for i := 1; i <= k; i++ {
		cur := make([]float64, m)
		ch := make([]int, m)
		for j := range cur {
			cur[j] = math.Inf(1)
			ch[j] = -1
		}
		for j := 1; j <= m-2; j++ {
			dist := cuts[j] - targets[i-1]
			if dist < 0 {
				dist = -dist
			}
			want := float64(dist)
			for p := j - 1; p >= 0; p-- {
				width := cuts[j] - cuts[p]
				if width > max && j != p+1 {
					// 行宽随 p 变小而变大，再往前只会更宽：这里 break 不会漏掉更优解。
					break
				}
				if math.IsInf(prev[p], 1) {
					continue
				}
				// 代价相同取靠前的切点：与 splitBalancedPreferIntact 同一约定，
				// 这样「切点差一个字」的提案能被还原成规则折行本来的切点，两条路径结果趋同。
				if v := prev[p] + want + orphanPenalty(width); v <= cur[j] {
					cur[j], ch[j] = v, p
				}
			}
		}
		prev, choice[i] = cur, ch
	}

	// 最后一行固定收到 n（内容不许多也不许少），因此只扫 m-2 之前的边界。
	best, bestAt := math.Inf(1), -1
	for p := m - 2; p >= 0; p-- {
		width := n - cuts[p]
		if width > max && p != m-2 {
			break
		}
		if math.IsInf(prev[p], 1) {
			continue
		}
		if v := prev[p] + orphanPenalty(width); v <= best {
			best, bestAt = v, p
		}
	}
	if bestAt < 0 {
		return nil, false
	}

	idx := make([]int, k+2)
	idx[0], idx[k+1] = 0, m-1
	for i, j := k, bestAt; i >= 1; i-- {
		idx[i] = j
		j = choice[i][j]
	}
	for i := range idx {
		out[i] = cuts[idx[i]]
	}
	return out, true
}

// orphanPenalty 孤行惩罚：窄于 captionSnapMinLineRunes 的行折算成一段位移代价。
func orphanPenalty(width int) float64 {
	if width < captionSnapMinLineRunes {
		return captionSnapOrphanPenalty
	}
	return 0
}
