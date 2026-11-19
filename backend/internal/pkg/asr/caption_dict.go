package asr

import (
	_ "embed"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// captionDictData 折行分词词表，每行「词 频次」，纯汉字 1–6 字。
//
// 来源：jieba 的 dict.txt（MIT License，Copyright (c) 2013 Sun Junyi），
// 生成规则：只保留纯汉字、长度 1–6、频次 ≥ 10 的词条，按词排序，共 10.4 万条。
// 频次是语料计数，做最大概率分词时取对数当分数用；生成脚本见 internal/pkg/asr/README.md。
//
//go:embed caption_dict.txt
var captionDictData string

const (
	// captionDictMaxWordRunes 词表内最长词的字数，限制 Viterbi 的候选枚举范围。
	captionDictMaxWordRunes = 6
	// captionDictFloorFreq 词表外单字的兜底频次：低于词表里任何词条，
	// 保证未收录的字只有在没有别的选择时才自成一词（最大概率分词会尽量把它并进相邻词）。
	captionDictFloorFreq = 1
	// captionLexiconSynthFreq 人工小词表里、通用词表又没收的词（如「科创板」）补的合成频次：
	// 取中位量级，既能让它成词，又不会压过通用词表里的高频词。
	captionLexiconSynthFreq = 1000
	// captionExtraBonusPerRune 调用方强制的词（extra）每字加分。
	// 远大于任何词的对数概率（最高频词也只有 -4 量级），因此 extra 词必定整体成词。
	captionExtraBonusPerRune = 100.0
)

// captionWordDict 折行分词用的词表：词 → 对数概率 + 词表外单字兜底分。
type captionWordDict struct {
	logProb  map[string]float64
	floor    float64
	maxRunes int
}

var (
	captionWordDictOnce sync.Once
	captionWordDictVal  *captionWordDict
)

// captionDict 返回分词词表，首次调用时解析（进程内只解析一次）。
func captionDict() *captionWordDict {
	captionWordDictOnce.Do(func() {
		captionWordDictVal = loadCaptionWordDict()
	})
	return captionWordDictVal
}

func loadCaptionWordDict() *captionWordDict {
	// 人工小词表并入：它是领域内确认「不可拆」的词，且未必在通用词表里。
	ensureCaptionLexicon()

	freqs := make(map[string]int32, 120000)
	total := int64(0)
	maxRunes := captionDictMaxWordRunes
	for line := range strings.SplitSeq(captionDictData, "\n") {
		sp := strings.LastIndexByte(line, ' ')
		if sp <= 0 || sp == len(line)-1 {
			continue
		}
		f, err := strconv.Atoi(strings.TrimRight(line[sp+1:], "\r"))
		if err != nil || f <= 0 {
			continue
		}
		w := line[:sp]
		if _, dup := freqs[w]; dup {
			continue
		}
		freqs[w] = int32(f)
		total += int64(f)
		if n := utf8.RuneCountInString(w); n > maxRunes {
			maxRunes = n
		}
	}
	for w := range captionLexiconSet {
		if _, ok := freqs[w]; ok {
			continue
		}
		freqs[w] = captionLexiconSynthFreq
		total += captionLexiconSynthFreq
		if n := utf8.RuneCountInString(w); n > maxRunes {
			maxRunes = n
		}
	}

	d := &captionWordDict{
		logProb:  make(map[string]float64, len(freqs)),
		maxRunes: maxRunes,
	}
	if total <= 0 {
		// 词表为空（理论上不会发生）：退化成「单字成词」。
		d.logProb = make(map[string]float64)
		d.floor = math.Log(float64(captionDictFloorFreq))
		return d
	}
	totalF := float64(total)
	for w, f := range freqs {
		d.logProb[w] = math.Log(float64(f) / totalF)
	}
	d.floor = math.Log(float64(captionDictFloorFreq) / totalF)
	return d
}

// score 返回单个候选词的对数概率；未收录的词按词表外单字兜底分计。
func (d *captionWordDict) score(w string) float64 {
	if s, ok := d.logProb[w]; ok {
		return s
	}
	return d.floor
}

// captionWordEnds 用最大概率分词（Viterbi）算出每个位置所属词的结束下标：
// ends[i] 是包含 runes[i] 的那个词的后一个位置（非汉字字符各自成「词」）。
//
// extra 是调用方当次强制的词（ASR 补词、调用方向下传的词），命中时给压倒性加分，
// 保证它们整段成词、与常规词表冲突时也优先成词。
func captionWordEnds(runes []rune, extra map[string]struct{}) []int {
	n := len(runes)
	ends := make([]int, n)
	if n == 0 {
		return ends
	}
	d := captionDict()
	maxLen := d.maxRunes
	for w := range extra {
		if k := utf8.RuneCountInString(w); k > maxLen {
			maxLen = k
		}
	}
	if maxLen > n {
		maxLen = n
	}

	// best[i]：前 i 个字的最大对数概率；prev[i]：达到该值的上一个词边界。
	best := make([]float64, n+1)
	prev := make([]int, n+1)
	best[0] = 0
	for i := 1; i <= n; i++ {
		// 单字成词：词表内的单字用它自己的频次，词表外的字用兜底分。
		single := string(runes[i-1 : i])
		best[i] = best[i-1] + d.score(single)
		prev[i] = i - 1
		for l := 2; l <= maxLen && l <= i; l++ {
			if !unicode.Is(unicode.Han, runes[i-l]) {
				// 词条首字必是汉字（词表与 extra 都只收纯汉字词），省掉无谓查表。
				continue
			}
			cand := string(runes[i-l : i])
			s, ok := d.logProb[cand]
			if _, forced := extra[cand]; forced {
				s, ok = captionExtraBonusPerRune*float64(l)+1, true
			}
			if !ok {
				continue
			}
			if v := best[i-l] + s; v > best[i] {
				best[i], prev[i] = v, i-l
			}
		}
	}

	for i := n; i > 0; {
		p := prev[i]
		for j := p; j < i; j++ {
			ends[j] = i
		}
		i = p
	}
	return ends
}
