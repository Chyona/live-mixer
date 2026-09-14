// asr_paragraphs 算法离线验证（不依赖 LLM / config）。
//
//	go run ./cmd/test2 -asr path/to/live_asr.json [-maxlen 200] [-out out.json] [-report report.txt] [-compare old.json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/service"

	"go.uber.org/zap"
)

func main() {
	asrPath := flag.String("asr", "", "live_asr JSON 路径（默认仓库根 asr_raw.json）")
	maxlen := flag.Int("maxlen", 200, "合并段落最大 Unicode 字数")
	outPath := flag.String("out", "tmp/paragraphs_algo.json", "输出 asr_paragraphs JSON")
	reportPath := flag.String("report", "", "文本报告路径（默认与 -out 同目录的 *_report.txt）")
	comparePath := flag.String("compare", "", "可选：与旧 LLM asr_paragraphs JSON 对比段数/均长")
	flag.Parse()

	repoRoot := findRepoRoot()
	path := strings.TrimSpace(*asrPath)
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
	fmt.Fprintf(os.Stderr, "ASR 文件: %s\n", path)
	fmt.Fprintf(os.Stderr, "时长: %dms  句段数: %d  maxlen: %d\n", durationMs, len(utterances), *maxlen)

	logger, err := zap.NewDevelopment()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	started := time.Now()
	paragraphs, warnings := service.BuildASRParagraphsByMinGap(utterances, *maxlen, logger)
	elapsed := time.Since(started).Round(time.Millisecond)

	if paragraphs == nil {
		paragraphs = []model.ASRParagraph{}
	}

	// 仅同说话人句级 gap<0 为异常；words 中 -1（空格等）为源 ASR 无效时间，1:1 保留，不告警。
	printOverlapAlerts(warnings)

	mergeCount := len(utterances) - len(paragraphs)
	if mergeCount < 0 {
		mergeCount = 0
	}
	fmt.Fprintf(os.Stderr, "完成: utterances=%d → paragraphs=%d  merges≈%d  耗时=%s  same_speaker_overlaps=%d\n",
		len(utterances), len(paragraphs), mergeCount, elapsed, len(warnings))

	issues := checkParagraphTimeline(paragraphs)
	if len(issues) > 0 {
		fmt.Fprintf(os.Stderr, "段级时间线问题 (%d，仍写出结果):\n", len(issues))
		limit := len(issues)
		if limit > 20 {
			limit = 20
		}
		for _, issue := range issues[:limit] {
			fmt.Fprintf(os.Stderr, "  - %s\n", issue)
		}
		if len(issues) > limit {
			fmt.Fprintf(os.Stderr, "  ... 另有 %d 条省略\n", len(issues)-limit)
		}
	} else {
		fmt.Fprintln(os.Stderr, "段级时间线校验通过（start<end；words 允许 -1 无效值）")
	}

	runeStats := collectRuneStats(paragraphs)
	if runeStats.max > *maxlen {
		fmt.Fprintf(os.Stderr, "警告: 存在段落 runes=%d > maxlen=%d\n", runeStats.max, *maxlen)
	}

	gaps := collectNonNegAdjacentGaps(utterances)
	fmt.Fprintf(os.Stderr, "段落字数: min=%d avg=%.1f max=%d\n", runeStats.min, runeStats.avg, runeStats.max)
	if len(gaps) > 0 {
		fmt.Fprintf(os.Stderr, "输入相邻非负 gap(ms): n=%d min=%d p50=%d max=%d\n",
			len(gaps), gaps[0], gaps[len(gaps)/2], gaps[len(gaps)-1])
	}

	out := strings.TrimSpace(*outPath)
	if out == "" {
		out = "tmp/paragraphs_algo.json"
	}
	jsonBytes, err := marshalASRParagraphsField(paragraphs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化失败: %v\n", err)
		os.Exit(1)
	}
	if err := ensureParentDir(out); err != nil {
		fmt.Fprintf(os.Stderr, "创建输出目录失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, jsonBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 JSON 失败: %s: %v\n", out, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已写入结果: %s (%d bytes, %d 段)\n", out, len(jsonBytes), len(paragraphs))

	var compareNote string
	if cp := strings.TrimSpace(*comparePath); cp != "" {
		compareNote = compareWithOld(cp, paragraphs)
		fmt.Fprintln(os.Stderr, compareNote)
	}

	report := strings.TrimSpace(*reportPath)
	if report == "" {
		report = strings.TrimSuffix(out, filepath.Ext(out)) + "_report.txt"
	}
	text := formatAlgoReport(path, durationMs, *maxlen, len(utterances), paragraphs, warnings, gaps, runeStats, elapsed, compareNote, issues)
	if err := ensureParentDir(report); err != nil {
		fmt.Fprintf(os.Stderr, "创建报告目录失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(report, []byte(text), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入报告失败: %s: %v\n", report, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "已写入报告: %s\n", report)
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func printOverlapAlerts(warnings []service.SameSpeakerOverlapWarning) {
	if len(warnings) == 0 {
		return
	}
	banner := strings.Repeat("!", 72)
	fmt.Fprintln(os.Stderr, banner)
	fmt.Fprintf(os.Stderr, "【强提醒】发现 %d 处同一说话人相邻语句时间重叠（gap=start_{i+1}-end_i < 0；已跳过合并）\n", len(warnings))
	fmt.Fprintln(os.Stderr, banner)
	for i, w := range warnings {
		fmt.Fprintf(os.Stderr, "  #%d speaker=%s idx=%d→%d left=[%d,%d) right=[%d,%d) gap_ms=%d\n",
			i+1, w.Speaker, w.LeftIndex, w.RightIndex, w.LeftStart, w.LeftEnd, w.RightStart, w.RightEnd, w.GapMs)
	}
	fmt.Fprintln(os.Stderr, banner)
}

type runeStats struct {
	min, max int
	avg      float64
}

func collectRuneStats(paragraphs []model.ASRParagraph) runeStats {
	if len(paragraphs) == 0 {
		return runeStats{}
	}
	minR, maxR := math.MaxInt, 0
	sum := 0
	for _, p := range paragraphs {
		n := utf8.RuneCountInString(p.Text)
		sum += n
		if n < minR {
			minR = n
		}
		if n > maxR {
			maxR = n
		}
	}
	return runeStats{min: minR, max: maxR, avg: float64(sum) / float64(len(paragraphs))}
}

func collectNonNegAdjacentGaps(utterances []asr.Utterance) []int64 {
	gaps := make([]int64, 0)
	for i := 0; i < len(utterances)-1; i++ {
		gap := utterances[i+1].StartTime - utterances[i].EndTime
		if gap >= 0 {
			gaps = append(gaps, gap)
		}
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	return gaps
}

func compareWithOld(path string, paras []model.ASRParagraph) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("对比失败: 读取 %s: %v", path, err)
	}
	var old []model.ASRParagraph
	if err := json.Unmarshal(raw, &old); err != nil {
		return fmt.Sprintf("对比失败: 解析 %s: %v", path, err)
	}
	oldStats := collectRuneStats(old)
	newStats := collectRuneStats(paras)
	return fmt.Sprintf("对比 %s: old_segs=%d avg_runes=%.1f | algo_segs=%d avg_runes=%.1f",
		path, len(old), oldStats.avg, len(paras), newStats.avg)
}

func formatAlgoReport(
	asrPath string,
	durationMs int64,
	maxlen, utteranceCount int,
	paragraphs []model.ASRParagraph,
	warnings []service.SameSpeakerOverlapWarning,
	gaps []int64,
	stats runeStats,
	elapsed time.Duration,
	compareNote string,
	timelineIssues []string,
) string {
	var b strings.Builder
	sep := strings.Repeat("=", 72)
	sub := strings.Repeat("-", 72)

	fmt.Fprintf(&b, "%s\n", sep)
	b.WriteString("asr_paragraphs 算法验证报告 (BuildASRParagraphsByMinGap)\n")
	fmt.Fprintf(&b, "%s\n", sep)

	if len(warnings) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Repeat("!", 72) + "\n")
		fmt.Fprintf(&b, "【强提醒】同一说话人相邻句 gap=start_{i+1}-end_i < 0，共 %d 处（已跳过合并）\n", len(warnings))
		b.WriteString(strings.Repeat("!", 72) + "\n")
		for i, w := range warnings {
			fmt.Fprintf(&b, "  #%d speaker=%s idx=%d→%d left=[%d,%d) right=[%d,%d) gap_ms=%d\n",
				i+1, w.Speaker, w.LeftIndex, w.RightIndex, w.LeftStart, w.LeftEnd, w.RightStart, w.RightEnd, w.GapMs)
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "ASR 文件: %s\n", asrPath)
	fmt.Fprintf(&b, "时长(ms): %d\n", durationMs)
	fmt.Fprintf(&b, "maxlen: %d\n", maxlen)
	fmt.Fprintf(&b, "句段数: %d\n", utteranceCount)
	fmt.Fprintf(&b, "段落数: %d\n", len(paragraphs))
	fmt.Fprintf(&b, "合并次数(约): %d\n", maxInt(0, utteranceCount-len(paragraphs)))
	fmt.Fprintf(&b, "同说话人句级重叠告警: %d\n", len(warnings))
	fmt.Fprintf(&b, "耗时: %s\n", elapsed)
	fmt.Fprintf(&b, "生成时间: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "段落字数: min=%d avg=%.1f max=%d\n", stats.min, stats.avg, stats.max)
	if len(gaps) > 0 {
		fmt.Fprintf(&b, "输入相邻非负 gap(ms): n=%d min=%d p50=%d max=%d\n",
			len(gaps), gaps[0], gaps[len(gaps)/2], gaps[len(gaps)-1])
	}
	b.WriteString("说明: words 1:1 自 live_asr 拷贝；空格等 start/end=-1 为无效时间，正常不告警。\n")
	b.WriteString("异常条件: 仅同说话人 (utterances[i+1].start_time - utterances[i].end_time) < 0。\n")
	if len(timelineIssues) == 0 {
		b.WriteString("段级校验: 通过（start<end；有效 words 未颠倒）\n")
	} else {
		fmt.Fprintf(&b, "段级校验: %d 条问题\n", len(timelineIssues))
		for _, issue := range timelineIssues {
			fmt.Fprintf(&b, "  - %s\n", issue)
		}
	}
	if compareNote != "" {
		fmt.Fprintf(&b, "%s\n", compareNote)
	}
	b.WriteByte('\n')

	b.WriteString(sep + "\n")
	b.WriteString("asr_paragraphs 摘要\n")
	b.WriteString(sep + "\n\n")
	if len(paragraphs) == 0 {
		b.WriteString("（空）\n\n")
	}
	for i, p := range paragraphs {
		fmt.Fprintf(&b, "%s\n", sub)
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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

// checkParagraphTimeline 段级硬约束；words 允许 -1（与源 ASR / 线上 validateASRParagraphTimeline 一致）。
// 不把词级 -1、相邻段时间交错当作异常——句级同说话人重叠由 BuildASRParagraphsByMinGap 单独告警。
func checkParagraphTimeline(paragraphs []model.ASRParagraph) []string {
	var issues []string
	for i, p := range paragraphs {
		if p.StartTime >= p.EndTime {
			issues = append(issues, fmt.Sprintf("[%d] start_time(%d) >= end_time(%d)", i+1, p.StartTime, p.EndTime))
		}
		for j, w := range p.Words {
			if w.StartTime >= 0 && w.EndTime >= 0 && w.EndTime < w.StartTime {
				issues = append(issues, fmt.Sprintf("[%d].words[%d] 时间颠倒 start=%d end=%d text=%q",
					i+1, j, w.StartTime, w.EndTime, w.Text))
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

func findRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// backend/go.mod → 仓库根为上一级
			parent := filepath.Dir(dir)
			if filepath.Base(dir) == "backend" {
				return parent
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}
