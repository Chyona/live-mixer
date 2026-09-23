package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
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
	// defaultCaptionSegmentTimeout 单条文案的调用总预算（含重试）。
	defaultCaptionSegmentTimeout = 15 * time.Second
	// defaultCaptionSegmentConcurrency 同时打向 LLM 的断句请求上限。
	defaultCaptionSegmentConcurrency = 4
	defaultCaptionSegmentCacheN      = 512
	// captionSegmentPromptVersion 提示词版本，改动下方提示词时必须同步 +1（缓存 key 的一员）。
	captionSegmentPromptVersion = "v1"
)

// captionSegmentSystemPromptTemplate 断句系统提示词（代码常量，与 asr.MaxCaptionRunes 强耦合，改动需发版）。
// %d 为行宽上限。
const captionSegmentSystemPromptTemplate = `你是中文视频字幕的断句专家。把用户给出的文本切成若干行字幕。
硬性要求：
1. 只允许在原文本中插入切分点；禁止改写、增删、合并、重排任何字符，标点也必须原样保留、留在它原本所属的那一行。
2. 每行不超过 %d 个字（中文按字计）。
3. 不得把词语、成语、机构名、人名、专业术语、数字与单位、英文单词从中间切开。
   例如：人工智能、美联储主席、科创板、3.14%%、iPhone 15 必须整块留在同一行。
4. 优先在语义完整处断开：主谓之间、短语之间、逗号处；不要在“的/了/是/和/就/而”这类虚词前后硬切，也不要让某一行只剩下半句话。
5. 各行长度尽量接近，行数尽量少，不要出现少于 3 个字的孤行。
6. 不得输出空行。
只输出 JSON：{"lines": ["第一行", "第二行"]}

示例一：
文本：今天我们来聊一聊人工智能在医疗领域的应用
输出：{"lines": ["今天我们来聊一聊", "人工智能在医疗领域的应用"]}

示例二：
文本：这款产品原价是￥199，现在直播间下单只要98折，相当于省了小两百块钱。
输出：{"lines": ["这款产品原价是￥199", "现在直播间下单只要98折", "相当于省了小两百块钱"]}`

// CaptionSegmentPrompt 渲染断句系统提示词。
func CaptionSegmentPrompt(maxRunes int) string {
	return fmt.Sprintf(captionSegmentSystemPromptTemplate, maxRunes)
}

// CaptionSegmenter 用 LLM 对切片文案做语义断行，输出仍受 asr 的硬约束与内容一致性校验约束；
// 任一条失败（超时/解析失败/校验不过）只影响该条，调用方按 nil 回退 asr.SplitLinesByRule。
type CaptionSegmenter struct {
	// Client LLM 出口；nil 表示不启用（全部回退规则折行）。
	Client LLMChatClient
	// Logger 可空。
	Logger *zap.Logger
	// Model 用于缓存 key 与日志（模型换代后旧缓存不再命中）；可空。
	Model string
	// Timeout 单条文案的调用总预算（含重试）；<=0 时回落 15s。
	Timeout time.Duration
	// Concurrency 并发调用上限；<=0 时回落 4。
	Concurrency int
	// CacheSize 结果缓存条数上限；<=0 时回落默认值。缓存让同一文案在重试/重跑时复用同一断句结果。
	CacheSize int

	once  sync.Once
	cache *captionLinesCache
}

// SegmentTexts 为每个切片文案返回断行建议，返回切片与输入等长：
// 元素为 nil 表示该条不调用模型或调用/校验失败，调用方应回退 asr.SplitLinesByRule。
// 本方法不返回 error：断句是增强能力，任何单条失败都不应影响成片。
func (s *CaptionSegmenter) SegmentTexts(ctx context.Context, texts []string) [][]string {
	if s == nil || s.Client == nil || len(texts) == 0 {
		return nil
	}
	s.once.Do(func() {
		s.cache = newCaptionLinesCache(s.cacheSize())
	})

	out := make([][]string, len(texts))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.concurrency())
	for i, text := range texts {
		i, text := i, text
		trimmed := strings.TrimSpace(text)
		// 一行放得下就不必问模型：规则折行本来就只有一行。
		if trimmed == "" || utf8.RuneCountInString(trimmed) <= asr.MaxCaptionRunes {
			continue
		}
		if cached, ok := s.cache.get(s.cacheKey(trimmed)); ok {
			out[i] = cached
			continue
		}
		g.Go(func() error {
			if lines, ok := s.segmentOne(gctx, trimmed); ok {
				out[i] = lines
				s.cache.put(s.cacheKey(trimmed), lines)
			}
			return nil
		})
	}
	// 所有 goroutine 都返回 nil，g.Wait 只用于等待与并发上限控制。
	_ = g.Wait()
	return out
}

// segmentOne 断一条文案：调用 LLM → 解析 → 内容一致校验与硬约束修正。任一步失败返回 ok=false。
func (s *CaptionSegmenter) segmentOne(ctx context.Context, text string) ([]string, bool) {
	cctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	messages := []llm.ChatMessage{
		{Role: "system", Content: CaptionSegmentPrompt(asr.MaxCaptionRunes)},
		{Role: "user", Content: "文本：" + text},
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

		lines, err := llm.ParseCaptionLines(content)
		if err != nil {
			lastErr = err
			break
		}
		valid, err := asr.ValidateCaptionLines(text, lines, asr.MaxCaptionRunes)
		if err != nil {
			logger.Warn("LLM 断句未通过校验，回退规则折行",
				zap.Error(err),
				zap.Int("line_count", len(lines)),
				zap.Int("text_runes", utf8.RuneCountInString(text)),
			)
			return nil, false
		}
		return valid, true
	}

	logger.Warn("LLM 断句失败，回退规则折行",
		zap.Error(lastErr),
		zap.Int("text_runes", utf8.RuneCountInString(text)),
	)
	return nil, false
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

// cacheKey 缓存标识：文案 + 行宽 + 提示词版本 + 模型，任一变化都视为不同断句请求。
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
// 目的是让同一文案在任务重试、重新提交成片时复用同一断句，而不是为了省一次调用。
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
