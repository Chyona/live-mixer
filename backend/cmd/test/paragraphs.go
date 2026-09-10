package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"live-mixer/internal/config"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/service"

	"go.uber.org/zap"
)

type paragraphsArgs struct {
	ConfigPath string
	ASRPath    string
	OutPath    string
	ReportPath string
	RepoRoot   string
}

func runParagraphsMode(a paragraphsArgs) {
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	if cfg.LLM.APIKey == "" {
		fmt.Fprintln(os.Stderr, "LLM API Key 未配置，请设置 APP_LLM_API_KEY 或在配置文件中填写 llm.api_key")
		os.Exit(1)
	}

	path := a.ASRPath
	if path == "" {
		path = filepath.Join(a.RepoRoot, "asr_raw.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 ASR 文件失败: %s: %v\n", path, err)
		os.Exit(1)
	}

	liveASR, durationMs, err := parseASRInput(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 ASR 输入失败: %v\n", err)
		os.Exit(1)
	}

	utterances := asr.FormatUtterancesForAPI(liveASR)
	modelName := cfg.LLM.FlashModelOrDefault()
	fmt.Fprintf(os.Stderr, "ASR 文件: %s\n", path)
	fmt.Fprintf(os.Stderr, "时长: %dms  句段数: %d  模型: %s (flash)\n", durationMs, len(utterances), modelName)
	fmt.Fprintln(os.Stderr, "开始计算 asr_paragraphs（跳过 ASR 识别与 asr_summaries）...")

	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	capture := &captureLLM{inner: llm.NewClient(cfg.LLM.LLMClientConfigForASR())}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	started := time.Now()
	paragraphs, err := service.RunASRParagraphs(ctx, capture, liveASR, durationMs, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "asr_paragraphs 计算失败: %v\n", err)
		os.Exit(1)
	}
	elapsed := time.Since(started).Round(time.Millisecond)
	fmt.Fprintf(os.Stderr, "完成: paragraphs=%d llm_calls=%d 耗时=%s\n",
		len(paragraphs), len(capture.Calls()), elapsed)

	if paragraphs == nil {
		paragraphs = []model.ASRParagraph{}
	}
	if issues := checkParagraphTimeline(paragraphs); len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "时间线校验失败 (%d):\n", len(issues))
		for _, issue := range issues {
			fmt.Fprintf(os.Stderr, "  - %s\n", issue)
		}
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "时间线校验通过（无重叠、start<end、words 无非法时间）")

	jsonBytes, err := marshalASRParagraphsField(paragraphs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化 asr_paragraphs 失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(a.OutPath, jsonBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 JSON 失败: %s: %v\n", a.OutPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已写入 live_material.asr_paragraphs 完整值: %s (%d bytes, %d 段)\n",
		a.OutPath, len(jsonBytes), len(paragraphs))

	if report := strings.TrimSpace(a.ReportPath); report != "" {
		text := formatParagraphReport(path, modelName, durationMs, len(utterances), elapsed, capture.Calls(), paragraphs)
		if err := os.WriteFile(report, []byte(text), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入报告失败: %s: %v\n", report, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "已写入调试报告: %s\n", report)
	}
}

type asrParagraphDTO struct {
	Speaker   string           `json:"speaker"`
	Text      string           `json:"text"`
	StartTime int64            `json:"start_time"`
	EndTime   int64            `json:"end_time"`
	Words     []model.ClipWord `json:"words"`
}

func marshalASRParagraphsField(paragraphs []model.ASRParagraph) ([]byte, error) {
	out := make([]asrParagraphDTO, 0, len(paragraphs))
	for _, p := range paragraphs {
		words := p.Words
		if words == nil {
			words = []model.ClipWord{}
		}
		out = append(out, asrParagraphDTO{
			Speaker:   p.Speaker,
			Text:      p.Text,
			StartTime: p.StartTime,
			EndTime:   p.EndTime,
			Words:     words,
		})
	}
	payload, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

type llmCall struct {
	Messages []llm.ChatMessage
	Response string
	Err      error
}

type captureLLM struct {
	inner *llm.Client
	mu    sync.Mutex
	calls []llmCall
}

func (c *captureLLM) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return c.ChatStructured(ctx, messages)
}

func (c *captureLLM) ChatStructured(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	resp, err := c.inner.ChatThinking(ctx, messages)
	copied := make([]llm.ChatMessage, len(messages))
	copy(copied, messages)
	c.mu.Lock()
	c.calls = append(c.calls, llmCall{Messages: copied, Response: resp, Err: err})
	c.mu.Unlock()
	return resp, err
}

func (c *captureLLM) ChatThinking(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return c.inner.ChatThinking(ctx, messages)
}

func (c *captureLLM) Calls() []llmCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]llmCall, len(c.calls))
	copy(out, c.calls)
	return out
}

func formatParagraphReport(
	asrPath, modelName string,
	durationMs int64,
	utteranceCount int,
	elapsed time.Duration,
	calls []llmCall,
	paragraphs []model.ASRParagraph,
) string {
	var b strings.Builder
	sep := strings.Repeat("=", 72)
	sub := strings.Repeat("-", 72)

	fmt.Fprintf(&b, "%s\n", sep)
	b.WriteString("asr_paragraphs 专项测试报告\n")
	fmt.Fprintf(&b, "%s\n", sep)
	fmt.Fprintf(&b, "ASR 文件: %s\n", asrPath)
	fmt.Fprintf(&b, "模型: %s\n", modelName)
	fmt.Fprintf(&b, "时长(ms): %d\n", durationMs)
	fmt.Fprintf(&b, "句段数: %d\n", utteranceCount)
	fmt.Fprintf(&b, "段落数: %d\n", len(paragraphs))
	fmt.Fprintf(&b, "LLM 调用次数: %d\n", len(calls))
	fmt.Fprintf(&b, "耗时: %s\n", elapsed)
	fmt.Fprintf(&b, "生成时间: %s\n", time.Now().Format(time.RFC3339))
	b.WriteString("校验: 通过（无重叠 / start<end / words 时间合法）\n\n")

	b.WriteString(sep + "\n")
	b.WriteString("一、LLM 提示词与响应（asr_paragraphs）\n")
	b.WriteString(sep + "\n\n")
	if len(calls) == 0 {
		b.WriteString("（无 LLM 调用记录）\n\n")
	}
	for i, call := range calls {
		fmt.Fprintf(&b, "%s\n", sub)
		fmt.Fprintf(&b, "【调用 #%d】\n", i+1)
		fmt.Fprintf(&b, "%s\n", sub)
		for _, msg := range call.Messages {
			fmt.Fprintf(&b, "\n----- %s -----\n", strings.ToUpper(msg.Role))
			b.WriteString(msg.Content)
			if !strings.HasSuffix(msg.Content, "\n") {
				b.WriteByte('\n')
			}
		}
		b.WriteString("\n----- MODEL RAW RESPONSE -----\n")
		if call.Err != nil {
			fmt.Fprintf(&b, "ERROR: %v\n", call.Err)
		}
		if call.Response != "" {
			b.WriteString(call.Response)
			if !strings.HasSuffix(call.Response, "\n") {
				b.WriteByte('\n')
			}
		} else if call.Err == nil {
			b.WriteString("（空响应）\n")
		}
		b.WriteByte('\n')
	}

	b.WriteString(sep + "\n")
	b.WriteString("二、asr_paragraphs 摘要（完整值见 JSON 文件）\n")
	b.WriteString(sep + "\n\n")
	if len(paragraphs) == 0 {
		b.WriteString("（空）\n\n")
	}
	for i, p := range paragraphs {
		fmt.Fprintf(&b, "[%d] speaker=%s  start_time=%d  end_time=%d  duration_ms=%d  words=%d\n",
			i+1, p.Speaker, p.StartTime, p.EndTime, p.EndTime-p.StartTime, len(p.Words))
		b.WriteString(p.Text)
		if !strings.HasSuffix(p.Text, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func checkParagraphTimeline(paragraphs []model.ASRParagraph) []string {
	var issues []string
	for i, p := range paragraphs {
		if p.StartTime >= p.EndTime {
			issues = append(issues, fmt.Sprintf("[%d] start_time(%d) >= end_time(%d)", i+1, p.StartTime, p.EndTime))
		}
		for j, w := range p.Words {
			if w.StartTime < 0 || w.EndTime < 0 {
				issues = append(issues, fmt.Sprintf("[%d].words[%d] 非法时间 start=%d end=%d text=%q",
					i+1, j, w.StartTime, w.EndTime, w.Text))
			} else if w.EndTime < w.StartTime {
				issues = append(issues, fmt.Sprintf("[%d].words[%d] 时间颠倒 start=%d end=%d",
					i+1, j, w.StartTime, w.EndTime))
			}
		}
		if i > 0 && p.StartTime < paragraphs[i-1].EndTime {
			issues = append(issues, fmt.Sprintf("[%d] 与上一段重叠: start=%d < prev.end=%d",
				i+1, p.StartTime, paragraphs[i-1].EndTime))
		}
	}
	return issues
}

func parseASRInput(raw []byte) (liveASR string, durationMs int64, err error) {
	if !json.Valid(raw) {
		return "", 0, fmt.Errorf("不是合法 JSON")
	}
	var wrapped struct {
		DurationMs int64           `json:"duration_ms"`
		LiveASR    json.RawMessage `json:"live_asr"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.LiveASR) > 0 && json.Valid(wrapped.LiveASR) {
		liveASR = string(wrapped.LiveASR)
		durationMs = wrapped.DurationMs
		if durationMs <= 0 {
			durationMs = asr.ParseDurationMs(wrapped.LiveASR)
		}
		if len(asr.FormatUtterancesForAPI(liveASR)) == 0 {
			return "", 0, fmt.Errorf("live_asr 中无有效句段")
		}
		return liveASR, durationMs, nil
	}
	liveASR = string(raw)
	durationMs = asr.ParseDurationMs(raw)
	if len(asr.FormatUtterancesForAPI(liveASR)) == 0 {
		return "", 0, fmt.Errorf("未识别到 asr_raw 包装格式，且根对象无有效句段")
	}
	return liveASR, durationMs, nil
}
