package service

import (
	"fmt"
	"sort"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
)

const (
	// aiSliceProjectMaxASRMS 单个剪辑项目有效 ASR 时长上限（30 分钟）。
	// clips0 为视频时间范围：含大量静音时墙钟时长可以超过 30 分钟。
	aiSliceProjectMaxASRMS int64 = 30 * 60 * 1000
	// aiSliceProjectMinASRMS 拆分后单个项目有效 ASR 时长下限（10 分钟）。
	// 用户提交的有效 ASR 本身不足 10 分钟时不拆分，仍创建 1 个项目。
	aiSliceProjectMinASRMS int64 = 10 * 60 * 1000
	// aiSliceProjectTargetWallMS 按选区墙钟时长估算项目数：N 分钟约 N/30 个（可多 1 或少 1）。
	aiSliceProjectTargetWallMS int64 = 30 * 60 * 1000

	aiSliceProjectNamePrefix      = "AI选片"
	aiSliceDraftProjectNamePrefix = "一键成片"
)

// clipAtom 选区内不可再切的最小时间片：边界对齐 ASR 分句，避免把一句话拆进两个项目。
type clipAtom struct {
	model.ClipRange
	atParagraphEnd bool
	asrMS          int64
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

func clipRangesASRDurationMS(clips []model.ClipRange, utterances []asr.Utterance) int64 {
	if len(clips) == 0 {
		return 0
	}
	if len(utterances) == 0 {
		return clipRangesDurationMS(clips)
	}
	var sum int64
	for _, c := range clips {
		if c.EndTime <= c.StartTime {
			continue
		}
		for _, u := range utterances {
			if u.EndTime <= u.StartTime {
				continue
			}
			start := c.StartTime
			if u.StartTime > start {
				start = u.StartTime
			}
			end := c.EndTime
			if u.EndTime < end {
				end = u.EndTime
			}
			if end > start {
				sum += end - start
			}
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

func atomsASRDuration(atoms []clipAtom) int64 {
	var sum int64
	for _, a := range atoms {
		if a.asrMS > 0 {
			sum += a.asrMS
		}
	}
	return sum
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// targetProjectCount 估算拆分项目数：约 N/30（N 为选区墙钟分钟），并受 ASR 上下限约束。
// 有效 ASR ≤30 分钟只需 1 组；每组有效 ASR 须 ≥10 分钟，因此静音很多时项目数会少于 N/30。
func targetProjectCount(wallMS, asrMS int64) int {
	if wallMS <= 0 {
		return 0
	}
	target := int((wallMS + aiSliceProjectTargetWallMS/2) / aiSliceProjectTargetWallMS)
	if target < 1 {
		target = 1
	}

	minGroups := 1
	if asrMS > aiSliceProjectMaxASRMS {
		minGroups = int((asrMS + aiSliceProjectMaxASRMS - 1) / aiSliceProjectMaxASRMS)
	}
	maxGroups := 1
	if asrMS > aiSliceProjectMinASRMS {
		maxGroups = int(asrMS / aiSliceProjectMinASRMS)
	}
	if minGroups < 1 {
		minGroups = 1
	}
	if maxGroups < minGroups {
		maxGroups = minGroups
	}
	if target < minGroups {
		return minGroups
	}
	if target > maxGroups {
		return maxGroups
	}
	return target
}

// splitClips0IntoProjects 将已排序合并的 clips0 拆成多组剪辑项目。
// 按有效 ASR 打包：每组 ASR ≤30 分钟、拆分后每组 ASR ≥10 分钟；静音不计入 ASR，故 clips0 墙钟可超过 30 分钟。
// 项目数约等于选区墙钟 N/30（可多 1 或少 1）。保证各组时长之和等于入参总时长（不丢选区）。
func splitClips0IntoProjects(merged []model.ClipRange, utterances []asr.Utterance, paragraphs []model.ASRParagraph) [][]model.ClipRange {
	if len(merged) == 0 {
		return nil
	}
	wallMS := clipRangesDurationMS(merged)
	if wallMS <= 0 {
		return nil
	}

	asrMS := clipRangesASRDurationMS(merged, utterances)
	if targetProjectCount(wallMS, asrMS) <= 1 {
		return [][]model.ClipRange{cloneClipRanges(merged)}
	}

	atoms := buildClipAtoms(merged, utterances, paragraphs)
	if len(atoms) == 0 {
		return [][]model.ClipRange{cloneClipRanges(merged)}
	}

	wallMS = atomsDurationMS(atoms)
	asrMS = atomsASRDuration(atoms)
	count := targetProjectCount(wallMS, asrMS)
	if count <= 1 {
		return [][]model.ClipRange{cloneClipRanges(merged)}
	}

	groups := packClipAtoms(atoms, count, aiSliceProjectMaxASRMS, aiSliceProjectMinASRMS)
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

func sortedUtterances(utterances []asr.Utterance) []asr.Utterance {
	if len(utterances) == 0 {
		return nil
	}
	out := make([]asr.Utterance, len(utterances))
	copy(out, utterances)
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartTime != out[j].StartTime {
			return out[i].StartTime < out[j].StartTime
		}
		return out[i].EndTime < out[j].EndTime
	})
	return out
}

// buildClipAtoms 在每个合并选区内，仅在 ASR 分句起止（及选区端点）切开。
// 无分句时退化为按 30 分钟硬切，并把墙钟视为有效时长。
func buildClipAtoms(merged []model.ClipRange, utterances []asr.Utterance, paragraphs []model.ASRParagraph) []clipAtom {
	sorted := sortedUtterances(utterances)
	paraEnds := paragraphEndSet(paragraphs)
	boundaries := utteranceBoundarySet(sorted)
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
	fillAtomASR(atoms, sorted, !hasUtterances)
	return atoms
}

func fillAtomASR(atoms []clipAtom, utterances []asr.Utterance, fallbackWall bool) {
	if fallbackWall || len(utterances) == 0 {
		for i := range atoms {
			d := atoms[i].EndTime - atoms[i].StartTime
			if d < 0 {
				d = 0
			}
			atoms[i].asrMS = d
		}
		return
	}
	j := 0
	for i := range atoms {
		a := &atoms[i]
		for j < len(utterances) && utterances[j].EndTime <= a.StartTime {
			j++
		}
		var sum int64
		for k := j; k < len(utterances) && utterances[k].StartTime < a.EndTime; k++ {
			start := utterances[k].StartTime
			if start < a.StartTime {
				start = a.StartTime
			}
			end := utterances[k].EndTime
			if end > a.EndTime {
				end = a.EndTime
			}
			if end > start {
				sum += end - start
			}
		}
		a.asrMS = sum
	}
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
		for t := clip.StartTime + aiSliceProjectTargetWallMS; t < clip.EndTime; t += aiSliceProjectTargetWallMS {
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

func packClipAtoms(atoms []clipAtom, count int, maxASR, minASR int64) [][]clipAtom {
	if len(atoms) == 0 {
		return nil
	}
	if count <= 1 {
		return [][]clipAtom{cloneAtoms(atoms)}
	}
	var groups [][]clipAtom
	i := 0
	remaining := count
	for remaining > 1 && i < len(atoms) {
		restASR := atomsASRDuration(atoms[i:])
		minThis := minASR
		mustTake := restASR - maxASR*int64(remaining-1)
		if mustTake > minThis {
			minThis = mustTake
		}

		groupMax := maxASR
		leaveMin := minASR * int64(remaining-1)
		if restASR > leaveMin {
			capForThis := restASR - leaveMin
			if capForThis < groupMax && capForThis >= minThis {
				groupMax = capForThis
			}
		}
		if groupMax < minThis {
			groupMax = minThis
		}

		targetASR := restASR / int64(remaining)
		if targetASR > maxASR {
			targetASR = maxASR
		}
		if targetASR < minThis {
			targetASR = minThis
		}

		end := findAtomCutIndex(atoms, i, targetASR, groupMax, minThis)
		if end < i {
			end = i
		}
		end = extendTrailingSilence(atoms, end)
		if !atomsHaveASR(atoms, end+1) {
			end = len(atoms) - 1
		}

		groups = append(groups, cloneAtoms(atoms[i:end+1]))
		i = end + 1
		remaining--
		if end >= len(atoms)-1 {
			break
		}
	}
	if i < len(atoms) {
		groups = append(groups, cloneAtoms(atoms[i:]))
	}
	return rebalanceMinASR(groups, minASR)
}

func cloneAtoms(src []clipAtom) []clipAtom {
	out := make([]clipAtom, len(src))
	copy(out, src)
	return out
}

func atomsHaveASR(atoms []clipAtom, from int) bool {
	for i := from; i < len(atoms); i++ {
		if atoms[i].asrMS > 0 {
			return true
		}
	}
	return false
}

func extendTrailingSilence(atoms []clipAtom, end int) int {
	for end+1 < len(atoms) && atoms[end+1].asrMS <= 0 {
		end++
	}
	return end
}

// findAtomCutIndex 从 start 起装满 targetASR（不超过 maxASR）；优先对齐最接近目标的段落末。
func findAtomCutIndex(atoms []clipAtom, start int, targetASR, maxASR, minASR int64) int {
	if start >= len(atoms) {
		return start
	}
	if maxASR <= 0 {
		maxASR = aiSliceProjectMaxASRMS
	}
	if targetASR <= 0 {
		targetASR = maxASR
	}
	if minASR < 0 {
		minASR = 0
	}

	lastCut := -1
	bestPara := -1
	bestParaDist := int64(-1)
	reached := -1
	var acc int64
	for i := start; i < len(atoms); i++ {
		d := atoms[i].asrMS
		if d < 0 {
			d = 0
		}
		next := acc + d
		if next > maxASR && lastCut >= start {
			break
		}
		acc = next
		lastCut = i
		if atoms[i].atParagraphEnd && acc >= minASR {
			dist := absInt64(acc - targetASR)
			if bestPara < 0 || dist <= bestParaDist {
				bestPara = i
				bestParaDist = dist
			}
		}
		if reached < 0 && acc >= targetASR && acc >= minASR {
			reached = i
		}
		if next > maxASR {
			break
		}
	}
	if lastCut < start {
		return start
	}
	if bestPara >= start {
		return bestPara
	}
	if reached >= start {
		return reached
	}
	return lastCut
}

// rebalanceMinASR 从尾部向前：末组有效 ASR <min 时整颗原子挪到末组；无法保持前组 ≥min 则与前组合并。
func rebalanceMinASR(groups [][]clipAtom, minASR int64) [][]clipAtom {
	if len(groups) <= 1 {
		return groups
	}
	for len(groups) >= 2 {
		last := groups[len(groups)-1]
		if atomsASRDuration(last) >= minASR {
			break
		}
		prev := groups[len(groups)-2]
		if len(prev) == 0 {
			groups = append(groups[:len(groups)-2], last)
			continue
		}
		stolen := prev[len(prev)-1]
		prevRest := prev[:len(prev)-1]
		if atomsASRDuration(prevRest) < minASR {
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
