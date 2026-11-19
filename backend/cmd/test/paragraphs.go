package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/service"

	"go.uber.org/zap"
)

type paragraphsArgs struct {
	ASRPath    string
	OutPath    string
	ReportPath string
	RepoRoot   string
}

func runParagraphsMode(a paragraphsArgs) {
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
	fmt.Fprintf(os.Stderr, "ASR 文件: %s\n", path)
	fmt.Fprintf(os.Stderr, "时长: %dms  句段数: %d\n", durationMs, len(utterances))
	fmt.Fprintln(os.Stderr, "开始计算 asr_paragraphs（MinGap 算法，无 LLM）...")

	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	ctx := context.Background()
	started := time.Now()
	paragraphs, err := service.RunASRParagraphs(ctx, liveASR, durationMs, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "asr_paragraphs 计算失败: %v\n", err)
		os.Exit(1)
	}
	elapsed := time.Since(started).Round(time.Millisecond)
	fmt.Fprintf(os.Stderr, "完成: paragraphs=%d 耗时=%s\n", len(paragraphs), elapsed)

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
	fmt.Fprintln(os.Stderr, "时间线校验通过（段级 start<end；words 允许 -1）")

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
		text := formatParagraphReport(path, durationMs, len(utterances), elapsed, paragraphs)
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

func formatParagraphReport(
	asrPath string,
	durationMs int64,
	utteranceCount int,
	elapsed time.Duration,
	paragraphs []model.ASRParagraph,
) string {
	var b strings.Builder
	sep := strings.Repeat("=", 72)

	fmt.Fprintf(&b, "%s\n", sep)
	b.WriteString("asr_paragraphs 专项测试报告（MinGap 算法）\n")
	fmt.Fprintf(&b, "%s\n", sep)
	fmt.Fprintf(&b, "ASR 文件: %s\n", asrPath)
	fmt.Fprintf(&b, "时长(ms): %d\n", durationMs)
	fmt.Fprintf(&b, "句段数: %d\n", utteranceCount)
	fmt.Fprintf(&b, "段落数: %d\n", len(paragraphs))
	fmt.Fprintf(&b, "耗时: %s\n", elapsed)
	fmt.Fprintf(&b, "生成时间: %s\n", time.Now().Format(time.RFC3339))
	b.WriteString("校验: 通过（段级 start<end；words 允许 -1）\n\n")

	b.WriteString(sep + "\n")
	b.WriteString("asr_paragraphs 摘要（完整值见 JSON 文件）\n")
	b.WriteString(sep + "\n\n")
	if len(paragraphs) == 0 {
		b.WriteString("（空）\n\n")
	}
	for i, p := range paragraphs {
		fmt.Fprintf(&b, "[%d] speaker=%s  start_time=%d  end_time=%d  duration_ms=%d  runes=%d  words=%d\n",
			i+1, p.Speaker, p.StartTime, p.EndTime, p.EndTime-p.StartTime, utf8.RuneCountInString(p.Text), len(p.Words))
		b.WriteString(p.Text)
		if !strings.HasSuffix(p.Text, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// checkParagraphTimeline 与线上一致：允许 words 的 -1；仅检查段级与有效词颠倒。
func checkParagraphTimeline(paragraphs []model.ASRParagraph) []string {
	var issues []string
	for i, p := range paragraphs {
		if p.StartTime >= p.EndTime {
			issues = append(issues, fmt.Sprintf("[%d] start_time(%d) >= end_time(%d)", i+1, p.StartTime, p.EndTime))
		}
		for j, w := range p.Words {
			if w.StartTime >= 0 && w.EndTime >= 0 && w.EndTime < w.StartTime {
				issues = append(issues, fmt.Sprintf("[%d].words[%d] 时间颠倒 start=%d end=%d",
					i+1, j, w.StartTime, w.EndTime))
			}
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
