package service

import (
	"fmt"
	"sort"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

const (
	// aiSliceProjectMaxDurationMS 单个剪辑项目 clips0 总时长上限（30 分钟）。
	aiSliceProjectMaxDurationMS int64 = 30 * 60 * 1000
	// aiSliceProjectMinDurationMS 拆分后单个项目 clips0 总时长下限（5 分钟）。
	// 用户提交总时长本身不足 5 分钟时不拆分，仍创建 1 个项目。
	aiSliceProjectMinDurationMS int64 = 5 * 60 * 1000

	aiSliceProjectNamePrefix      = "AI选片"
	aiSliceDraftProjectNamePrefix = "一键成片"
)

// clipAtom 选区内不可再切的最小时间片：边界对齐 ASR 分句，避免把一句话拆进两个项目。
type clipAtom struct {
	model.ClipRange
	atParagraphEnd bool
}

func clipRangesDurationMS(clips []model.ClipRange) int64 {
	var sum int64
	for _, c := range clips {
		if c.EndTime > c.StartTime {
			sum += c.EndTime - c.StartTime
		}
	}
	return sum
}

func atomsDurationMS(atoms []clipAtom) int64 {
	var sum int64
	for _, a := range atoms {
		if a.EndTime > a.StartTime {
			sum += a.EndTime - a.StartTime
		}
	}
	return sum
}

// splitClips0IntoProjects 将已排序合并的 clips0 按 30 分钟上限拆成多组。
// 总时长 ≤30 分钟时原样返回 1 组；>30 分钟时按 live_asr 分句边界切原子，并优先在 asr_paragraphs 段末切开。
// 保证各组时长之和等于入参总时长（不丢选区）。
func splitClips0IntoProjects(merged []model.ClipRange, utterances []asr.Utterance, paragraphs []model.ASRParagraph) [][]model.ClipRange {
	if len(merged) == 0 {
		return nil
	}
	total := clipRangesDurationMS(merged)
	if total <= 0 {
		return nil
	}
	if total <= aiSliceProjectMaxDurationMS {
		return [][]model.ClipRange{cloneClipRanges(merged)}
	}

	atoms := buildClipAtoms(merged, utterances, paragraphs)
	if len(atoms) == 0 {
		return [][]model.ClipRange{cloneClipRanges(merged)}
	}

	groups := packClipAtoms(atoms, aiSliceProjectMaxDurationMS, aiSliceProjectMinDurationMS)
	out := make([][]model.ClipRange, 0, len(groups))
	for _, g := range groups {
		clips := mergeContiguousAtoms(g)
		if len(clips) == 0 {
			continue
		}
		out = append(out, clips)
	}
	if len(out) == 0 {
		return [][]model.ClipRange{cloneClipRanges(merged)}
	}
	return out
}

func cloneClipRanges(clips []model.ClipRange) []model.ClipRange {
	out := make([]model.ClipRange, len(clips))
	copy(out, clips)
	return out
}

func paragraphEndSet(paragraphs []model.ASRParagraph) map[int64]struct{} {
	out := make(map[int64]struct{}, len(paragraphs))
	for _, p := range paragraphs {
		if p.EndTime > p.StartTime {
			out[p.EndTime] = struct{}{}
		}
	}
	return out
}

func utteranceBoundarySet(utterances []asr.Utterance) map[int64]struct{} {
	out := make(map[int64]struct{}, len(utterances)*2)
	for _, u := range utterances {
		if u.EndTime <= u.StartTime {
			continue
		}
		out[u.StartTime] = struct{}{}
		out[u.EndTime] = struct{}{}
	}
	return out
}

// buildClipAtoms 在每个合并选区内，仅在 ASR 分句起止（及选区端点）切开。
// 无分句时退化为按 30 分钟硬切，避免超长静音无法拆分。
func buildClipAtoms(merged []model.ClipRange, utterances []asr.Utterance, paragraphs []model.ASRParagraph) []clipAtom {
	paraEnds := paragraphEndSet(paragraphs)
	boundaries := utteranceBoundarySet(utterances)
	hasUtterances := len(boundaries) > 0

	var atoms []clipAtom
	for _, clip := range merged {
		if clip.EndTime <= clip.StartTime {
			continue
		}
		cuts := collectAtomCutPoints(clip, boundaries, hasUtterances)
		for i := 0; i < len(cuts)-1; i++ {
			start, end := cuts[i], cuts[i+1]
			if end <= start {
				continue
			}
			_, atPara := paraEnds[end]
			atoms = append(atoms, clipAtom{
				ClipRange:      model.ClipRange{StartTime: start, EndTime: end},
				atParagraphEnd: atPara,
			})
		}
	}
	return atoms
}

func collectAtomCutPoints(clip model.ClipRange, utteranceBoundaries map[int64]struct{}, hasUtterances bool) []int64 {
	set := map[int64]struct{}{
		clip.StartTime: {},
		clip.EndTime:   {},
	}
	if hasUtterances {
		for t := range utteranceBoundaries {
			if t > clip.StartTime && t < clip.EndTime {
				set[t] = struct{}{}
			}
		}
	} else {
		for t := clip.StartTime + aiSliceProjectMaxDurationMS; t < clip.EndTime; t += aiSliceProjectMaxDurationMS {
			set[t] = struct{}{}
		}
	}
	cuts := make([]int64, 0, len(set))
	for t := range set {
		cuts = append(cuts, t)
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })
	return cuts
}

func packClipAtoms(atoms []clipAtom, maxMS, minMS int64) [][]clipAtom {
	if len(atoms) == 0 {
		return nil
	}
	var groups [][]clipAtom
	for i := 0; i < len(atoms); {
		end := findAtomCutIndex(atoms, i, maxMS, minMS)
		if end < i {
			end = i
		}
		groups = append(groups, cloneAtoms(atoms[i:end+1]))
		i = end + 1
	}
	return rebalanceMinDuration(groups, minMS)
}

func cloneAtoms(src []clipAtom) []clipAtom {
	out := make([]clipAtom, len(src))
	copy(out, src)
	return out
}

// findAtomCutIndex 从 start 起尽量装满 maxMS；若窗口内有段落结束且该段 ≥minMS，则对齐到最后一段段落末。
func findAtomCutIndex(atoms []clipAtom, start int, maxMS, minMS int64) int {
	if start >= len(atoms) {
		return start
	}
	lastCut := -1
	lastPara := -1
	var acc int64
	for i := start; i < len(atoms); i++ {
		d := atoms[i].EndTime - atoms[i].StartTime
		if d < 0 {
			d = 0
		}
		next := acc + d
		if next > maxMS && lastCut >= start {
			break
		}
		acc = next
		lastCut = i
		if atoms[i].atParagraphEnd {
			lastPara = i
		}
		if next > maxMS {
			break
		}
	}
	if lastCut < start {
		return start
	}
	if lastPara >= start {
		paraDur := atomsDurationMS(atoms[start : lastPara+1])
		if paraDur >= minMS {
			return lastPara
		}
	}
	return lastCut
}

// rebalanceMinDuration 从尾部向前：末组 <min 时整颗原子挪到末组；无法保持前组 ≥min 则与前组合并（允许超过 max，避免丢片）。
func rebalanceMinDuration(groups [][]clipAtom, minMS int64) [][]clipAtom {
	if len(groups) <= 1 {
		return groups
	}
	for len(groups) >= 2 {
		last := groups[len(groups)-1]
		if atomsDurationMS(last) >= minMS {
			break
		}
		prev := groups[len(groups)-2]
		if len(prev) == 0 {
			groups = append(groups[:len(groups)-2], last)
			continue
		}
		stolen := prev[len(prev)-1]
		prevRest := prev[:len(prev)-1]
		if atomsDurationMS(prevRest) < minMS {
			merged := append(cloneAtoms(prev), last...)
			groups[len(groups)-2] = merged
			groups = groups[:len(groups)-1]
			continue
		}
		groups[len(groups)-2] = cloneAtoms(prevRest)
		groups[len(groups)-1] = append([]clipAtom{stolen}, last...)
	}
	return groups
}

func mergeContiguousAtoms(atoms []clipAtom) []model.ClipRange {
	if len(atoms) == 0 {
		return nil
	}
	out := make([]model.ClipRange, 0, len(atoms))
	cur := atoms[0].ClipRange
	for i := 1; i < len(atoms); i++ {
		next := atoms[i].ClipRange
		if next.StartTime <= cur.EndTime {
			if next.EndTime > cur.EndTime {
				cur.EndTime = next.EndTime
			}
			continue
		}
		out = append(out, cur)
		cur = next
	}
	out = append(out, cur)
	return out
}

func autoSliceProjectNames(prefix string, n int, now time.Time) []string {
	if n <= 0 {
		return nil
	}
	base := fmt.Sprintf("%s_%s", prefix, now.Format("2006-01-02_15:04:05"))
	if n == 1 {
		return []string{base}
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("%s_%d", base, i+1)
	}
	return out
}
