package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func asrPostLogger(logger *zap.Logger) *zap.Logger {
	if logger == nil {
		return zap.NewNop()
	}
	return logger
}

const (
	// asrLLMWindowMaxRunes 单窗 user 侧句段列表最大 rune 数（Map 预算）；超时长辅约束后再按此收缩。
	asrLLMWindowMaxRunes       = 16000
	asrParagraphMaxRunes       = 200                   // 段落正文上限（含）；超限按句号兜底拆分
	asrSummaryMinDurationMs    = int64(5 * 60 * 1000)  // 5 分钟
	asrSummaryMaxDurationMs    = int64(60 * 60 * 1000) // 60 分钟
	asrSummaryMergeGapMs       = int64(2 * 60 * 1000)  // 同主题合并允许的最大时间间隙
	asrSummaryTitleMaxRunes    = 6
	asrParagraphWindowMs       = int64(25 * 60 * 1000) // 段落窗口约 25 分钟
	asrSummaryWindowMs         = int64(60 * 60 * 1000) // 总结窗口约 60 分钟
	asrLLMTransportMaxAttempts = 3                     // 仅网络/接口异常重试（含首次）
	asrLLMTransportBackoffBase = 100 * time.Millisecond
	asrLLMWindowParallelism    = 8                     // Map 阶段多窗并行上限
)

const asrSummariesSystemPrompt = `你是直播内容主题提炼助手。根据带编号的 ASR 句段列表，提炼若干「核心主题」分段。
要求：
1. 只输出一个 JSON 对象，格式严格为 {"items":[...]}，不要输出其它文字或 markdown。
2. items 每项格式：{"title":"...","start_index":0,"end_index":3}，为句段编号闭区间（含两端）。不要输出 summary 或其它字段。
3. title 必须严格不超过 6 个汉字（按 Unicode 字符计，含标点）；禁止写成短句。优先用 2~4 字主题词，例如「开场互动」「产品讲解」「福利促销」。坏例：「今天给大家介绍优惠活动」。
4. 每段对应时长（由 start_index~end_index 句段时间推算）应在 5~60 分钟；服务端会丢弃不合此时长的段。
5. 各段是核心主题，不必覆盖全文；段与段可以连续、间断或索引相交。
6. start_index/end_index 必须落在输入句段编号范围内，且 start_index<=end_index。
7. 可以输出 0 段（items 为空数组），表示本窗无合适主题。`

const asrParagraphsSystemPrompt = `你是直播 ASR 段落划分助手。根据带编号的 ASR 句段列表，将全文划分为连续段落。
要求：
1. 只输出一个 JSON 对象，格式严格为 {"items":[...]}，不要输出其它文字或 markdown。
2. items 每项格式：{"start_index":0,"end_index":3}，为句段编号闭区间（含两端）。
3. 所有编号必须恰好覆盖输入中的全部句段一次：无遗漏、无重叠。
4. 每个区间内只能有一个说话人（speaker 相同）。
5. 每个区间拼接后的正文字数必须小于 200 字。
6. 按时间顺序输出区间。`

// asrParagraphRange LLM 返回的段落边界（utterance 闭区间下标）。
type asrParagraphRange struct {
	StartIndex int `json:"start_index"`
	EndIndex   int `json:"end_index"`
}

// asrSummaryLLMItem LLM 返回的总结项（句段 index 锚点）。
type asrSummaryLLMItem struct {
	Title      string `json:"title"`
	StartIndex int    `json:"start_index"`
	EndIndex   int    `json:"end_index"`
}

// asrPostprocessResult ASR 后处理产出。
type asrPostprocessResult struct {
	Summaries  []model.ASRSummarySegment
	Paragraphs []model.ASRParagraph
}

type asrLLMWindowDebug struct {
	Offset       int                       `json:"offset"`
	UserPrompt   string                    `json:"user_prompt"`
	RawResponse  string                    `json:"raw_response"`
	RepairPrompt string                    `json:"repair_prompt,omitempty"`
	RepairRaw    string                    `json:"repair_raw_response,omitempty"`
	Segments     []model.ASRSummarySegment `json:"segments,omitempty"`
	Ranges       []asrParagraphRange       `json:"ranges,omitempty"`
}

// RunASRPostprocess 根据完整 live_asr JSON 生成 asr_summaries 与 asr_paragraphs（供 CLI / 集成调用）。
// logger 可为 nil（此时不输出过程日志）。
func RunASRPostprocess(ctx context.Context, llmClient LLMChatClient, liveASR string, durationMs int64, logger *zap.Logger) ([]model.ASRSummarySegment, []model.ASRParagraph, error) {
	out, err := runASRPostprocess(ctx, llmClient, liveASR, durationMs, nil, logger)
	if err != nil {
		return nil, nil, err
	}
	return out.Summaries, out.Paragraphs, nil
}

// RunASRParagraphs 仅根据 live_asr 生成 asr_paragraphs（跳过 asr_summaries 与上游 ASR 识别）。
// logger 可为 nil。
func RunASRParagraphs(ctx context.Context, llmClient LLMChatClient, liveASR string, durationMs int64, logger *zap.Logger) ([]model.ASRParagraph, error) {
	logger = asrPostLogger(logger)
	if llmClient == nil {
		return nil, fmt.Errorf("LLM 客户端未配置")
	}
	utterances := asr.FormatUtterancesForAPI(liveASR)
	if len(utterances) == 0 {
		return nil, fmt.Errorf("ASR 分句为空，无法生成段落")
	}
	if durationMs <= 0 {
		durationMs = utterances[len(utterances)-1].EndTime
	}
	started := time.Now()
	logger.Info("开始 ASR paragraphs 生成",
		zap.Int("utterance_count", len(utterances)),
		zap.Int64("duration_ms", durationMs),
		zap.Int("paragraph_windows", len(splitUtterancesByDuration(utterances, asrParagraphWindowMs, asrLLMWindowMaxRunes))),
	)
	paragraphs, err := generateASRParagraphs(ctx, llmClient, utterances, durationMs, nil, logger)
	if err != nil {
		return nil, fmt.Errorf("生成 asr_paragraphs 失败: %w", err)
	}
	logger.Info("ASR paragraphs 生成完成",
		zap.Int("paragraph_count", len(paragraphs)),
		zap.Duration("elapsed", time.Since(started)),
	)
	return paragraphs, nil
}

// runASRPostprocess 调用 LLM 生成 summaries 与 paragraphs；任一步失败返回 error。
// summaries 与 paragraphs 两路并行；rec 非空时将 LLM 中间过程写入 003/004 调试文件。
func runASRPostprocess(ctx context.Context, llmClient LLMChatClient, liveASR string, durationMs int64, rec *asrDebugRecorder, logger *zap.Logger) (asrPostprocessResult, error) {
	var out asrPostprocessResult
	logger = asrPostLogger(logger)
	if llmClient == nil {
		return out, fmt.Errorf("LLM 客户端未配置")
	}
	utterances := asr.FormatUtterancesForAPI(liveASR)
	if len(utterances) == 0 {
		return out, fmt.Errorf("ASR 分句为空，无法生成总结与段落")
	}
	if durationMs <= 0 {
		durationMs = utterances[len(utterances)-1].EndTime
	}

	summaryWindows := splitUtterancesByDuration(utterances, asrSummaryWindowMs, asrLLMWindowMaxRunes)
	paragraphWindows := splitUtterancesByDuration(utterances, asrParagraphWindowMs, asrLLMWindowMaxRunes)
	started := time.Now()
	logger.Info("开始 ASR LLM 后处理生成",
		zap.Int("utterance_count", len(utterances)),
		zap.Int64("duration_ms", durationMs),
		zap.Int("summary_windows", len(summaryWindows)),
		zap.Int("paragraph_windows", len(paragraphWindows)),
	)

	var (
		summaries  []model.ASRSummarySegment
		paragraphs []model.ASRParagraph
		sumErr     error
		paraErr    error
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		summaries, err = generateASRSummaries(gctx, llmClient, utterances, durationMs, rec, logger)
		if err != nil {
			sumErr = fmt.Errorf("生成 asr_summaries 失败: %w", err)
			return sumErr
		}
		return nil
	})
	g.Go(func() error {
		var err error
		paragraphs, err = generateASRParagraphs(gctx, llmClient, utterances, durationMs, rec, logger)
		if err != nil {
			paraErr = fmt.Errorf("生成 asr_paragraphs 失败: %w", err)
			return paraErr
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		switch {
		case sumErr != nil && paraErr != nil:
			return out, errors.Join(sumErr, paraErr)
		case sumErr != nil:
			return out, sumErr
		case paraErr != nil:
			return out, paraErr
		default:
			return out, err
		}
	}
	out.Summaries = summaries
	out.Paragraphs = paragraphs
	logger.Info("ASR LLM 后处理生成完成",
		zap.Int("summary_count", len(summaries)),
		zap.Int("paragraph_count", len(paragraphs)),
		zap.Duration("elapsed", time.Since(started)),
	)
	return out, nil
}

// asrChatStructured 调用结构化 LLM；仅对网络/接口异常重试，最多 asrLLMTransportMaxAttempts 次。
// 模型一旦返回内容即交给调用方，不做内容向重试。
func asrChatStructured(ctx context.Context, llmClient LLMChatClient, messages []llm.ChatMessage, logger *zap.Logger) (string, error) {
	logger = asrPostLogger(logger)
	var lastErr error
	for attempt := 1; attempt <= asrLLMTransportMaxAttempts; attempt++ {
		content, err := llmClient.ChatStructured(ctx, messages)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if !isASRLLMTransportError(err) || attempt == asrLLMTransportMaxAttempts {
			return "", err
		}
		logger.Warn("ASR LLM 调用传输异常，准备重试",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", asrLLMTransportMaxAttempts),
			zap.Error(err),
		)
		delay := asrLLMTransportBackoffBase * time.Duration(attempt)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(delay):
		}
	}
	return "", lastErr
}

func isASRLLMTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := err.Error()
	for _, p := range []string{
		"请求 LLM 失败",
		"读取 LLM 响应失败",
		"创建请求失败",
		"LLM HTTP",
		"LLM 返回错误",
		"LLM 响应无 choices",
		"LLM 响应内容为空",
		"解析 LLM 响应失败",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func generateASRSummaries(ctx context.Context, llmClient LLMChatClient, utterances []asr.Utterance, durationMs int64, rec *asrDebugRecorder, logger *zap.Logger) ([]model.ASRSummarySegment, error) {
	logger = asrPostLogger(logger).With(zap.String("phase", "summaries"))
	// Map：按时长 + rune 预算切窗；各窗并行调 LLM。
	windows := splitUtterancesByDuration(utterances, asrSummaryWindowMs, asrLLMWindowMaxRunes)
	type winOut struct {
		segs []model.ASRSummarySegment
		dbg  asrLLMWindowDebug
	}
	outs := make([]winOut, len(windows))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(asrLLMWindowParallelism)
	for i, win := range windows {
		i, win := i, win
		g.Go(func() error {
			segs, dbg, err := generateASRSummariesWindow(gctx, llmClient, win, durationMs, logger)
			outs[i] = winOut{segs: segs, dbg: dbg}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var all []model.ASRSummarySegment
	debugWindows := make([]asrLLMWindowDebug, 0, len(windows))
	for _, o := range outs {
		debugWindows = append(debugWindows, o.dbg)
		all = append(all, o.segs...)
	}
	if rec != nil && len(debugWindows) > 0 {
		rec.Write("003_llm_summaries.json", map[string]any{
			"recorded_at": asrDebugRecordedAt(),
			"windows":     debugWindows,
		})
	}

	beforeReduce := len(all)
	logger.Info("ASR summaries Map 完成",
		zap.Int("windows", len(windows)),
		zap.Int("segment_count_before_reduce", beforeReduce),
	)

	// Reduce：规范化 → 跨窗同主题合并 → 去包含去重 → 按时长过滤 → 校验 / 排序。
	normalizeASRSummaries(all, durationMs)
	all = mergeASRSummaries(all)
	afterMerge := len(all)
	all = dedupeContainedASRSummaries(all)
	afterDedupe := len(all)
	all = filterASRSummariesByDuration(all, logger)
	afterFilter := len(all)
	logger.Info("ASR summaries Reduce 完成",
		zap.Int("before_reduce", beforeReduce),
		zap.Int("after_merge", afterMerge),
		zap.Int("after_dedupe", afterDedupe),
		zap.Int("after_filter", afterFilter),
	)
	if err := validateASRSummaries(all); err != nil {
		return nil, err
	}
	sortASRSummariesByTime(all)
	if all == nil {
		all = []model.ASRSummarySegment{}
	}
	return all, nil
}

func generateASRSummariesWindow(
	ctx context.Context,
	llmClient LLMChatClient,
	win utteranceWindow,
	durationMs int64,
	logger *zap.Logger,
) ([]model.ASRSummarySegment, asrLLMWindowDebug, error) {
	logger = asrPostLogger(logger)
	userPrompt := buildASRSummariesUserPrompt(win.Utterances, win.Offset, durationMs)
	messages := []llm.ChatMessage{
		{Role: "system", Content: asrSummariesSystemPrompt},
		{Role: "user", Content: userPrompt},
	}
	dbg := asrLLMWindowDebug{Offset: win.Offset, UserPrompt: userPrompt}

	content, err := asrChatStructured(ctx, llmClient, messages, logger)
	dbg.RawResponse = content
	if err != nil {
		return nil, dbg, err
	}

	// 模型已有输出：不再内容重试；解析/校验失败则兜底为空列表。
	segs, err := parseAndResolveASRSummaries(content, win, durationMs)
	if err != nil {
		logger.Warn("ASR summaries 窗结果不可用，已兜底为空",
			zap.Int("window_offset", win.Offset),
			zap.Error(err),
			zap.String("raw_preview", truncateRunes(content, 256)),
		)
		dbg.Segments = []model.ASRSummarySegment{}
		return dbg.Segments, dbg, nil
	}
	// 窗内不过滤时长：短碎片留给 Reduce 跨窗合并后再按 [5,60] 分钟过滤。
	normalizeASRSummaries(segs, durationMs)
	if vErr := validateASRSummaries(segs); vErr != nil {
		logger.Warn("ASR summaries 窗结果不可用，已兜底为空",
			zap.Int("window_offset", win.Offset),
			zap.Error(vErr),
			zap.String("raw_preview", truncateRunes(content, 256)),
		)
		dbg.Segments = []model.ASRSummarySegment{}
		return dbg.Segments, dbg, nil
	}
	dbg.Segments = segs
	return segs, dbg, nil
}

func generateASRParagraphs(ctx context.Context, llmClient LLMChatClient, utterances []asr.Utterance, durationMs int64, rec *asrDebugRecorder, logger *zap.Logger) ([]model.ASRParagraph, error) {
	logger = asrPostLogger(logger).With(zap.String("phase", "paragraphs"))
	// Map：按时长 + rune 预算切窗；各窗并行调 LLM（调用失败或结构不可用则窗内本地相邻合并）。
	windows := splitUtterancesByDuration(utterances, asrParagraphWindowMs, asrLLMWindowMaxRunes)
	type winOut struct {
		ranges []asrParagraphRange
		dbg    asrLLMWindowDebug
	}
	outs := make([]winOut, len(windows))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(asrLLMWindowParallelism)
	for i, win := range windows {
		i, win := i, win
		g.Go(func() error {
			local, dbg, err := generateASRParagraphsWindow(gctx, llmClient, win, logger)
			outs[i] = winOut{ranges: local, dbg: dbg}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	var ranges []asrParagraphRange
	debugWindows := make([]asrLLMWindowDebug, 0, len(windows))
	for i, o := range outs {
		debugWindows = append(debugWindows, o.dbg)
		offset := windows[i].Offset
		for _, r := range o.ranges {
			ranges = append(ranges, asrParagraphRange{
				StartIndex: r.StartIndex + offset,
				EndIndex:   r.EndIndex + offset,
			})
		}
	}
	if rec != nil && len(debugWindows) > 0 {
		rec.Write("004_llm_paragraphs.json", map[string]any{
			"recorded_at": asrDebugRecordedAt(),
			"windows":     debugWindows,
		})
	}

	logger.Info("ASR paragraphs Map 完成",
		zap.Int("windows", len(windows)),
		zap.Int("range_count", len(ranges)),
	)

	// Reduce：局部 index + offset → 全局 ranges → stitch；失败则全文本地重建 → 时间线规范化。
	paragraphs, err := stitchASRParagraphs(utterances, ranges)
	if err != nil {
		logger.Warn("ASR paragraphs 全局拼接失败，已全量本地重建",
			zap.Error(err),
			zap.Int("range_count", len(ranges)),
		)
		ranges = buildParagraphRangesLocally(utterances)
		paragraphs, err = stitchASRParagraphs(utterances, ranges)
		if err != nil {
			return nil, err
		}
	}
	paragraphs, splitCount := enforceASRParagraphMaxRunes(paragraphs)
	finalizeASRParagraphTimeline(paragraphs, durationMs)
	if err := validateASRParagraphWordIdentity(utterances, paragraphs); err != nil {
		return nil, err
	}
	if err := validateASRParagraphContentAlign(paragraphs); err != nil {
		return nil, err
	}
	if err := validateASRParagraphTimeline(paragraphs); err != nil {
		return nil, err
	}
	logger.Info("ASR paragraphs 生成完成",
		zap.Int("paragraph_count", len(paragraphs)),
		zap.Int("split_by_max_runes", splitCount),
	)
	return paragraphs, nil
}

func generateASRParagraphsWindow(
	ctx context.Context,
	llmClient LLMChatClient,
	win utteranceWindow,
	logger *zap.Logger,
) ([]asrParagraphRange, asrLLMWindowDebug, error) {
	logger = asrPostLogger(logger)
	userPrompt := buildASRParagraphsUserPrompt(win.Utterances)
	messages := []llm.ChatMessage{
		{Role: "system", Content: asrParagraphsSystemPrompt},
		{Role: "user", Content: userPrompt},
	}
	dbg := asrLLMWindowDebug{Offset: win.Offset, UserPrompt: userPrompt}

	fallbackLocal := func(reason error) ([]asrParagraphRange, asrLLMWindowDebug, error) {
		logger.Warn("ASR paragraphs 窗不可用，已本地相邻合并重建",
			zap.Int("window_offset", win.Offset),
			zap.Error(reason),
			zap.String("raw_preview", truncateRunes(dbg.RawResponse, 256)),
		)
		local := buildParagraphRangesLocally(win.Utterances)
		dbg.Ranges = local
		return local, dbg, nil
	}

	content, err := asrChatStructured(ctx, llmClient, messages, logger)
	dbg.RawResponse = content
	if err != nil {
		// LLM 传输/超时等失败：最差也回退为相邻句段合并，不中断整次 paragraphs。
		return fallbackLocal(err)
	}

	// 模型已有输出：不再内容重试；结构不可用时本地按说话人相邻合并重建。
	local, parseErr := parseASRParagraphRanges(content)
	var stitchErr error
	if parseErr == nil {
		if _, stitchErr = stitchASRParagraphs(win.Utterances, local); stitchErr == nil {
			dbg.Ranges = local
			return local, dbg, nil
		}
	}
	fallbackErr := parseErr
	if fallbackErr == nil {
		fallbackErr = stitchErr
	}
	return fallbackLocal(fallbackErr)
}

// buildParagraphRangesLocally 按说话人切换，并贪心打包使拼接文本 ≤ asrParagraphMaxRunes。
// 单句本身超过上限时单独成段，交由后续句号拆分兜底。
func buildParagraphRangesLocally(utterances []asr.Utterance) []asrParagraphRange {
	if len(utterances) == 0 {
		return nil
	}
	ranges := make([]asrParagraphRange, 0)
	start := 0
	runes := utf8.RuneCountInString(utterances[0].Text)
	for i := 1; i < len(utterances); i++ {
		sameSpeaker := strings.TrimSpace(utterances[i].Speaker) == strings.TrimSpace(utterances[start].Speaker)
		nextRunes := utf8.RuneCountInString(utterances[i].Text)
		if !sameSpeaker || runes+nextRunes > asrParagraphMaxRunes {
			ranges = append(ranges, asrParagraphRange{StartIndex: start, EndIndex: i - 1})
			start = i
			runes = nextRunes
			continue
		}
		runes += nextRunes
	}
	ranges = append(ranges, asrParagraphRange{StartIndex: start, EndIndex: len(utterances) - 1})
	return ranges
}

type utteranceWindow struct {
	Offset     int
	Utterances []asr.Utterance
}

// splitUtterancesByDuration 先按 windowMs 切窗，再按 maxRunes 收缩（Map 阶段输入预算）。
func splitUtterancesByDuration(utterances []asr.Utterance, windowMs int64, maxRunes int) []utteranceWindow {
	if len(utterances) == 0 {
		return nil
	}

	var windows []utteranceWindow
	start := 0
	for start < len(utterances) {
		end := start
		windowStart := utterances[start].StartTime
		for end+1 < len(utterances) {
			next := utterances[end+1]
			if windowMs > 0 && next.EndTime-windowStart > windowMs {
				break
			}
			end++
		}
		if end < start {
			end = start
		}
		// 单窗仍超长：逐步缩短。
		for end > start {
			chunk := utterances[start : end+1]
			if utf8.RuneCountInString(formatASRTranscriptLines(chunk, 0)) <= maxRunes {
				break
			}
			end--
		}
		windows = append(windows, utteranceWindow{
			Offset:     start,
			Utterances: utterances[start : end+1],
		})
		start = end + 1
	}
	return windows
}

func formatASRTranscriptLines(utterances []asr.Utterance, indexOffset int) string {
	var b strings.Builder
	for i, u := range utterances {
		speaker := u.Speaker
		if speaker == "" {
			speaker = "?"
		}
		fmt.Fprintf(&b, "[%d] speaker=%s t=%d-%d %s\n", indexOffset+i, speaker, u.StartTime, u.EndTime, u.Text)
	}
	return b.String()
}

func buildASRSummariesUserPrompt(utterances []asr.Utterance, indexOffset int, durationMs int64) string {
	var b strings.Builder
	b.WriteString("整场时长(毫秒)：")
	fmt.Fprintf(&b, "%d\n", durationMs)
	fmt.Fprintf(&b, "本窗句段编号范围：%d~%d（闭区间）\n", indexOffset, indexOffset+len(utterances)-1)
	b.WriteString("ASR 句段列表：\n")
	b.WriteString(formatASRTranscriptLines(utterances, indexOffset))
	b.WriteString("\n请输出 JSON 对象 {\"items\":[...]}。")
	return b.String()
}

func buildASRSummariesRepairPrompt(prevErr error, prevContent string) string {
	var b strings.Builder
	b.WriteString("上一次输出未通过校验：")
	b.WriteString(prevErr.Error())
	b.WriteString("\n请只输出修正后的完整 JSON 对象 {\"items\":[...]}。")
	b.WriteString("每项仅含 title/start_index/end_index；title 必须 ≤6 字；索引必须合法。")
	b.WriteString("每段推算时长应在 5~60 分钟，否则该段会被丢弃。")
	b.WriteString("\n上次输出摘要：\n")
	b.WriteString(truncateRunes(prevContent, 800))
	return b.String()
}

func buildASRParagraphsUserPrompt(utterances []asr.Utterance) string {
	var b strings.Builder
	b.WriteString("ASR 句段列表（本窗局部编号从 0 开始）：\n")
	b.WriteString(formatASRTranscriptLines(utterances, 0))
	b.WriteString("\n请输出覆盖本窗全部句段的 JSON 对象 {\"items\":[...]}。")
	return b.String()
}

func buildASRParagraphsRepairPrompt(prevErr error, prevContent string, utteranceCount int) string {
	var b strings.Builder
	b.WriteString("上一次输出未通过校验：")
	b.WriteString(prevErr.Error())
	fmt.Fprintf(&b, "\n本窗共有 %d 个句段，局部编号 0~%d，必须恰好覆盖一次。", utteranceCount, utteranceCount-1)
	b.WriteString("\n请只输出修正后的完整 JSON 对象 {\"items\":[...]}。")
	b.WriteString("\n上次输出摘要：\n")
	b.WriteString(truncateRunes(prevContent, 800))
	return b.String()
}

func parseAndResolveASRSummaries(content string, win utteranceWindow, durationMs int64) ([]model.ASRSummarySegment, error) {
	items, err := parseASRSummaryItems(content)
	if err != nil {
		return nil, err
	}
	return resolveASRSummaries(items, win, durationMs)
}

func parseASRSummaryItems(content string) ([]asrSummaryLLMItem, error) {
	raw, err := extractLLMJSON(content)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Items []asrSummaryLLMItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Items != nil {
		return wrapped.Items, nil
	}
	var items []asrSummaryLLMItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("解析 asr_summaries JSON 失败: %w", err)
	}
	return items, nil
}

func resolveASRSummaries(items []asrSummaryLLMItem, win utteranceWindow, durationMs int64) ([]model.ASRSummarySegment, error) {
	if len(items) == 0 {
		return []model.ASRSummarySegment{}, nil
	}
	n := len(win.Utterances)
	segs := make([]model.ASRSummarySegment, 0, len(items))
	for i, it := range items {
		localStart := it.StartIndex - win.Offset
		localEnd := it.EndIndex - win.Offset
		if localStart < 0 || localEnd >= n || localStart > localEnd {
			return nil, fmt.Errorf("asr_summaries[%d] 下标越界: [%d,%d] (窗 offset=%d, 大小=%d)", i, it.StartIndex, it.EndIndex, win.Offset, n)
		}
		start := win.Utterances[localStart].StartTime
		end := win.Utterances[localEnd].EndTime
		if durationMs > 0 {
			if start < 0 {
				start = 0
			}
			if end > durationMs {
				end = durationMs
			}
		}
		segs = append(segs, model.ASRSummarySegment{
			Title:     it.Title,
			StartTime: start,
			EndTime:   end,
		})
	}
	return segs, nil
}

func parseASRParagraphRanges(content string) ([]asrParagraphRange, error) {
	raw, err := extractLLMJSON(content)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Items []asrParagraphRange `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Items != nil {
		return wrapped.Items, nil
	}
	var ranges []asrParagraphRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, fmt.Errorf("解析 asr_paragraphs 边界 JSON 失败: %w", err)
	}
	return ranges, nil
}

// extractLLMJSON 提取 JSON 对象或数组（兼容 markdown 代码块）。
func extractLLMJSON(content string) (json.RawMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("LLM 返回内容为空")
	}
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```JSON")
		content = strings.TrimPrefix(content, "```")
		if idx := strings.LastIndex(content, "```"); idx >= 0 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}
	if json.Valid([]byte(content)) && (strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[")) {
		return json.RawMessage(content), nil
	}
	if obj := extractJSONObjectLiteral(content); obj != "" && json.Valid([]byte(obj)) {
		return json.RawMessage(obj), nil
	}
	if arr := extractJSONArrayLiteral(content); arr != "" && json.Valid([]byte(arr)) {
		return json.RawMessage(arr), nil
	}
	return nil, fmt.Errorf("无法解析 LLM 返回的 JSON: %s", truncateRunes(content, 256))
}

// extractLLMJSONArray 保留给测试/兼容：优先抽数组。
func extractLLMJSONArray(content string) (json.RawMessage, error) {
	raw, err := extractLLMJSON(content)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		return raw, nil
	}
	var wrapped struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Items) > 0 && strings.HasPrefix(strings.TrimSpace(string(wrapped.Items)), "[") {
		return wrapped.Items, nil
	}
	extracted := extractJSONArrayLiteral(string(raw))
	if extracted == "" || !json.Valid([]byte(extracted)) {
		return nil, fmt.Errorf("无法解析 LLM 返回的 JSON 数组: %s", truncateRunes(string(raw), 256))
	}
	return json.RawMessage(extracted), nil
}

func extractJSONArrayLiteral(s string) string {
	return extractJSONBracketLiteral(s, '[', ']')
}

func extractJSONObjectLiteral(s string) string {
	return extractJSONBracketLiteral(s, '{', '}')
}

func extractJSONBracketLiteral(s string, open, close byte) string {
	start := strings.IndexByte(s, open)
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// normalizeASRSummaries 截断 title、纠正时间边界；不返回 error。
func normalizeASRSummaries(segs []model.ASRSummarySegment, durationMs int64) {
	for i := range segs {
		segs[i].Title = truncateRunesHard(strings.TrimSpace(segs[i].Title), asrSummaryTitleMaxRunes)
		if segs[i].EndTime < segs[i].StartTime {
			segs[i].StartTime, segs[i].EndTime = segs[i].EndTime, segs[i].StartTime
		}
		if durationMs > 0 {
			if segs[i].StartTime < 0 {
				segs[i].StartTime = 0
			}
			if segs[i].EndTime > durationMs {
				segs[i].EndTime = durationMs
			}
		}
	}
}

func sortASRSummariesByTime(segs []model.ASRSummarySegment) {
	sort.SliceStable(segs, func(i, j int) bool {
		if segs[i].StartTime == segs[j].StartTime {
			return segs[i].EndTime < segs[j].EndTime
		}
		return segs[i].StartTime < segs[j].StartTime
	})
}

// mergeASRSummaries 合并时间相邻/相交且 title 相同的段（跨窗碎片归约）；在时长过滤之前调用。
func mergeASRSummaries(segs []model.ASRSummarySegment) []model.ASRSummarySegment {
	if len(segs) == 0 {
		return []model.ASRSummarySegment{}
	}
	sorted := append([]model.ASRSummarySegment(nil), segs...)
	sortASRSummariesByTime(sorted)

	out := make([]model.ASRSummarySegment, 0, len(sorted))
	out = append(out, sorted[0])
	for i := 1; i < len(sorted); i++ {
		cur := sorted[i]
		last := &out[len(out)-1]
		if asrSummaryTitlesEqual(last.Title, cur.Title) && asrSummariesCanMerge(*last, cur) {
			if cur.StartTime < last.StartTime {
				last.StartTime = cur.StartTime
			}
			if cur.EndTime > last.EndTime {
				last.EndTime = cur.EndTime
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}

func asrSummaryTitlesEqual(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// asrSummariesCanMerge 在已按 StartTime 排序的前提下，判断 a 与后段 b 可否合并。
func asrSummariesCanMerge(a, b model.ASRSummarySegment) bool {
	if b.StartTime <= a.EndTime {
		return true // 相交或相接
	}
	return b.StartTime-a.EndTime <= asrSummaryMergeGapMs
}

// dedupeContainedASRSummaries 去掉被同 title 更长段严格包含的短段。
func dedupeContainedASRSummaries(segs []model.ASRSummarySegment) []model.ASRSummarySegment {
	if len(segs) <= 1 {
		if segs == nil {
			return []model.ASRSummarySegment{}
		}
		return segs
	}
	keep := make([]bool, len(segs))
	for i := range keep {
		keep[i] = true
	}
	for i := range segs {
		if !keep[i] {
			continue
		}
		for j := range segs {
			if i == j || !keep[j] {
				continue
			}
			if !asrSummaryTitlesEqual(segs[i].Title, segs[j].Title) {
				continue
			}
			iDur := segs[i].EndTime - segs[i].StartTime
			jDur := segs[j].EndTime - segs[j].StartTime
			// i 被 j 包含（允许端点相等）；等长时保留靠前的。
			if segs[i].StartTime >= segs[j].StartTime && segs[i].EndTime <= segs[j].EndTime {
				if iDur < jDur || (iDur == jDur && i > j) {
					keep[i] = false
					break
				}
			}
		}
	}
	out := make([]model.ASRSummarySegment, 0, len(segs))
	for i, ok := range keep {
		if ok {
			out = append(out, segs[i])
		}
	}
	return out
}

// filterASRSummariesByDuration 丢弃时长不在 [5,60] 分钟的段；可返回空切片。
func filterASRSummariesByDuration(segs []model.ASRSummarySegment, logger *zap.Logger) []model.ASRSummarySegment {
	logger = asrPostLogger(logger)
	if len(segs) == 0 {
		return []model.ASRSummarySegment{}
	}
	out := make([]model.ASRSummarySegment, 0, len(segs))
	for _, s := range segs {
		dur := s.EndTime - s.StartTime
		if dur < asrSummaryMinDurationMs || dur > asrSummaryMaxDurationMs {
			logger.Warn("ASR summaries 段因时长不符被丢弃",
				zap.String("title", s.Title),
				zap.Int64("start_time_ms", s.StartTime),
				zap.Int64("end_time_ms", s.EndTime),
				zap.Int64("duration_ms", dur),
				zap.Int64("min_duration_ms", asrSummaryMinDurationMs),
				zap.Int64("max_duration_ms", asrSummaryMaxDurationMs),
			)
			continue
		}
		out = append(out, s)
	}
	return out
}

// validateASRSummaries 校验保留段；允许空列表（过滤后无合法段时不失败）。
func validateASRSummaries(segs []model.ASRSummarySegment) error {
	for i, s := range segs {
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("asr_summaries[%d] title 不能为空", i)
		}
		if s.EndTime < s.StartTime {
			return fmt.Errorf("asr_summaries[%d] 时间非法", i)
		}
	}
	return nil
}

func stitchASRParagraphs(utterances []asr.Utterance, ranges []asrParagraphRange) ([]model.ASRParagraph, error) {
	if len(ranges) == 0 {
		return nil, fmt.Errorf("asr_paragraphs 边界不能为空")
	}
	n := len(utterances)
	covered := make([]bool, n)
	paragraphs := make([]model.ASRParagraph, 0, len(ranges))

	for i, r := range ranges {
		if r.StartIndex < 0 || r.EndIndex >= n || r.StartIndex > r.EndIndex {
			return nil, fmt.Errorf("asr_paragraphs[%d] 下标越界: [%d,%d]", i, r.StartIndex, r.EndIndex)
		}
		speaker := strings.TrimSpace(utterances[r.StartIndex].Speaker)
		var textBuilder strings.Builder
		words := make([]model.ClipWord, 0)
		for idx := r.StartIndex; idx <= r.EndIndex; idx++ {
			if covered[idx] {
				return nil, fmt.Errorf("asr_paragraphs 句段 %d 被重复覆盖", idx)
			}
			covered[idx] = true
			u := utterances[idx]
			uSpeaker := strings.TrimSpace(u.Speaker)
			if speaker == "" {
				speaker = uSpeaker
			}
			if uSpeaker != speaker {
				return nil, fmt.Errorf("asr_paragraphs[%d] 含多个说话人", i)
			}
			textBuilder.WriteString(u.Text)
			// 原样拷贝源 words（含空格词与 -1 时间），不插入标点、不改写时间。
			for _, w := range u.Words {
				words = append(words, model.ClipWord{
					Text:      w.Text,
					StartTime: w.StartTime,
					EndTime:   w.EndTime,
				})
			}
		}
		text := textBuilder.String()
		// 段级时间与 words 首尾对齐：start=words[0].start_time，end=words[last].end_time。
		p := model.ASRParagraph{
			Speaker: speaker,
			Text:    text,
			Words:   words,
		}
		syncASRParagraphTimesFromWords(&p)
		paragraphs = append(paragraphs, p)
	}

	for i, ok := range covered {
		if !ok {
			return nil, fmt.Errorf("asr_paragraphs 未覆盖句段 %d", i)
		}
	}

	// 保持句段覆盖顺序（与 live_asr 文本顺序一致），不做时间排序以免打乱正文。
	var full strings.Builder
	for _, u := range utterances {
		full.WriteString(u.Text)
	}
	var joined strings.Builder
	for _, p := range paragraphs {
		joined.WriteString(p.Text)
	}
	if full.String() != joined.String() {
		return nil, fmt.Errorf("asr_paragraphs 拼接结果与完整 ASR 不一致")
	}
	return paragraphs, nil
}

// enforceASRParagraphMaxRunes 将超长段落按句号兜底拆分，保证每段正文 ≤ asrParagraphMaxRunes。
// 拆分只分区源 words，不增删、不改写词级时间。
func enforceASRParagraphMaxRunes(paragraphs []model.ASRParagraph) ([]model.ASRParagraph, int) {
	if len(paragraphs) == 0 {
		return paragraphs, 0
	}
	out := make([]model.ASRParagraph, 0, len(paragraphs))
	splitCount := 0
	for _, p := range paragraphs {
		if utf8.RuneCountInString(p.Text) <= asrParagraphMaxRunes {
			out = append(out, p)
			continue
		}
		split := splitASRParagraphBySentences(p, asrParagraphMaxRunes)
		if len(split) == 1 && split[0].Text == p.Text {
			// 切分失败回退整段（仍可能 >max；由上游 ranges 尽量避免）。
			out = append(out, p)
			continue
		}
		splitCount++
		out = append(out, split...)
	}
	return out, splitCount
}

// splitASRParagraphBySentences 按句末标点切开后贪心打包；单句仍超限则按 rune 硬切。
// words 按「去句读后的正文」分区源词流；每段必须满足 strip(text)==strip(join(words))；失败则回退整段。
func splitASRParagraphBySentences(p model.ASRParagraph, maxRunes int) []model.ASRParagraph {
	if maxRunes <= 0 || utf8.RuneCountInString(p.Text) <= maxRunes {
		return []model.ASRParagraph{p}
	}
	parts := splitTextBySentenceEnds(p.Text)
	chunks := packSentenceParts(parts, maxRunes)
	if len(chunks) == 0 {
		return []model.ASRParagraph{p}
	}

	out := make([]model.ASRParagraph, 0, len(chunks))
	restWords := p.Words
	origWordCount := len(p.Words)
	runeOffset := 0
	totalRunes := utf8.RuneCountInString(p.Text)
	for ci, chunk := range chunks {
		chunkRunes := utf8.RuneCountInString(chunk)
		fbStart := interpolateParagraphTime(p.StartTime, p.EndTime, runeOffset, totalRunes)
		fbEnd := interpolateParagraphTime(p.StartTime, p.EndTime, runeOffset+chunkRunes, totalRunes)
		if ci == 0 {
			fbStart = p.StartTime
		}
		if ci == len(chunks)-1 {
			fbEnd = p.EndTime
		}

		taken, nextRest := takeWordsForText(restWords, chunk)
		if ci == len(chunks)-1 {
			// 末段：仅允许吞掉「去句读后为空」的残留词（罕见）；若仍有正文词则切分失败。
			for _, w := range nextRest {
				if stripASRAlignPunct(w.Text) != "" {
					return []model.ASRParagraph{p}
				}
				taken = append(taken, w)
			}
			nextRest = nil
		}
		if !asrParagraphContentAligned(chunk, taken) {
			return []model.ASRParagraph{p}
		}
		restWords = nextRest

		seg := model.ASRParagraph{
			Speaker: p.Speaker,
			Text:    chunk,
			Words:   taken,
		}
		syncASRParagraphTimesFromWords(&seg)
		if seg.EndTime <= seg.StartTime {
			// 无有效字时间时回退插值，避免非法区间。
			seg.StartTime = fbStart
			seg.EndTime = fbEnd
			if seg.EndTime <= seg.StartTime {
				seg.EndTime = seg.StartTime + 1
			}
		}
		out = append(out, seg)
		runeOffset += chunkRunes
	}
	if len(restWords) != 0 {
		return []model.ASRParagraph{p}
	}
	gotWords := 0
	for _, seg := range out {
		gotWords += len(seg.Words)
	}
	if gotWords != origWordCount {
		return []model.ASRParagraph{p}
	}
	return out
}

func interpolateParagraphTime(start, end int64, runePos, totalRunes int) int64 {
	if totalRunes <= 0 {
		return start
	}
	if runePos <= 0 {
		return start
	}
	if runePos >= totalRunes {
		return end
	}
	span := end - start
	return start + span*int64(runePos)/int64(totalRunes)
}

// isASRAlignPunct 判断是否为「正文有、words 常无」的句读标点（小数点除外）。
func isASRAlignPunct(r rune) bool {
	switch r {
	case '，', '。', '！', '？', '、', '；', '：', '…',
		',', '!', '?', ';', ':':
		return true
	case '.':
		return true // 对齐阶段先当标点；小数点由调用方结合上下文保留
	default:
		return false
	}
}

// isASRAlignPunctInText 在 text 位置 i 是否应按对齐噪声跳过（保留小数点）。
func isASRAlignPunctInText(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) {
		return false
	}
	r := runes[i]
	if r == '.' || r == '．' {
		if i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1]) {
			return false
		}
		return true
	}
	return isASRAlignPunct(r) && r != '.'
}

// stripASRAlignPunct 去掉句读标点（保留空格与小数点），用于 text/words 内容对齐。
func stripASRAlignPunct(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range runes {
		if isASRAlignPunctInText(runes, i) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// takeWordsForText 从 words 头部消费与 text「去句读后正文」对齐的源词切片。
// 不增删词、不改时间；源词中的空格等一律保留。允许 join(words)!=text。
func takeWordsForText(words []model.ClipWord, text string) (taken []model.ClipWord, rest []model.ClipWord) {
	if text == "" {
		return nil, words
	}
	if len(words) == 0 {
		return nil, nil
	}
	need := []rune(stripASRAlignPunct(text))
	if len(need) == 0 {
		// 纯句读 chunk：不消费源词。
		return nil, words
	}
	taken = make([]model.ClipWord, 0, len(words))
	ni := 0
	pendingEmpty := make([]model.ClipWord, 0)
	for i, w := range words {
		wr := []rune(stripASRAlignPunct(w.Text))
		if len(wr) == 0 {
			// 源侧偶发纯句读/空词：暂挂，跟下一段内容词一起划入，避免丢失破坏 1:1。
			pendingEmpty = append(pendingEmpty, w)
			continue
		}
		if ni+len(wr) > len(need) {
			return taken, append(append([]model.ClipWord{}, pendingEmpty...), words[i:]...)
		}
		for j, r := range wr {
			if need[ni+j] != r {
				return taken, append(append([]model.ClipWord{}, pendingEmpty...), words[i:]...)
			}
		}
		if len(pendingEmpty) > 0 {
			taken = append(taken, pendingEmpty...)
			pendingEmpty = pendingEmpty[:0]
		}
		taken = append(taken, w)
		ni += len(wr)
		if ni == len(need) {
			return taken, words[i+1:]
		}
	}
	if len(pendingEmpty) > 0 {
		taken = append(taken, pendingEmpty...)
	}
	return taken, nil
}

func joinedClipWordText(words []model.ClipWord) string {
	var b strings.Builder
	for _, w := range words {
		b.WriteString(w.Text)
	}
	return b.String()
}

// flattenUtteranceWords 按序展平 live_asr 字级词（原样拷贝）。
func flattenUtteranceWords(utterances []asr.Utterance) []model.ClipWord {
	n := 0
	for _, u := range utterances {
		n += len(u.Words)
	}
	out := make([]model.ClipWord, 0, n)
	for _, u := range utterances {
		for _, w := range u.Words {
			out = append(out, model.ClipWord{
				Text:      w.Text,
				StartTime: w.StartTime,
				EndTime:   w.EndTime,
			})
		}
	}
	return out
}

func flattenParagraphWords(paragraphs []model.ASRParagraph) []model.ClipWord {
	n := 0
	for _, p := range paragraphs {
		n += len(p.Words)
	}
	out := make([]model.ClipWord, 0, n)
	for _, p := range paragraphs {
		out = append(out, p.Words...)
	}
	return out
}

// validateASRParagraphWordIdentity 校验 asr_paragraphs.words 与 live_asr utterances.words
// 总数相等且按序一一对应（text/start_time/end_time）。
func validateASRParagraphWordIdentity(utterances []asr.Utterance, paragraphs []model.ASRParagraph) error {
	want := flattenUtteranceWords(utterances)
	got := flattenParagraphWords(paragraphs)
	if len(got) != len(want) {
		return fmt.Errorf("asr_paragraphs words 总数=%d 与 live_asr words 总数=%d 不一致", len(got), len(want))
	}
	for i := range want {
		if got[i].Text != want[i].Text || got[i].StartTime != want[i].StartTime || got[i].EndTime != want[i].EndTime {
			return fmt.Errorf("asr_paragraphs.words[%d] 与 live_asr 不一致: got={%q,%d,%d} want={%q,%d,%d}",
				i, got[i].Text, got[i].StartTime, got[i].EndTime,
				want[i].Text, want[i].StartTime, want[i].EndTime)
		}
	}
	return nil
}

// asrParagraphContentAligned 判断去句读后 text 与 join(words) 正文一致。
// 两侧使用同一套 strip，以兼容词内偶发标点（如 "5:5"）。
func asrParagraphContentAligned(text string, words []model.ClipWord) bool {
	return stripASRAlignPunct(text) == stripASRAlignPunct(joinedClipWordText(words))
}

// validateASRParagraphContentAlign 校验每段 strip(text)==strip(join(words))。
func validateASRParagraphContentAlign(paragraphs []model.ASRParagraph) error {
	for i, p := range paragraphs {
		if !asrParagraphContentAligned(p.Text, p.Words) {
			return fmt.Errorf("asr_paragraphs[%d] 去标点正文与 words 不一致: text_stripped=%q words_stripped=%q",
				i,
				truncateRunes(stripASRAlignPunct(p.Text), 64),
				truncateRunes(stripASRAlignPunct(joinedClipWordText(p.Words)), 64),
			)
		}
	}
	return nil
}

func splitTextBySentenceEnds(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	parts := make([]string, 0)
	var cur strings.Builder
	for i := 0; i < len(runes); i++ {
		cur.WriteRune(runes[i])
		if isASRSentenceEnd(runes, i) {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func isASRSentenceEnd(runes []rune, i int) bool {
	r := runes[i]
	switch r {
	case '。', '！', '？', '；', '…':
		return true
	case '!', '?':
		return true
	case '.':
		if i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1]) {
			return false
		}
		return true
	default:
		return false
	}
}

func packSentenceParts(parts []string, maxRunes int) []string {
	if len(parts) == 0 {
		return nil
	}
	out := make([]string, 0, len(parts))
	cur := ""
	flush := func() {
		if cur == "" {
			return
		}
		out = append(out, cur)
		cur = ""
	}
	for _, part := range parts {
		if utf8.RuneCountInString(part) > maxRunes {
			flush()
			out = append(out, hardSplitRunes(part, maxRunes)...)
			continue
		}
		if cur == "" {
			cur = part
			continue
		}
		if utf8.RuneCountInString(cur+part) <= maxRunes {
			cur += part
			continue
		}
		flush()
		cur = part
	}
	flush()
	return out
}

func hardSplitRunes(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return []string{s}
	}
	out := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

// asrTimeValid 判断 live_asr 时间戳是否可用（豆包空格等非法值为 -1）。
func asrTimeValid(t int64) bool {
	return t >= 0
}

// utteranceTimeBounds 取句段起止；句段时间非法时回退到字级有效时间。
func utteranceTimeBounds(u asr.Utterance) (start, end int64) {
	start, end = u.StartTime, u.EndTime
	if asrTimeValid(start) && asrTimeValid(end) && end >= start {
		return start, end
	}
	words := make([]model.ClipWord, 0, len(u.Words))
	for _, w := range u.Words {
		words = append(words, model.ClipWord{Text: w.Text, StartTime: w.StartTime, EndTime: w.EndTime})
	}
	return paragraphTimesFromWords(words, start, end)
}

// paragraphTimesFromWords 按词序取首个有效 start、末个有效 end；否则用 fallback。
func paragraphTimesFromWords(words []model.ClipWord, fallbackStart, fallbackEnd int64) (start, end int64) {
	start, end = fallbackStart, fallbackEnd
	for _, w := range words {
		if asrTimeValid(w.StartTime) {
			start = w.StartTime
			break
		}
	}
	for i := len(words) - 1; i >= 0; i-- {
		if asrTimeValid(words[i].EndTime) {
			end = words[i].EndTime
			break
		}
	}
	if !asrTimeValid(start) {
		start = 0
	}
	if !asrTimeValid(end) {
		end = start
	}
	if end < start {
		start, end = end, start
	}
	return start, end
}

// repairASRWordTimes 将 live_asr 中 -1/颠倒的字级时间，用左右相邻有效时间修补（仍锚定 ASR 原文时间轴）。
func repairASRWordTimes(words []model.ClipWord, fallbackStart, fallbackEnd int64) []model.ClipWord {
	if len(words) == 0 {
		return nil
	}
	out := append([]model.ClipWord(nil), words...)

	leftAnchor := make([]int64, len(out))
	last := fallbackStart
	if !asrTimeValid(last) {
		last = 0
	}
	for i := range out {
		leftAnchor[i] = last
		if asrTimeValid(out[i].EndTime) {
			last = out[i].EndTime
		} else if asrTimeValid(out[i].StartTime) {
			last = out[i].StartTime
		}
	}

	rightAnchor := make([]int64, len(out))
	next := fallbackEnd
	if !asrTimeValid(next) {
		next = last
	}
	for i := len(out) - 1; i >= 0; i-- {
		rightAnchor[i] = next
		if asrTimeValid(out[i].StartTime) {
			next = out[i].StartTime
		} else if asrTimeValid(out[i].EndTime) {
			next = out[i].EndTime
		}
	}

	for i := range out {
		w := &out[i]
		startOK := asrTimeValid(w.StartTime)
		endOK := asrTimeValid(w.EndTime)
		if startOK && endOK && w.EndTime >= w.StartTime {
			continue
		}
		left := leftAnchor[i]
		right := rightAnchor[i]
		if !startOK && !endOK {
			w.StartTime = left
			if right > left {
				w.EndTime = right
			} else {
				w.EndTime = left
			}
			continue
		}
		if !startOK {
			w.StartTime = left
			if endOK && w.EndTime < w.StartTime {
				w.StartTime = w.EndTime
			}
			continue
		}
		if !endOK {
			w.EndTime = right
			if w.EndTime < w.StartTime {
				w.EndTime = w.StartTime
			}
			continue
		}
		// start/end 均有效但颠倒
		w.StartTime, w.EndTime = w.EndTime, w.StartTime
	}
	return out
}

// syncASRParagraphTimesFromWords 将段级时间对齐为：
// start_time = words[0].start_time，end_time = words[len-1].end_time。
// 若首/末词时间为 -1 等非法值，则回退到段内首个/末个有效字时间（无法把 -1 写成合法段边界）。
// 返回是否成功从 words 得到合法区间。
func syncASRParagraphTimesFromWords(p *model.ASRParagraph) bool {
	if p == nil || len(p.Words) == 0 {
		return false
	}
	start := p.Words[0].StartTime
	end := p.Words[len(p.Words)-1].EndTime
	if !asrTimeValid(start) || !asrTimeValid(end) || end < start {
		ws, we, ok := paragraphWordTimeSpan(p.Words)
		if !ok {
			return false
		}
		if !asrTimeValid(start) {
			start = ws
		}
		if !asrTimeValid(end) || end < start {
			end = we
		}
	}
	p.StartTime = start
	p.EndTime = end
	if p.EndTime <= p.StartTime {
		p.EndTime = p.StartTime + 1
	}
	return true
}

// paragraphWordTimeSpan 取段内首个有效字 start、末个有效字 end；无有效时间则 ok=false。
func paragraphWordTimeSpan(words []model.ClipWord) (start, end int64, ok bool) {
	start, end = -1, -1
	for _, w := range words {
		if asrTimeValid(w.StartTime) {
			start = w.StartTime
			break
		}
	}
	for i := len(words) - 1; i >= 0; i-- {
		if asrTimeValid(words[i].EndTime) {
			end = words[i].EndTime
			break
		}
	}
	if !asrTimeValid(start) || !asrTimeValid(end) {
		return 0, 0, false
	}
	if end < start {
		start, end = end, start
	}
	return start, end, true
}

// finalizeASRParagraphTimeline 将每段 start/end 与 words 首尾对齐。
// 不再为「消重叠」改写 start（否则会破坏 start==words[0].start_time）。
// 不改写 words。durationMs 仅作无 words 时可参考的上限，有 words 时以字级时间为准。
func finalizeASRParagraphTimeline(paragraphs []model.ASRParagraph, durationMs int64) {
	if len(paragraphs) == 0 {
		return
	}
	for i := range paragraphs {
		p := &paragraphs[i]
		if syncASRParagraphTimesFromWords(p) {
			continue
		}
		// 无 words：仅修复非法区间，不强制消重叠。
		if !asrTimeValid(p.StartTime) || !asrTimeValid(p.EndTime) || p.EndTime <= p.StartTime {
			if durationMs > 0 && (!asrTimeValid(p.EndTime) || p.EndTime <= p.StartTime) {
				if !asrTimeValid(p.StartTime) || p.StartTime < 0 {
					p.StartTime = 0
				}
				p.EndTime = p.StartTime + 1
				if p.EndTime > durationMs && durationMs > p.StartTime {
					p.EndTime = durationMs
				}
			} else if p.EndTime <= p.StartTime {
				p.EndTime = p.StartTime + 1
			}
		}
	}
}

// ensureASRParagraphCoversWords 保留旧名；现与 syncASRParagraphTimesFromWords 等价。
func ensureASRParagraphCoversWords(p *model.ASRParagraph) {
	syncASRParagraphTimesFromWords(p)
}

// normalizeASRParagraphTimeline 保留旧名供测试调用，行为同 finalizeASRParagraphTimeline。
func normalizeASRParagraphTimeline(paragraphs []model.ASRParagraph, durationMs int64) {
	finalizeASRParagraphTimeline(paragraphs, durationMs)
}

// validateASRParagraphTimeline 校验段落时间线硬约束（段级）；词级允许与 live_asr 一致的 -1。
// 有 words 时要求 start/end 与首尾字时间一致；允许相邻段时间重叠（字级时间轴本身可能交错）。
func validateASRParagraphTimeline(paragraphs []model.ASRParagraph) error {
	for i, p := range paragraphs {
		if p.StartTime >= p.EndTime {
			return fmt.Errorf("asr_paragraphs[%d] 时间非法: start_time=%d >= end_time=%d", i, p.StartTime, p.EndTime)
		}
		for j, w := range p.Words {
			// 与源 ASR 一致：允许 -1；若两端均有效则不得颠倒。
			if asrTimeValid(w.StartTime) && asrTimeValid(w.EndTime) && w.EndTime < w.StartTime {
				return fmt.Errorf("asr_paragraphs[%d].words[%d] 时间颠倒: start=%d end=%d", i, j, w.StartTime, w.EndTime)
			}
		}
		if len(p.Words) > 0 {
			w0 := p.Words[0]
			wLast := p.Words[len(p.Words)-1]
			if asrTimeValid(w0.StartTime) && p.StartTime != w0.StartTime {
				return fmt.Errorf("asr_paragraphs[%d] start_time=%d != words[0].start_time=%d", i, p.StartTime, w0.StartTime)
			}
			if asrTimeValid(wLast.EndTime) && p.EndTime != wLast.EndTime {
				return fmt.Errorf("asr_paragraphs[%d] end_time=%d != words[last].end_time=%d", i, p.EndTime, wLast.EndTime)
			}
		}
	}
	return nil
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "..."
}

// truncateRunesHard 按 rune 截断到 max，不加省略号；max<=0 时返回原串。
func truncateRunesHard(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
