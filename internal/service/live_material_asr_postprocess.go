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
	asrParagraphWindowMs        = int64(25 * 60 * 1000) // 超长文本段落窗口约 25 分钟
	asrSummaryWindowMs          = int64(60 * 60 * 1000) // 超长文本总结窗口约 60 分钟
)

const asrSummariesSystemPrompt = `你是直播内容主题提炼助手。根据带编号的 ASR 句段列表，提炼若干「核心主题」分段。
要求：
1. 只输出一个 JSON 数组，不要输出其它文字或 markdown。
2. 每项格式：{"title":"...","summary":"...","start_time":0,"end_time":0}，时间为毫秒。
3. title 不超过 6 个字；summary 不超过 30 个字，提炼核心主题，不要复述全文。
4. 每段时长默认应在 5~60 分钟；若整场时长不足 5 分钟，可输出覆盖全场的一段。
5. 各段是核心主题，不必覆盖全文；段与段可以连续、间断或时间相交。
6. 至少输出 1 段。`

const asrParagraphsSystemPrompt = `你是直播 ASR 段落划分助手。根据带编号的 ASR 句段列表，将全文划分为连续段落。
要求：
1. 只输出一个 JSON 数组，不要输出其它文字或 markdown。
2. 每项格式：{"start_index":0,"end_index":3}，为句段编号闭区间（含两端）。
3. 所有编号必须恰好覆盖输入中的全部句段一次：无遗漏、无重叠。
4. 每个区间内只能有一个说话人（speaker 相同）。
5. 每个区间拼接后的正文字数必须小于 300 字。
6. 按时间顺序输出区间。`

// asrParagraphRange LLM 返回的段落边界（utterance 闭区间下标）。
type asrParagraphRange struct {
	StartIndex int `json:"start_index"`
	EndIndex   int `json:"end_index"`
}

// asrPostprocessResult ASR 后处理产出。
type asrPostprocessResult struct {
	Summaries  []model.ASRSummarySegment
	Paragraphs []model.ASRParagraph
}

// runASRPostprocess 调用 LLM 生成 summaries 与 paragraphs；任一步失败返回 error。
func runASRPostprocess(ctx context.Context, llmClient LLMChatClient, liveASR string, durationMs int64) (asrPostprocessResult, error) {
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

	summaries, err := generateASRSummaries(ctx, llmClient, utterances, durationMs)
	if err != nil {
		return out, fmt.Errorf("生成 asr_summaries 失败: %w", err)
	}
	paragraphs, err := generateASRParagraphs(ctx, llmClient, utterances)
	if err != nil {
		return out, fmt.Errorf("生成 asr_paragraphs 失败: %w", err)
	}
	out.Summaries = summaries
	out.Paragraphs = paragraphs
	return out, nil
}

func generateASRSummaries(ctx context.Context, llmClient LLMChatClient, utterances []asr.Utterance, durationMs int64) ([]model.ASRSummarySegment, error) {
	windows := splitUtterancesByDuration(utterances, asrSummaryWindowMs, asrPostprocessMaxInputRunes)
	var all []model.ASRSummarySegment
	for _, win := range windows {
		content, err := llmClient.Chat(ctx, []llm.ChatMessage{
			{Role: "system", Content: asrSummariesSystemPrompt},
			{Role: "user", Content: buildASRSummariesUserPrompt(win.Utterances, win.Offset, durationMs)},
		})
		if err != nil {
			return nil, err
		}
		segs, err := parseASRSummaries(content)
		if err != nil {
			return nil, err
		}
		all = append(all, segs...)
	}
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

func generateASRParagraphs(ctx context.Context, llmClient LLMChatClient, utterances []asr.Utterance) ([]model.ASRParagraph, error) {
	windows := splitUtterancesByDuration(utterances, asrParagraphWindowMs, asrPostprocessMaxInputRunes)
	var ranges []asrParagraphRange
	for _, win := range windows {
		content, err := llmClient.Chat(ctx, []llm.ChatMessage{
			{Role: "system", Content: asrParagraphsSystemPrompt},
			{Role: "user", Content: buildASRParagraphsUserPrompt(win.Utterances)},
		})
		if err != nil {
			return nil, err
		}
		local, err := parseASRParagraphRanges(content)
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

type utteranceWindow struct {
	Offset     int
	Utterances []asr.Utterance
}

// splitUtterancesByDuration 按时长窗口切分；单窗输入过长时再按条数收缩。
func splitUtterancesByDuration(utterances []asr.Utterance, windowMs int64, maxRunes int) []utteranceWindow {
	if len(utterances) == 0 {
		return nil
	}
	full := formatASRTranscriptLines(utterances, 0)
	if utf8.RuneCountInString(full) <= maxRunes {
		return []utteranceWindow{{Offset: 0, Utterances: utterances}}
	}

	var windows []utteranceWindow
	start := 0
	for start < len(utterances) {
		end := start
		windowStart := utterances[start].StartTime
		for end+1 < len(utterances) {
			next := utterances[end+1]
			if next.EndTime-windowStart > windowMs {
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
	b.WriteString("ASR 句段列表：\n")
	b.WriteString(formatASRTranscriptLines(utterances, indexOffset))
	b.WriteString("\n请输出 JSON 数组。")
	return b.String()
}

func buildASRParagraphsUserPrompt(utterances []asr.Utterance) string {
	var b strings.Builder
	b.WriteString("ASR 句段列表（本窗局部编号从 0 开始）：\n")
	b.WriteString(formatASRTranscriptLines(utterances, 0))
	b.WriteString("\n请输出覆盖本窗全部句段的 JSON 数组。")
	return b.String()
}

func parseASRSummaries(content string) ([]model.ASRSummarySegment, error) {
	raw, err := extractLLMJSONArray(content)
	if err != nil {
		return nil, err
	}
	var segs []model.ASRSummarySegment
	if err := json.Unmarshal(raw, &segs); err != nil {
		return nil, fmt.Errorf("解析 asr_summaries JSON 失败: %w", err)
	}
	return segs, nil
}

func parseASRParagraphRanges(content string) ([]asrParagraphRange, error) {
	raw, err := extractLLMJSONArray(content)
	if err != nil {
		return nil, err
	}
	var ranges []asrParagraphRange
	if err := json.Unmarshal(raw, &ranges); err != nil {
		return nil, fmt.Errorf("解析 asr_paragraphs 边界 JSON 失败: %w", err)
	}
	return ranges, nil
}

func extractLLMJSONArray(content string) (json.RawMessage, error) {
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
	if json.Valid([]byte(content)) && strings.HasPrefix(content, "[") {
		return json.RawMessage(content), nil
	}
	extracted := extractJSONArrayLiteral(content)
	if extracted == "" || !json.Valid([]byte(extracted)) {
		return nil, fmt.Errorf("无法解析 LLM 返回的 JSON 数组: %s", truncateRunes(content, 256))
	}
	return json.RawMessage(extracted), nil
}

func extractJSONArrayLiteral(s string) string {
	start := strings.Index(s, "[")
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
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
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
		if utf8.RuneCountInString(title) > asrSummaryTitleMaxRunes {
			return fmt.Errorf("asr_summaries[%d] title 超过 %d 字", i, asrSummaryTitleMaxRunes)
		}
		if utf8.RuneCountInString(summary) > asrSummaryTextMaxRunes {
			return fmt.Errorf("asr_summaries[%d] summary 超过 %d 字", i, asrSummaryTextMaxRunes)
		}
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
