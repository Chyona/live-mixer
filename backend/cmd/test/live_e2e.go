package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// live-e2e：打真实 webserver，串联 UI 同款链路：
// 添加正在直播 → 跟播录像满 duration → 等到窗口 ASR 覆盖该时长 → 一键成片。
// 用于验证 live_ingest / 窗口 ASR / ai-slice-draft 现有逻辑的正确性与可靠性。

type liveE2EArgs struct {
	BaseURL      string
	ConfigPath   string
	JWTSecret    string
	Username     string
	Password     string
	Token        string
	UserID       uint
	ForceLogin   bool
	Name         string
	M3U8URL      string
	Remark       string
	Duration     time.Duration // 目标时长（报告用；默认与 RecordFor / MaxClipMS 一致）
	RecordFor    time.Duration // 录像目标时长（按 material.duration 计）
	PollInterval time.Duration
	Wait         time.Duration // 录制+ASR 总等待上限
	PromptID     uint
	MaxClipMS    int64
	ProjectSrc   string
	CanvasW      int
	CanvasH      int
	OneClickWait time.Duration
	SkipOneClick bool
	OutPath      string
}

type liveE2EReport struct {
	CreatedAt   time.Time          `json:"created_at"`
	BaseURL     string             `json:"base_url"`
	M3U8URL     string             `json:"m3u8_url"`
	Duration    string             `json:"duration"`
	RecordFor   string             `json:"record_for"`
	MaxClipMS   int64              `json:"max_clip_ms"`
	MaterialID  uint               `json:"material_id"`
	Create      materialSnapshot   `json:"create"`
	AfterRecord materialSnapshot   `json:"after_record,omitempty"`
	AfterASR    materialSnapshot   `json:"after_asr,omitempty"`
	Polls       []materialSnapshot `json:"polls"`
	OneClickOut string             `json:"one_click_report,omitempty"`
	OK          bool               `json:"ok"`
	Reason      string             `json:"reason"`
}

func runLiveE2EMode(a liveE2EArgs) {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:30000"
	}
	m3u8 := strings.TrimSpace(a.M3U8URL)
	if m3u8 == "" {
		fmt.Fprintln(os.Stderr, "请用 -url 指定正在直播的 m3u8")
		os.Exit(2)
	}
	recordFor := a.RecordFor
	if recordFor <= 0 {
		recordFor = a.Duration
	}
	if recordFor <= 0 {
		recordFor = 10 * time.Minute
	}
	e2eDur := a.Duration
	if e2eDur <= 0 {
		e2eDur = recordFor
	}
	maxClipMS := a.MaxClipMS
	if maxClipMS <= 0 {
		maxClipMS = e2eDur.Milliseconds()
	}
	poll := a.PollInterval
	if poll <= 0 {
		poll = 15 * time.Second
	}
	wait := a.Wait
	if wait <= 0 {
		wait = recordFor + 15*time.Minute
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = fmt.Sprintf("e2e-live-%s", time.Now().Format("20060102-150405"))
	}

	client := &http.Client{Timeout: 60 * time.Second}
	ctx := context.Background()

	token, err := resolveHTTPToken(ctx, client, base, a.Token, a.Username, a.Password, a.ConfigPath, a.JWTSecret, a.UserID, a.ForceLogin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 token 失败: %v\n", err)
		fmt.Fprintf(os.Stderr, "提示: 本地 webserver.exe 用 config.yaml 的 jwt.secret；docker 用 APP_JWT_SECRET。可用 -jwt-secret 显式指定。\n")
		os.Exit(1)
	}

	reqBody := map[string]any{
		"name":        name,
		"source_mode": "live",
		"m3u8_url":    m3u8,
	}
	if r := strings.TrimSpace(a.Remark); r != "" {
		reqBody["remark"] = r
	}

	fmt.Printf("=== live-e2e ===\nbase=%s\nurl=%s\nduration=%s record_for=%s max_clip_ms=%d wait=%s\n",
		base, m3u8, e2eDur, recordFor, maxClipMS, wait)
	fmt.Println("1/3 创建源视频（模拟 UI 正在直播）…")
	created, err := apiCreateLiveMaterial(ctx, client, base, token, reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建失败: %v\n", err)
		os.Exit(1)
	}
	printMaterialLine("已创建", created)

	report := liveE2EReport{
		CreatedAt:  time.Now(),
		BaseURL:    base,
		M3U8URL:    m3u8,
		Duration:   e2eDur.String(),
		RecordFor:  recordFor.String(),
		MaxClipMS:  maxClipMS,
		MaterialID: created.ID,
		Create:     created,
		Polls:      []materialSnapshot{created},
	}

	recordTargetMS := recordFor.Milliseconds()
	// 成片选区需要 ASR 覆盖到选区终点；允许少量封窗误差。
	asrTargetMS := maxClipMS
	if asrTargetMS > recordTargetMS {
		asrTargetMS = recordTargetMS
	}
	const asrCoverSlackMS int64 = 3000
	if asrTargetMS > asrCoverSlackMS {
		asrTargetMS -= asrCoverSlackMS
	}
	deadline := time.Now().Add(wait)
	fmt.Printf("2/3 等待跟播录像 ≥ %s，并等待 ASR 覆盖 ≥ %dms（asr_cursor_ms）…\n", recordFor, asrTargetMS)

	var latest materialSnapshot = created
	recordOK, asrOK := false, false
	for time.Now().Before(deadline) {
		time.Sleep(poll)
		snap, err := apiGetLiveMaterial(ctx, client, base, token, created.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "轮询失败: %v\n", err)
			continue
		}
		latest = snap
		report.Polls = append(report.Polls, snap)
		printMaterialLine("轮询", snap)

		if snap.LiveStatus == "failed" {
			report.OK = false
			report.Reason = "live_failed"
			report.AfterRecord = snap
			writeLiveE2EReport(a.OutPath, report)
			fmt.Fprintf(os.Stderr, "跟播失败: %s\n", firstNonEmpty(snap.IngestErrorMsg, snap.ASRErrorMsg))
			os.Exit(1)
		}
		if !recordOK && snap.Duration >= recordTargetMS {
			recordOK = true
			report.AfterRecord = snap
			fmt.Printf("录像达标: duration_ms=%d (≥ %d)\n", snap.Duration, recordTargetMS)
		}
		if !asrOK && snap.ASRCursorMS >= asrTargetMS {
			asrOK = true
			report.AfterASR = snap
			fmt.Printf("ASR 已覆盖: asr_cursor_ms=%d (≥ %d) asr_progress=%d%%\n",
				snap.ASRCursorMS, asrTargetMS, snap.ASRProgress)
		}
		if recordOK && asrOK {
			break
		}
	}

	if !recordOK || !asrOK {
		report.OK = false
		report.Reason = "timeout_record_or_asr"
		report.AfterRecord = latest
		if asrOK {
			report.AfterASR = latest
		}
		writeLiveE2EReport(a.OutPath, report)
		fmt.Fprintf(os.Stderr, "超时：record_ok=%v asr_ok=%v（最后 duration_ms=%d asr_cursor_ms=%d，目标 record≥%d asr≥%d）\n",
			recordOK, asrOK, latest.Duration, latest.ASRCursorMS, recordTargetMS, asrTargetMS)
		fmt.Fprintln(os.Stderr, "请查 webserver 日志：跟播录像进度 / 窗口 ASR 调度到期 / 调度窗口 ASR / 窗口 ASR 步骤")
		os.Exit(1)
	}

	if a.SkipOneClick {
		report.OK = true
		report.Reason = "skip_one_click"
		writeLiveE2EReport(a.OutPath, report)
		fmt.Printf("已跳过一键成片（-skip-one-click）。live_id=%d\n", created.ID)
		return
	}

	fmt.Printf("3/3 发起一键成片 live_id=%d（选区上限 %dms）…\n", created.ID, maxClipMS)
	oneClickOut := ""
	if strings.TrimSpace(a.OutPath) != "" {
		oneClickOut = strings.TrimSuffix(a.OutPath, ".json") + "_one_click.json"
		if oneClickOut == a.OutPath {
			oneClickOut = a.OutPath + ".one_click.json"
		}
	}
	ocWait := a.OneClickWait
	if ocWait <= 0 {
		ocWait = 30 * time.Minute
	}
	// 复用 one-click 完整提交与轮询（同一套 /v1/tasks/ai-slice-draft）
	runOneClickMode(oneClickArgs{
		BaseURL:      base,
		ConfigPath:   a.ConfigPath,
		JWTSecret:    a.JWTSecret,
		Token:        token,
		UserID:       a.UserID,
		LiveID:       created.ID,
		PromptID:     a.PromptID,
		AutoClip:     true,
		MaxClipMS:    maxClipMS,
		ProjectSrc:   firstNonEmpty(strings.TrimSpace(a.ProjectSrc), "timeline"),
		CanvasW:      a.CanvasW,
		CanvasH:      a.CanvasH,
		PollInterval: poll,
		Wait:         ocWait,
		OutPath:      oneClickOut,
	})

	// runOneClickMode 成功会 return；失败会 Exit。走到这里即成片轮询成功。
	report.OK = true
	report.Reason = "e2e_ok"
	report.AfterASR = latest
	report.OneClickOut = oneClickOut
	writeLiveE2EReport(a.OutPath, report)
	fmt.Printf("live-e2e 完成 live_id=%d\n", created.ID)
}

func writeLiveE2EReport(path string, report liveE2EReport) {
	if strings.TrimSpace(path) == "" {
		return
	}
	mustWriteJSON(path, report)
	fmt.Printf("E2E 报告已写入 %s\n", path)
}
