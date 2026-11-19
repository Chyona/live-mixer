package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// 断句调用参数：一次请求即一次判断，失败回退规则折行的代价很低，因此重试只覆盖
// 「传输层瞬时抖动」这一种情况，且受单条总预算约束。
const (
	captionSegmentMaxAttempts = 2
	captionSegmentBackoffBase = 200 * time.Millisecond
	// defaultCaptionSegmentTimeout 单条小句的调用总预算（含重试）。
	defaultCaptionSegmentTimeout = 15 * time.Second
	// defaultCaptionSegmentConcurrency 同时打向 LLM 的断句请求上限。
	defaultCaptionSegmentConcurrency = 4
	defaultCaptionSegmentCacheN      = 512
	// captionSegmentPromptVersion 提示词版本，改动下方提示词时必须同步 +1（缓存 key 的一员）。
	// v1 → v2：输出契约从「整行文本」换成「切点位置」。
	captionSegmentPromptVersion = "v2"
)

// captionSegmentSystemPromptTemplate 超长小句的断句提示词（代码常量，与 asr.MaxCaptionRunes 强耦合，改动需发版）。
//
// 提示词的落点只有一件事：报切点位置。文本交给 asr.SplitLinesByCuts 按位置切，
// 模型没有任何复述/改写正文的机会——「模型产行」被废弃的原因正在于此（模型改写或丢字会让整条切片的
// 时间分配降级为均分，音字对不齐）。位置口径必须与 asr.SplitLinesByCuts 保持一致。
// %d 为行宽上限。
const captionSegmentSystemPromptTemplate = `你是中文视频字幕的断句专家。用户给出的一句话已经按标点切分过、且超过了单行字数上限，请在句中补上切分点。
硬性要求：
1. 只输出切分点的位置，不要复述或改写文本，也不要输出任何解释。
2. 位置是「切分点左边最后一个字的序号」，从 0 开始计。
   例如文本「最直接的解决方法是升级到修复该问题的版本」输出 8：第 8 个字（0 起）是「是」，在「是」后面切开。
3. 每行不超过 %d 个字，各行长度尽量接近，行数尽量少。
4. 不得把词语、成语、机构名、人名、专业术语、数字与单位、英文单词从中间切开。
   例如：人工智能、美联储主席、科创板、3.14%%、iPhone 15 必须整块留在同一行。
5. 优先在语义完整处断开：主谓之间、短语之间；不要在“的/了/是/和/就/而”这类虚词前后硬切。
6. 只输出 JSON，例如：{"cuts": [位置1, 位置2]}

示例一：
文本：最直接的解决方法是升级到修复该问题的版本
输出：{"cuts": [8]}

示例二：
文本：这个颜色特别好看而且它的面料非常柔软穿起来很舒服
输出：{"cuts": [7, 17]}`

// CaptionSegmentPrompt 渲染断句系统提示词。
func CaptionSegmentPrompt(maxRunes int) string {
	return fmt.Sprintf(captionSegmentSystemPromptTemplate, maxRunes)
}

// CaptionSegmenter 把切片文案折成字幕行：先按标点断句（产品规则，代码负责），
// 只有「标点断句后仍超过行宽上限」的小句才问 LLM 要切点位置（语义判断，模型负责）。
//
// 这样切分的理由：标点与 12 字上限是确定的产品不变量，写在代码里比写在提示词里便宜且不会失手；
// 模型只回答「长句该从第几个字断开」这一问题，而它报的位置要过同一套硬校验——
// 越界即拒、去重排序、吸附到词边界（西文词/数字词/词表词不拆）、行宽不得超上限。
// 正文始终由代码切出来，模型输出里没有任何文本，因此「模型改写了字幕」这一失败模式被彻底移除。
//
// 失败面：某小句超时/解析失败/位置不可用，只回退这一句的规则折行，同一文案的其他小句照用模型结果；
// 一条文案里没有任何小句用上模型结果时该条返回 nil，调用方按规则折行（与未启用本层完全一致）。
type CaptionSegmenter struct {
	// Client LLM 出口；nil 表示不启用（全部回退规则折行）。
	Client LLMChatClient
	// Logger 可空。
	Logger *zap.Logger
	// Model 用于缓存 key 与日志（模型换代后旧缓存不再命中）；可空。
	Model string
	// Timeout 单条小句的调用总预算（含重试）；<=0 时回落 15s。
	Timeout time.Duration
	// Concurrency 并发调用上限；<=0 时回落 4。
	Concurrency int
	// CacheSize 结果缓存条数上限；<=0 时回落默认值。缓存让同一小句在重试/重跑时复用同一断句结果。
	CacheSize int

	once  sync.Once
	cache *captionLinesCache
}

// captionPlanItem 单条文案断行计划里的一项：按子句顺序排列。
// lines 非空表示这一句已经定下来（≤ 行宽上限，或缓存命中的模型结果）；job >= 0 表示等 jobs[job] 的切点结果。
type captionPlanItem struct {
	lines   []string
	fromLLM bool // lines 是否来自模型（缓存命中算来自模型）：决定该条文案要不要覆盖规则折行
	clause  string
	job     int
}

// clauseResult 单个超长小句的模型结果；ok=false 表示该句不可用（只回退这一句）。
type clauseResult struct {
	lines []string
	ok    bool
}

// SegmentTexts 为每个切片文案返回字幕行，返回切片与输入等长：
// 元素为 nil 表示该条没有用上模型结果（没有超长小句、小句全部失败或调用方未启用），
// 调用方应回退 asr.SplitLinesByRule。
// 本方法不返回 error：断句是增强能力，任何单条失败都不应影响成片。
func (s *CaptionSegmenter) SegmentTexts(ctx context.Context, texts []string) [][]string {
	if s == nil || s.Client == nil || len(texts) == 0 {
		return nil
	}
	s.once.Do(func() {
		s.cache = newCaptionLinesCache(s.cacheSize())
	})

	max := asr.MaxCaptionRunes
	out := make([][]string, len(texts))
	plans := make([][]captionPlanItem, len(texts))
	// 全局去重：同一小句只调一次模型（长句在多个切片里重复出现的场景很常见）。
	jobs := make([]string, 0, len(texts))
	jobByClause := make(map[string]int, len(texts))
	for i, text := range texts {
		for _, clause := range asr.SplitCaptionClauses(text) {
			// 一行放得下就不必问模型：标点断句给出的子句本身就是最终行。
			if clause.Runes <= max {
				plans[i] = append(plans[i], captionPlanItem{lines: []string{clause.Text}, job: -1})
				continue
			}
			if cached, ok := s.cache.get(s.cacheKey(clause.Text)); ok {
				plans[i] = append(plans[i], captionPlanItem{lines: cached, fromLLM: true, job: -1})
				continue
			}
			job, ok := jobByClause[clause.Text]
			if !ok {
				job = len(jobs)
				jobByClause[clause.Text] = job
				jobs = append(jobs, clause.Text)
			}
			plans[i] = append(plans[i], captionPlanItem{clause: clause.Text, job: job})
		}
	}
	// jobs 为空（没有超长小句，或全部命中缓存）时下面的循环不产生任何调用：
	// 缓存命中的小句照常出现在结果里；全是短子句的文案一行也不产生，该条为 nil，
	// 调用方按规则折行（规则折行能带上 ASR 词表，比这里的纯规则更准）。
	results := make([]clauseResult, len(jobs))
	// 吸附统计：本次断句里有多少切点被挪到词边界上。缓存命中不计数（缓存里存的已是吸附后的行）。
	var snap struct {
		clauses   atomic.Int64
		cuts      atomic.Int64
		moved     atomic.Int64
		displaced atomic.Int64
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.concurrency())
	for j, clause := range jobs {
		j, clause := j, clause
		g.Go(func() error {
			if lines, stats, ok := s.segmentClause(gctx, clause); ok {
				results[j] = clauseResult{lines: lines, ok: true}
				s.cache.put(s.cacheKey(clause), lines)
				snap.clauses.Add(1)
				snap.cuts.Add(int64(stats.CutCount))
				snap.moved.Add(int64(stats.MovedCuts))
				snap.displaced.Add(int64(stats.Displaced))
			}
			return nil
		})
	}
	// 所有 goroutine 都返回 nil，g.Wait 只用于等待与并发上限控制。
	_ = g.Wait()
	if n := snap.clauses.Load(); n > 0 {
		s.logger().Info("LLM 断句吸附统计",
			zap.Int64("clauses", n),
			zap.Int64("cut_count", snap.cuts.Load()),
			zap.Int64("moved_cuts", snap.moved.Load()),
			zap.Int64("displaced_runes", snap.displaced.Load()),
		)
	}

	for i := range texts {
		var lines []string
		usedLLM := false
		for _, item := range plans[i] {
			if item.job < 0 {
				lines = append(lines, item.lines...)
				usedLLM = usedLLM || item.fromLLM
				continue
			}
			if r := results[item.job]; r.ok {
				lines = append(lines, r.lines...)
				usedLLM = true
				continue
			}
			// 该小句不可用：只回退这一句，同一文案的其他小句仍用模型结果。
			lines = append(lines, asr.SplitLinesByRuleMax(item.clause, max)...)
		}
		if usedLLM {
			out[i] = lines
		}
	}
	return out
}

// segmentClause 断一个超长小句：要模型报切点位置 → 解析 → 按位置切行（越界/吸附不出即失败）。
// 任一步失败返回 ok=false，调用方只回退这一句。
func (s *CaptionSegmenter) segmentClause(ctx context.Context, clause string) ([]string, asr.CaptionSnapStats, bool) {
	cctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	messages := []llm.ChatMessage{
		{Role: "system", Content: CaptionSegmentPrompt(asr.MaxCaptionRunes)},
		{Role: "user", Content: "文本：" + clause},
	}
	logger := s.logger()

	var lastErr error
	for attempt := 1; attempt <= captionSegmentMaxAttempts; attempt++ {
		content, err := s.Client.ChatStructured(cctx, messages)
		if err != nil {
			lastErr = err
			// 仅在预算内且属于瞬时传输错误时重试；超时（等不到结果）直接回退，不拖长成片时间。
			if !isRetryableSegmentErr(err) || cctx.Err() != nil || attempt == captionSegmentMaxAttempts {
				break
			}
			select {
			case <-cctx.Done():
			case <-time.After(captionSegmentBackoffBase * time.Duration(attempt)):
			}
			continue
		}

		cuts, err := llm.ParseCaptionCuts(content)
		if err != nil {
			lastErr = err
			break
		}
		lines, stats, err := asr.SplitLinesByCuts(clause, cuts, asr.MaxCaptionRunes)
		if err != nil {
			logger.Warn("LLM 断句位置不可用，该句回退规则折行",
				zap.Error(err),
				zap.Int("clause_runes", utf8.RuneCountInString(clause)),
				zap.Int("cut_count", len(cuts)),
			)
			return nil, stats, false
		}
		return lines, stats, true
	}

	logger.Warn("LLM 断句失败，该句回退规则折行",
		zap.Error(lastErr),
		zap.Int("clause_runes", utf8.RuneCountInString(clause)),
	)
	return nil, asr.CaptionSnapStats{}, false
}

// isRetryableSegmentErr 只把超时与网络类错误视为可重试；
// 上游错误文案可变，因此不按错误字符串判定。
func isRetryableSegmentErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// cacheKey 缓存标识：小句文本 + 行宽 + 提示词版本 + 模型，任一变化都视为不同断句请求。
// 缓存的是「小句 → 行」，因此同一小句出现在不同切片里也只调一次模型。
// 行宽是代码常量（asr.MaxCaptionRunes），写进 key 是为了语义完整：将来行宽若变，旧缓存自然失效。
func (s *CaptionSegmenter) cacheKey(text string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s", text, asr.MaxCaptionRunes, captionSegmentPromptVersion, s.Model)))
	return hex.EncodeToString(sum[:])
}

func (s *CaptionSegmenter) timeout() time.Duration {
	if s.Timeout <= 0 {
		return defaultCaptionSegmentTimeout
	}
	return s.Timeout
}

func (s *CaptionSegmenter) concurrency() int {
	if s.Concurrency <= 0 {
		return defaultCaptionSegmentConcurrency
	}
	return s.Concurrency
}

func (s *CaptionSegmenter) cacheSize() int {
	if s.CacheSize <= 0 {
		return defaultCaptionSegmentCacheN
	}
	return s.CacheSize
}

func (s *CaptionSegmenter) logger() *zap.Logger {
	if s.Logger == nil {
		return zap.NewNop()
	}
	return s.Logger
}

// captionLinesCache 进程内断句结果缓存（FIFO 淘汰，容量固定）。
// 目的是让同一小句在任务重试、重新提交成片时复用同一断句，而不是为了省一次调用。
type captionLinesCache struct {
	mu    sync.Mutex
	max   int
	order []string
	items map[string][]string
}

func newCaptionLinesCache(max int) *captionLinesCache {
	if max <= 0 {
		max = defaultCaptionSegmentCacheN
	}
	return &captionLinesCache{max: max, items: make(map[string][]string, max)}
}

func (c *captionLinesCache) get(key string) ([]string, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	lines, ok := c.items[key]
	return lines, ok
}

func (c *captionLinesCache) put(key string, lines []string) {
	if c == nil || len(lines) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	for len(c.order) >= c.max {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
	c.items[key] = lines
	c.order = append(c.order, key)
}
