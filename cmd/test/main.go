// ASR paragraphs 专项测试工具：读取 live_asr JSON（默认 asr_raw.json），
// 跳过 ASR 识别与 asr_summaries，仅跑 worker 同款 asr_paragraphs 计算；
// 将 live_material.asr_paragraphs 的完整字段值写入 JSON 文件。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func main() {
	configPath := flag.String("config", "", "外部配置文件路径（可选；否则用内嵌 config + 环境变量）")
	asrPath := flag.String("asr", "", "live_asr JSON 路径（默认仓库根目录 asr_raw.json）")
	outPath := flag.String("out", "asr_paragraphs.json", "asr_paragraphs 完整 JSON 输出路径（默认 asr_paragraphs.json）")
	reportPath := flag.String("report", "", "可选：额外写入含 LLM 提示词的文本报告路径")
	envFile := flag.String("env", "", "可选 .env 路径（默认尝试 docker/.env），用于注入 APP_LLM_*")
	flag.Parse()

	repoRoot := findRepoRoot()
	if err := loadDotEnv(firstNonEmpty(*envFile, filepath.Join(repoRoot, "docker", ".env"))); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 加载 .env 失败: %v\n", err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}
	if cfg.LLM.APIKey == "" {
		fmt.Fprintln(os.Stderr, "LLM API Key 未配置，请设置 APP_LLM_API_KEY 或在配置文件中填写 llm.api_key")
		os.Exit(1)
	}

	path := *asrPath
	if path == "" {
		path = filepath.Join(repoRoot, "asr_raw.json")
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

	out := strings.TrimSpace(*outPath)
	if out == "" {
		out = "asr_paragraphs.json"
	}
	if err := os.WriteFile(out, jsonBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 JSON 失败: %s: %v\n", out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已写入 live_material.asr_paragraphs 完整值: %s (%d bytes, %d 段)\n",
		out, len(jsonBytes), len(paragraphs))

	if report := strings.TrimSpace(*reportPath); report != "" {
		text := formatParagraphReport(path, modelName, durationMs, len(utterances), elapsed, capture.Calls(), paragraphs)
		if err := os.WriteFile(report, []byte(text), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "写入报告失败: %s: %v\n", report, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "已写入调试报告: %s\n", report)
	}
}

// asrParagraphDTO 与 live_material.asr_paragraphs 元素一致；words 始终输出（含空数组）。
type asrParagraphDTO struct {
	Speaker   string           `json:"speaker"`
	Text      string           `json:"text"`
	StartTime int64            `json:"start_time"`
	EndTime   int64            `json:"end_time"`
	Words     []model.ClipWord `json:"words"`
}

// marshalASRParagraphsField 序列化为入库字段 live_material.asr_paragraphs 的完整 JSON 数组。
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

// captureLLM 包装真实 LLM，记录每次 ChatStructured 的完整提示词与响应。
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

// checkParagraphTimeline 复核段落时间线硬约束（与 service 校验一致，供 CLI 二次确认）。
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

// parseASRInput 支持包装格式 {"duration_ms":N,"live_asr":{...}}，
// 以及旧格式（直接豆包 ASR 根对象，含 result.utterances）。
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

func findRepoRoot() string {
	_, self, _, ok := runtime.Caller(0)
	if ok {
		root := filepath.Clean(filepath.Join(filepath.Dir(self), "..", ".."))
		if fileExists(filepath.Join(root, "go.mod")) {
			return root
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func loadDotEnv(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return sc.Err()
}
