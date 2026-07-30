package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
)

const (
	asrPostprocessMaxInputRunes = 80000
	asrSummaryMinDurationMs     = int64(5 * 60 * 1000)  // 5 分钟
	asrSummaryMaxDurationMs     = int64(60 * 60 * 1000) // 60 分钟
	asrSummaryTitleMaxRunes     = 6
	asrSummaryTextMaxRunes      = 30
	asrParagraphMaxRunes        = 300
	asrParagraphWindowMs        = int64(25 * 60 * 1000) // 段落窗口约 25 分钟
	asrSummaryWindowMs          = int64(60 * 60 * 1000) // 总结窗口约 60 分钟
	asrPostprocessMaxRepair     = 1                     // 校验失败最多再修 1 次
)

const asrSummariesSystemPrompt = `你是直播内容主题提炼助手。根据带编号的 ASR 句段列表，提炼若干「核心主题」分段。
要求：
1. 只输出一个 JSON 对象，格式严格为 {"items":[...]}，不要输出其它文字或 markdown。
2. items 每项格式：{"title":"...","summary":"...","start_index":0,"end_index":3}，为句段编号闭区间（含两端）。
3. title 必须严格不超过 6 个汉字（按 Unicode 字符计，含标点）；禁止写成短句。优先用 2~4 字主题词，例如「开场互动」「产品讲解」「福利促销」。坏例：「今天给大家介绍优惠活动」。
4. summary 不超过 30 个字，提炼核心主题，不要复述全文。
5. 每段对应时长（由 start_index~end_index 句段时间推算）默认应在 5~60 分钟；若整场时长不足 5 分钟，可输出覆盖全场句段的一段。
6. 各段是核心主题，不必覆盖全文；段与段可以连续、间断或索引相交。
7. start_index/end_index 必须落在输入句段编号范围内，且 start_index<=end_index。
8. 至少输出 1 段。`

const asrParagraphsSystemPrompt = `你是直播 ASR 段落划分助手。根据带编号的 ASR 句段列表，将全文划分为连续段落。
要求：
1. 只输出一个 JSON 对象，格式严格为 {"items":[...]}，不要输出其它文字或 markdown。
2. items 每项格式：{"start_index":0,"end_index":3}，为句段编号闭区间（含两端）。
3. 所有编号必须恰好覆盖输入中的全部句段一次：无遗漏、无重叠。
4. 每个区间内只能有一个说话人（speaker 相同）。
5. 每个区间拼接后的正文字数必须小于 300 字。
6. 按时间顺序输出区间。`

// asrParagraphRange LLM 返回的段落边界（utterance 闭区间下标）。
type asrParagraphRange struct {
	StartIndex int `json:"start_index"`
	EndIndex   int `json:"end_index"`
}

// asrSummaryLLMItem LLM 返回的总结项（句段 index 锚点）。
type asrSummaryLLMItem struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
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

// runASRPostprocess 调用 LLM 生成 summaries 与 paragraphs；任一步失败返回 error。
// rec 非空时将 LLM 中间过程写入 003/004 调试文件。
func runASRPostprocess(ctx context.Context, llmClient LLMChatClient, liveASR string, durationMs int64, rec *asrDebugRecorder) (asrPostprocessResult, error) {
	var out asrPostprocessResult
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

	summaries, err := generateASRSummaries(ctx, llmClient, utterances, durationMs, rec)
	if err != nil {
		return out, fmt.Errorf("生成 asr_summaries 失败: %w", err)
	}
	paragraphs, err := generateASRParagraphs(ctx, llmClient, utterances, rec)
	if err != nil {
		return out, fmt.Errorf("生成 asr_paragraphs 失败: %w", err)
	}
	out.Summaries = summaries
	out.Paragraphs = paragraphs
	return out, nil
}

func asrChatStructured(ctx context.Context, llmClient LLMChatClient, messages []llm.ChatMessage) (string, error) {
	return llmClient.ChatStructured(ctx, messages)
}

func generateASRSummaries(ctx context.Context, llmClient LLMChatClient, utterances []asr.Utterance, durationMs int64, rec *asrDebugRecorder) ([]model.ASRSummarySegment, error) {
	windows := splitUtterancesByDuration(utterances, asrSummaryWindowMs, asrPostprocessMaxInputRunes)
	var all []model.ASRSummarySegment
	debugWindows := make([]asrLLMWindowDebug, 0, len(windows))
	defer func() {
		if rec == nil || len(debugWindows) == 0 {
			return
		}
		rec.Write("003_llm_summaries.json", map[string]any{
			"recorded_at": asrDebugRecordedAt(),
			"windows":     debugWindows,
		})
	}()
	for _, win := range windows {
		segs, dbg, err := generateASRSummariesWindow(ctx, llmClient, win, durationMs)
		debugWindows = append(debugWindows, dbg)
		if err != nil {
			return nil, err
		}
		all = append(all, segs...)
	}
	normalizeASRSummaries(all, durationMs)
	if err := validateASRSummaries(all, durationMs); err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].StartTime == all[j].StartTime {
			return all[i].EndTime < all[j].EndTime
		}
		return all[i].StartTime < all[j].StartTime
	})
	return all, nil
}

func generateASRSummariesWindow(
	ctx context.Context,
	llmClient LLMChatClient,
	win utteranceWindow,
	durationMs int64,
) ([]model.ASRSummarySegment, asrLLMWindowDebug, error) {
	userPrompt := buildASRSummariesUserPrompt(win.Utterances, win.Offset, durationMs)
	messages := []llm.ChatMessage{
		{Role: "system", Content: asrSummariesSystemPrompt},
		{Role: "user", Content: userPrompt},
	}
	dbg := asrLLMWindowDebug{Offset: win.Offset, UserPrompt: userPrompt}

	content, err := asrChatStructured(ctx, llmClient, messages)
	dbg.RawResponse = content
	if err != nil {
		return nil, dbg, err
	}

	segs, err := parseAndResolveASRSummaries(content, win, durationMs)
	if err == nil {
		normalizeASRSummaries(segs, durationMs)
		if vErr := validateASRSummaries(segs, durationMs); vErr == nil {
			dbg.Segments = segs
			return segs, dbg, nil
		} else {
			err = vErr
		}
	}

	// 校验/解析失败：带上错误原因再修一次。
	for attempt := 0; attempt < asrPostprocessMaxRepair; attempt++ {
		repairPrompt := buildASRSummariesRepairPrompt(err, content)
		dbg.RepairPrompt = repairPrompt
		repairMsgs := append(append([]llm.ChatMessage{}, messages...),
			llm.ChatMessage{Role: "assistant", Content: content},
			llm.ChatMessage{Role: "user", Content: repairPrompt},
		)
		repaired, rErr := asrChatStructured(ctx, llmClient, repairMsgs)
		dbg.RepairRaw = repaired
		if rErr != nil {
			return nil, dbg, rErr
		}
		content = repaired
		segs, err = parseAndResolveASRSummaries(content, win, durationMs)
		if err != nil {
			continue
		}
		normalizeASRSummaries(segs, durationMs)
		if err = validateASRSummaries(segs, durationMs); err != nil {
			continue
		}
		dbg.Segments = segs
		return segs, dbg, nil
	}
	return nil, dbg, err
}

func generateASRParagraphs(ctx context.Context, llmClient LLMChatClient, utterances []asr.Utterance, rec *asrDebugRecorder) ([]model.ASRParagraph, error) {
	windows := splitUtterancesByDuration(utterances, asrParagraphWindowMs, asrPostprocessMaxInputRunes)
	var ranges []asrParagraphRange
	debugWindows := make([]asrLLMWindowDebug, 0, len(windows))
	defer func() {
		if rec == nil || len(debugWindows) == 0 {
			return
		}
		rec.Write("004_llm_paragraphs.json", map[string]any{
			"recorded_at": asrDebugRecordedAt(),
			"windows":     debugWindows,
		})
	}()
	for _, win := range windows {
		local, dbg, err := generateASRParagraphsWindow(ctx, llmClient, win)
		debugWindows = append(debugWindows, dbg)
		if err != nil {
			return nil, err
		}
		for _, r := range local {
			ranges = append(ranges, asrParagraphRange{
				StartIndex: r.StartIndex + win.Offset,
				EndIndex:   r.EndIndex + win.Offset,
			})
		}
	}
	return stitchASRParagraphs(utterances, ranges)
}

func generateASRParagraphsWindow(
	ctx context.Context,
	llmClient LLMChatClient,
	win utteranceWindow,
) ([]asrParagraphRange, asrLLMWindowDebug, error) {
	userPrompt := buildASRParagraphsUserPrompt(win.Utterances)
	messages := []llm.ChatMessage{
		{Role: "system", Content: asrParagraphsSystemPrompt},
		{Role: "user", Content: userPrompt},
	}
	dbg := asrLLMWindowDebug{Offset: win.Offset, UserPrompt: userPrompt}

	content, err := asrChatStructured(ctx, llmClient, messages)
	dbg.RawResponse = content
	if err != nil {
		return nil, dbg, err
	}

	local, err := parseASRParagraphRanges(content)
	if err == nil {
		if _, sErr := stitchASRParagraphs(win.Utterances, local); sErr == nil {
			dbg.Ranges = local
			return local, dbg, nil
		} else {
			err = sErr
		}
	}

	for attempt := 0; attempt < asrPostprocessMaxRepair; attempt++ {
		repairPrompt := buildASRParagraphsRepairPrompt(err, content, len(win.Utterances))
		dbg.RepairPrompt = repairPrompt
		repairMsgs := append(append([]llm.ChatMessage{}, messages...),
			llm.ChatMessage{Role: "assistant", Content: content},
			llm.ChatMessage{Role: "user", Content: repairPrompt},
		)
		repaired, rErr := asrChatStructured(ctx, llmClient, repairMsgs)
		dbg.RepairRaw = repaired
		if rErr != nil {
			return nil, dbg, rErr
		}
		content = repaired
		local, err = parseASRParagraphRanges(content)
		if err != nil {
			continue
		}
		if _, err = stitchASRParagraphs(win.Utterances, local); err != nil {
			continue
		}
		dbg.Ranges = local
		return local, dbg, nil
	}
	return nil, dbg, err
}

type utteranceWindow struct {
	Offset     int
	Utterances []asr.Utterance
}

// splitUtterancesByDuration 始终按 windowMs 切窗；单窗输入超过 maxRunes 时再按条数收缩。
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
	b.WriteString("title 必须 ≤6 字，summary 必须 ≤30 字；start_index/end_index 必须合法。")
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
		return nil, fmt.Errorf("asr_summaries items 不能为空")
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
			Summary:   it.Summary,
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

// normalizeASRSummaries 截断字数、纠正时间边界；不返回 error。
func normalizeASRSummaries(segs []model.ASRSummarySegment, durationMs int64) {
	for i := range segs {
		segs[i].Title = truncateRunesHard(strings.TrimSpace(segs[i].Title), asrSummaryTitleMaxRunes)
		segs[i].Summary = truncateRunesHard(strings.TrimSpace(segs[i].Summary), asrSummaryTextMaxRunes)
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

func validateASRSummaries(segs []model.ASRSummarySegment, durationMs int64) error {
	if len(segs) == 0 {
		return fmt.Errorf("asr_summaries 不能为空")
	}
	allowShortFull := durationMs > 0 && durationMs < asrSummaryMinDurationMs
	for i, s := range segs {
		title := strings.TrimSpace(s.Title)
		summary := strings.TrimSpace(s.Summary)
		if title == "" || summary == "" {
			return fmt.Errorf("asr_summaries[%d] title/summary 不能为空", i)
		}
		// 字数超限已在 normalize 截断，此处不再报错。
		if s.EndTime < s.StartTime {
			return fmt.Errorf("asr_summaries[%d] 时间非法", i)
		}
		dur := s.EndTime - s.StartTime
		if allowShortFull {
			continue
		}
		if dur < asrSummaryMinDurationMs || dur > asrSummaryMaxDurationMs {
			return fmt.Errorf("asr_summaries[%d] 时长须在 5~60 分钟", i)
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
			for _, w := range u.Words {
				words = append(words, model.ClipWord{
					Text:      w.Text,
					StartTime: w.StartTime,
					EndTime:   w.EndTime,
				})
			}
		}
		text := textBuilder.String()
		if utf8.RuneCountInString(text) >= asrParagraphMaxRunes {
			return nil, fmt.Errorf("asr_paragraphs[%d] 字数须小于 %d", i, asrParagraphMaxRunes)
		}
		paragraphs = append(paragraphs, model.ASRParagraph{
			Speaker:   speaker,
			Text:      text,
			StartTime: utterances[r.StartIndex].StartTime,
			EndTime:   utterances[r.EndIndex].EndTime,
			Words:     words,
		})
	}

	for i, ok := range covered {
		if !ok {
			return nil, fmt.Errorf("asr_paragraphs 未覆盖句段 %d", i)
		}
	}

	sort.SliceStable(paragraphs, func(i, j int) bool {
		if paragraphs[i].StartTime == paragraphs[j].StartTime {
			return paragraphs[i].EndTime < paragraphs[j].EndTime
		}
		return paragraphs[i].StartTime < paragraphs[j].StartTime
	})
	for i := 1; i < len(paragraphs); i++ {
		if paragraphs[i].StartTime < paragraphs[i-1].EndTime {
			return nil, fmt.Errorf("asr_paragraphs 时间线相交: [%d,%d) 与后续段", paragraphs[i-1].StartTime, paragraphs[i-1].EndTime)
		}
	}

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
