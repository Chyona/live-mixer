// ASR / 跟播本地验证工具（backend/cmd/test）。
//
// 模式：
//
//	paragraphs  — 已有：从 live_asr JSON 只跑 asr_paragraphs
//	live-window — 新增：拉取跟播 m3u8 分片，按当前窗口 ASR 逻辑转写并输出 JSON
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	mode := flag.String("mode", "", "paragraphs | live-window；空则：指定 -url 时用 live-window，否则 paragraphs")
	configPath := flag.String("config", "", "外部配置文件路径（可选）")
	envFile := flag.String("env", "", "可选 .env 路径（默认尝试 docker/.env）")

	// paragraphs
	asrPath := flag.String("asr", "", "live_asr JSON 路径（paragraphs 模式；默认仓库根 asr_raw.json）")
	outPath := flag.String("out", "", "输出 JSON 路径（paragraphs 默认 asr_paragraphs.json；live-window 默认 live_window_asr.json）")
	reportPath := flag.String("report", "", "paragraphs：可选含 LLM 提示词的文本报告")

	// live-window
	m3u8URL := flag.String("url", "", "跟播 live.m3u8 地址（live-window）")
	workDir := flag.String("workdir", "", "本地工作目录（默认 ./tmp/live-window-asr）")
	maxWindows := flag.Int("max-windows", 2, "最多跑几窗 ASR（控制费用；0=追平全部已下载分片）")
	maxSegs := flag.Int("max-segs", 0, "最多下载分片数（0=按 max-windows 自动估算）")
	skipASR := flag.Bool("skip-asr", false, "只下载/探测/规划窗口，不调用 ASR（不产生费用）")
	flag.Parse()

	repoRoot := findRepoRoot()
	if err := loadDotEnv(firstNonEmpty(*envFile, filepath.Join(repoRoot, "docker", ".env"))); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 加载 .env 失败: %v\n", err)
	}

	resolved := strings.TrimSpace(*mode)
	if resolved == "" {
		if strings.TrimSpace(*m3u8URL) != "" {
			resolved = "live-window"
		} else {
			resolved = "paragraphs"
		}
	}

	switch resolved {
	case "paragraphs":
		out := strings.TrimSpace(*outPath)
		if out == "" {
			out = "asr_paragraphs.json"
		}
		runParagraphsMode(paragraphsArgs{
			ConfigPath: *configPath,
			ASRPath:    *asrPath,
			OutPath:    out,
			ReportPath: *reportPath,
			RepoRoot:   repoRoot,
		})
	case "live-window":
		out := strings.TrimSpace(*outPath)
		if out == "" {
			out = "live_window_asr.json"
		}
		wd := strings.TrimSpace(*workDir)
		if wd == "" {
			wd = filepath.Join(repoRoot, "tmp", "live-window-asr")
		}
		url := strings.TrimSpace(*m3u8URL)
		if url == "" {
			url = defaultLiveWindowTestURL
		}
		runLiveWindowMode(liveWindowArgs{
			ConfigPath: *configPath,
			URL:        url,
			WorkDir:    wd,
			OutPath:    out,
			MaxWindows: *maxWindows,
			MaxSegs:    *maxSegs,
			SkipASR:    *skipASR,
			RepoRoot:   repoRoot,
		})
	default:
		fmt.Fprintf(os.Stderr, "未知 mode=%q，请用 paragraphs 或 live-window\n", resolved)
		os.Exit(2)
	}
}

// 用户提供的跟播列表，便于本地复现。
const defaultLiveWindowTestURL = "https://live-mixer-1426793176.cos.ap-shanghai.myqcloud.com/video_editing/live-record/1e84428cac6e4dd1ac24b8bdcec0a3c8/live.m3u8"
