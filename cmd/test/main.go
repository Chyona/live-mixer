// ASR 分段测试工具：读取 asr_raw.json（duration_ms + live_asr），调用仓库内 ASR 后处理，
// 以纯文本输出完整提示词、asr_summaries 与 asr_paragraphs。
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
)

func main() {
	configPath := flag.String("config", "", "外部配置文件路径（可选；否则用内嵌 config + 环境变量）")
	asrPath := flag.String("asr", "", "ASR 输入 JSON 路径（默认仓库根目录 asr_raw.json）")
	outPath := flag.String("out", "", "结果文本输出路径（默认 stdout）")
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
	asrModel := cfg.LLM.FlashModelOrDefault()
	fmt.Fprintf(os.Stderr, "ASR 文件: %s\n", path)
	fmt.Fprintf(os.Stderr, "时长: %dms  句段数: %d  模型: %s (flash)\n", durationMs, len(utterances), asrModel)
	fmt.Fprintln(os.Stderr, "开始调用 ASR 后处理（summaries + paragraphs，深度思考）...")

	capture := &captureLLM{inner: llm.NewClient(cfg.LLM.LLMClientConfigForASR())}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	started := time.Now()
	summaries, paragraphs, err := service.RunASRPostprocess(ctx, capture, liveASR, durationMs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ASR 后处理失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "完成: summaries=%d paragraphs=%d llm_calls=%d 耗时=%s\n",
		len(summaries), len(paragraphs), len(capture.Calls()), time.Since(started).Round(time.Millisecond))

	text := formatTextReport(path, asrModel, durationMs, len(utterances), capture.Calls(), summaries, paragraphs)
	if *outPath == "" {
		if _, err := os.Stdout.WriteString(text); err != nil {
			fmt.Fprintf(os.Stderr, "写入 stdout 失败: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(*outPath, []byte(text), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入结果文件失败: %s: %v\n", *outPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "结果已写入: %s\n", *outPath)
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
	// 测试工具开启深度思考；仍走 ChatStructured 接口以复用现有后处理逻辑。
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

func formatTextReport(
	asrPath, model string,
	durationMs int64,
	utteranceCount int,
	calls []llmCall,
	summaries []model.ASRSummarySegment,
	paragraphs []model.ASRParagraph,
) string {
	var b strings.Builder
	sep := strings.Repeat("=", 72)
	sub := strings.Repeat("-", 72)

	fmt.Fprintf(&b, "%s\n", sep)
	b.WriteString("ASR 后处理测试报告\n")
	fmt.Fprintf(&b, "%s\n", sep)
	fmt.Fprintf(&b, "ASR 文件: %s\n", asrPath)
	fmt.Fprintf(&b, "模型: %s\n", model)
	fmt.Fprintf(&b, "时长(ms): %d\n", durationMs)
	fmt.Fprintf(&b, "句段数: %d\n", utteranceCount)
	fmt.Fprintf(&b, "LLM 调用次数: %d\n", len(calls))
	fmt.Fprintf(&b, "生成时间: %s\n\n", time.Now().Format(time.RFC3339))

	b.WriteString(sep + "\n")
	b.WriteString("一、完整提示词（每次 LLM 调用）\n")
	b.WriteString(sep + "\n\n")
	if len(calls) == 0 {
		b.WriteString("（无 LLM 调用记录）\n\n")
	}
	for i, call := range calls {
		kind := detectPromptKind(call.Messages)
		fmt.Fprintf(&b, "%s\n", sub)
		fmt.Fprintf(&b, "【调用 #%d】类型: %s\n", i+1, kind)
		fmt.Fprintf(&b, "%s\n", sub)
		for _, msg := range call.Messages {
			role := strings.ToUpper(msg.Role)
			fmt.Fprintf(&b, "\n----- %s -----\n", role)
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
	b.WriteString("二、asr_summaries\n")
	b.WriteString(sep + "\n\n")
	if len(summaries) == 0 {
		b.WriteString("（空）\n\n")
	}
	for i, s := range summaries {
		fmt.Fprintf(&b, "[%d] title=%s\n", i+1, s.Title)
		fmt.Fprintf(&b, "    start_time=%d  end_time=%d  duration_ms=%d\n\n",
			s.StartTime, s.EndTime, s.EndTime-s.StartTime)
	}

	b.WriteString(sep + "\n")
	b.WriteString("三、asr_paragraphs\n")
	b.WriteString(sep + "\n\n")
	if len(paragraphs) == 0 {
		b.WriteString("（空）\n\n")
	}
	for i, p := range paragraphs {
		fmt.Fprintf(&b, "[%d] speaker=%s  start_time=%d  end_time=%d  duration_ms=%d\n",
			i+1, p.Speaker, p.StartTime, p.EndTime, p.EndTime-p.StartTime)
		b.WriteString(p.Text)
		if !strings.HasSuffix(p.Text, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	return b.String()
}

// parseASRInput 支持新格式 asr_raw.json：{"duration_ms":N,"live_asr":{...}}；
// 也兼容旧格式（直接豆包 ASR 根对象，含 audio_info / result.utterances）。
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

	// 旧格式：整份即为 live_asr。
	liveASR = string(raw)
	durationMs = asr.ParseDurationMs(raw)
	if len(asr.FormatUtterancesForAPI(liveASR)) == 0 {
		return "", 0, fmt.Errorf("未识别到 asr_raw 包装格式，且根对象无有效句段")
	}
	return liveASR, durationMs, nil
}

func detectPromptKind(messages []llm.ChatMessage) string {
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		if strings.Contains(msg.Content, "主题提炼") {
			return "asr_summaries"
		}
		if strings.Contains(msg.Content, "段落划分") {
			return "asr_paragraphs"
		}
	}
	return "unknown"
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
