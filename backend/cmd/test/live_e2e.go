package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"live-mixer/internal/config"
	jwtpkg "live-mixer/internal/pkg/jwt"
)

// live-e2e：打真实 webserver，串联 UI 同款链路：
// 添加正在直播 → 跟播录像满 record-for → 等到窗口 ASR 推进 → 一键成片。
// 用于验证 live_ingest / 窗口 ASR / ai-slice-draft 现有逻辑的正确性与可靠性。

type liveE2EArgs struct {
	BaseURL      string
	ConfigPath   string
	Username     string
	Password     string
	Token        string
	UserID       uint
	ForceLogin   bool
	Name         string
	M3U8URL      string
	Remark       string
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
	RecordFor   string             `json:"record_for"`
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
		recordFor = 10 * time.Minute
	}
	poll := a.PollInterval
	if poll <= 0 {
		poll = 15 * time.Second
	}
	wait := a.Wait
	if wait <= 0 {
		// 录 10 分钟 + 首窗 ASR 余量 + 网络抖动
		wait = recordFor + 15*time.Minute
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = fmt.Sprintf("e2e-live-%s", time.Now().Format("20060102-150405"))
	}

	client := &http.Client{Timeout: 60 * time.Second}
	ctx := context.Background()

	token, err := resolveHTTPToken(ctx, client, base, a.Token, a.Username, a.Password, a.ConfigPath, a.UserID, a.ForceLogin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 token 失败: %v\n", err)
		fmt.Fprintf(os.Stderr, "请确认 -config / APP_JWT_SECRET 与 webserver 一致，或改用 -login 密码登录（当前 %s，登录 URL 形如 %s）\n",
			base, apiURL(base, "/v1/auth/login"))
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

	fmt.Printf("=== live-e2e ===\nbase=%s\nurl=%s\nrecord_for=%s wait=%s\n", base, m3u8, recordFor, wait)
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
		RecordFor:  recordFor.String(),
		MaterialID: created.ID,
		Create:     created,
		Polls:      []materialSnapshot{created},
	}

	recordTargetMS := recordFor.Milliseconds()
	deadline := time.Now().Add(wait)
	fmt.Printf("2/3 等待跟播录像 ≥ %s，并等待窗口 ASR 推进（asr_cursor_ms>0）…\n", recordFor)

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
		if !asrOK && (snap.ASRCursorMS > 0 || snap.ASRProgress > 0) {
			asrOK = true
			report.AfterASR = snap
			fmt.Printf("ASR 已推进: asr_cursor_ms=%d asr_progress=%d%%\n", snap.ASRCursorMS, snap.ASRProgress)
		}
		// 产品逻辑：约录满一个窗口后才调度 ASR；两者都满足再成片更稳。
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
		fmt.Fprintf(os.Stderr, "超时：record_ok=%v asr_ok=%v（最后 duration_ms=%d asr_cursor_ms=%d）\n",
			recordOK, asrOK, latest.Duration, latest.ASRCursorMS)
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

	fmt.Printf("3/3 发起一键成片 live_id=%d …\n", created.ID)
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
		Token:        token,
		UserID:       a.UserID,
		LiveID:       created.ID,
		PromptID:     a.PromptID,
		AutoClip:     true,
		MaxClipMS:    a.MaxClipMS,
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

func resolveHTTPToken(ctx context.Context, client *http.Client, base, token, username, password, configPath string, userID uint, forceLogin bool) (string, error) {
	if t := strings.TrimSpace(token); t != "" {
		return t, nil
	}
	if !forceLogin {
		uid := userID
		if uid == 0 {
			uid = 1
		}
		user := firstNonEmpty(strings.TrimSpace(username), os.Getenv("LIVE_MIXER_USER"), "admin")
		t, err := mintTokenFromConfig(configPath, uid, user)
		if err == nil {
			fmt.Printf("已用配置 JWT 签发 token（uid=%d user=%s）\n", uid, user)
			return t, nil
		}
		fmt.Fprintf(os.Stderr, "警告: 本地签发 token 失败，回退密码登录: %v\n", err)
	}
	user := firstNonEmpty(strings.TrimSpace(username), os.Getenv("LIVE_MIXER_USER"), "admin")
	pass := firstNonEmpty(strings.TrimSpace(password), os.Getenv("LIVE_MIXER_PASSWORD"), "admin")
	t, err := apiLogin(ctx, client, base, user, pass)
	if err != nil {
		return "", err
	}
	fmt.Printf("已登录 %s @ %s\n", user, base)
	return t, nil
}

func mintTokenFromConfig(configPath string, userID uint, username string) (string, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return "", fmt.Errorf("加载配置: %w", err)
	}
	secret := strings.TrimSpace(cfg.JWT.Secret)
	if secret == "" {
		return "", fmt.Errorf("jwt.secret 为空（可设 APP_JWT_SECRET 或 -config）")
	}
	exp := cfg.JWT.ExpiresIn
	if exp <= 0 {
		exp = 86400
	}
	if userID == 0 {
		userID = 1
	}
	if strings.TrimSpace(username) == "" {
		username = "admin"
	}
	return jwtpkg.GenerateToken(secret, exp, jwtpkg.UserClaims{
		UserID:   userID,
		Username: username,
		Nickname: "测试",
		Roles:    []string{"ADMIN"},
	})
}
