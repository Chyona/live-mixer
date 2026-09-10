// ASR / 跟播本地验证工具（backend/cmd/test）。
//
// 模式：
//
//	paragraphs   — 从 live_asr JSON 只跑 asr_paragraphs
//	live-window  — 拉取跟播 m3u8 分片，按当前窗口 ASR 逻辑转写并输出 JSON
//	add-live     — 模拟 UI 添加「正在直播」源视频，轮询直至 ASR 推进
//	one-click    — 对源视频发起一键成片（POST /v1/tasks/ai-slice-draft），轮询至完成
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	mode := flag.String("mode", "", "paragraphs | live-window | add-live | one-click")
	configPath := flag.String("config", "", "外部配置文件路径（可选；paragraphs/live-window）")
	envFile := flag.String("env", "", "可选 .env 路径（默认尝试 docker/.env）")

	// paragraphs
	asrPath := flag.String("asr", "", "live_asr JSON 路径（paragraphs 模式；默认仓库根 asr_raw.json）")
	outPath := flag.String("out", "", "输出 JSON 路径")
	reportPath := flag.String("report", "", "paragraphs：可选含 LLM 提示词的文本报告")

	// live-window
	m3u8URL := flag.String("url", "", "m3u8 地址（live-window=跟播列表；add-live=源站直播地址）")
	workDir := flag.String("workdir", "", "本地工作目录（默认 ./tmp/live-window-asr）")
	maxWindows := flag.Int("max-windows", 2, "最多跑几窗 ASR（控制费用；0=追平全部已下载分片）")
	maxSegs := flag.Int("max-segs", 0, "最多下载分片数（0=按 max-windows 自动估算）")
	skipASR := flag.Bool("skip-asr", false, "只下载/探测/规划窗口，不调用 ASR（不产生费用）")

	// HTTP 联调（add-live / one-click）
	baseURL := flag.String("base-url", "", "webserver 根地址（默认 http://127.0.0.1:30000 或 LIVE_MIXER_BASE_URL）")
	username := flag.String("user", "", "登录用户（默认 admin 或 LIVE_MIXER_USER）")
	password := flag.String("password", "", "登录密码（默认 admin 或 LIVE_MIXER_PASSWORD）")
	token := flag.String("token", "", "已有 JWT，跳过登录")
	name := flag.String("name", "", "源视频名称（add-live；默认 test-live-时间戳）")
	sourceMode := flag.String("source-mode", "live", "upcoming|live|replay（add-live，默认 live）")
	remark := flag.String("remark", "", "备注（add-live）")
	pollEvery := flag.Duration("poll", 15*time.Second, "轮询间隔（add-live / one-click）")
	waitFor := flag.Duration("wait", 0, "最长等待（add-live 默认 15m；one-click 默认 30m）")
	noWait := flag.Bool("no-wait", false, "只提交不轮询（add-live / one-click）")

	// one-click
	liveID := flag.Uint("live-id", 0, "源视频 ID（one-click，add-live 返回的 id）")
	promptID := flag.Uint("prompt-id", 1, "系统提示词 ID（one-click，默认 1）")
	clipMS := flag.String("clip", "", "选区毫秒 start-end，多个逗号分隔：0-600000")
	clipSec := flag.String("clip-sec", "", "选区秒 start-end，多个逗号分隔：0-600")
	autoClip := flag.Bool("auto-clip", true, "未指定 -clip 时按 asr_cursor 自动选区 [0,end]")
	maxClipMS := flag.Int64("max-clip-ms", 10*60*1000, "自动选区最大毫秒（默认 10 分钟，控 LLM/成片费用）")
	projectSource := flag.String("project-source", "timeline", "项目来源（one-click，默认 timeline）")
	canvasW := flag.Int("canvas-width", 0, "画布宽（可选）")
	canvasH := flag.Int("canvas-height", 0, "画布高（可选）")
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

	httpBase := firstNonEmpty(strings.TrimSpace(*baseURL), os.Getenv("LIVE_MIXER_BASE_URL"), "http://127.0.0.1:30000")

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
	case "add-live":
		out := strings.TrimSpace(*outPath)
		if out == "" {
			out = "add_live_report.json"
		}
		wait := *waitFor
		if wait <= 0 {
			wait = 15 * time.Minute
		}
		runAddLiveMode(addLiveArgs{
			BaseURL:      httpBase,
			Username:     *username,
			Password:     *password,
			Token:        *token,
			Name:         *name,
			M3U8URL:      strings.TrimSpace(*m3u8URL),
			SourceMode:   *sourceMode,
			Remark:       *remark,
			PollInterval: *pollEvery,
			Wait:         wait,
			NoWait:       *noWait,
			OutPath:      out,
		})
	case "one-click":
		out := strings.TrimSpace(*outPath)
		if out == "" {
			out = "one_click_report.json"
		}
		clips, err := parseClipList(*clipMS, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "-clip: %v\n", err)
			os.Exit(2)
		}
		if secClips, err := parseClipList(*clipSec, true); err != nil {
			fmt.Fprintf(os.Stderr, "-clip-sec: %v\n", err)
			os.Exit(2)
		} else {
			clips = append(clips, secClips...)
		}
		wait := *waitFor
		if wait <= 0 {
			wait = 30 * time.Minute
		}
		runOneClickMode(oneClickArgs{
			BaseURL:      httpBase,
			Username:     *username,
			Password:     *password,
			Token:        *token,
			LiveID:       uint(*liveID),
			PromptID:     uint(*promptID),
			Clips:        clips,
			AutoClip:     *autoClip && len(clips) == 0,
			MaxClipMS:    *maxClipMS,
			ProjectSrc:   *projectSource,
			CanvasW:      *canvasW,
			CanvasH:      *canvasH,
			PollInterval: *pollEvery,
			Wait:         wait,
			NoWait:       *noWait,
			OutPath:      out,
		})
	default:
		fmt.Fprintf(os.Stderr, "未知 mode=%q，请用 paragraphs | live-window | add-live | one-click\n", resolved)
		os.Exit(2)
	}
}

// 用户提供的跟播列表，便于本地复现。
const defaultLiveWindowTestURL = "https://live-mixer-1426793176.cos.ap-shanghai.myqcloud.com/video_editing/live-record/1e84428cac6e4dd1ac24b8bdcec0a3c8/live.m3u8"
