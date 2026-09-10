package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type addLiveArgs struct {
	BaseURL      string
	Username     string
	Password     string
	Token        string
	Name         string
	M3U8URL      string
	SourceMode   string
	Remark       string
	PollInterval time.Duration
	Wait         time.Duration
	NoWait       bool
	OutPath      string
}

type apiBody struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type loginData struct {
	Token string `json:"token"`
}

type materialSnapshot struct {
	ID                uint   `json:"id"`
	Name              string `json:"name"`
	M3U8URL           string `json:"m3u8_url"`
	LiveURL           string `json:"live_url"`
	PlayURL           string `json:"play_url"`
	RecordPlaylistURL string `json:"record_playlist_url"`
	SourceMode        string `json:"source_mode"`
	LiveStatus        string `json:"live_status"`
	ASRStatus         string `json:"asr_status"`
	ASRProgress       int16  `json:"asr_progress"`
	ASRCursorMS       int64  `json:"asr_cursor_ms"`
	Duration          int64  `json:"duration"`
	NextSeg           int64  `json:"next_seg"`
	IngestErrorMsg    string `json:"ingest_error_msg"`
	ASRErrorMsg       string `json:"asr_error_msg"`
}

type addLiveReport struct {
	CreatedAt time.Time          `json:"created_at"`
	BaseURL   string             `json:"base_url"`
	Request   map[string]any     `json:"request"`
	Material  materialSnapshot   `json:"material"`
	Polls     []materialSnapshot `json:"polls"`
	OK        bool               `json:"ok"`
	Reason    string             `json:"reason"`
}

func runAddLiveMode(a addLiveArgs) {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:30000"
	}
	m3u8 := strings.TrimSpace(a.M3U8URL)
	if m3u8 == "" {
		fmt.Fprintln(os.Stderr, "请用 -url 指定直播 m3u8（与 UI「正在直播」添加一致）")
		os.Exit(2)
	}
	mode := strings.TrimSpace(a.SourceMode)
	if mode == "" {
		mode = "live"
	}
	switch mode {
	case "live", "upcoming", "replay":
	default:
		fmt.Fprintf(os.Stderr, "不支持的 source_mode=%q（live|upcoming|replay）\n", mode)
		os.Exit(2)
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = fmt.Sprintf("test-live-%s", time.Now().Format("20060102-150405"))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	ctx := context.Background()

	token := strings.TrimSpace(a.Token)
	if token == "" {
		user := firstNonEmpty(strings.TrimSpace(a.Username), os.Getenv("LIVE_MIXER_USER"), "admin")
		pass := firstNonEmpty(strings.TrimSpace(a.Password), os.Getenv("LIVE_MIXER_PASSWORD"), "admin")
		var err error
		token, err = apiLogin(ctx, client, base, user, pass)
		if err != nil {
			fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已登录 %s\n", user)
	}

	reqBody := map[string]any{
		"name":        name,
		"source_mode": mode,
		"m3u8_url":    m3u8,
	}
	if r := strings.TrimSpace(a.Remark); r != "" {
		reqBody["remark"] = r
	}

	fmt.Printf("创建源视频 name=%q mode=%s url=%s\n", name, mode, m3u8)
	created, err := apiCreateLiveMaterial(ctx, client, base, token, reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建失败: %v\n", err)
		os.Exit(1)
	}
	printMaterialLine("已创建", created)

	report := addLiveReport{
		CreatedAt: time.Now(),
		BaseURL:   base,
		Request:   reqBody,
		Material:  created,
		Polls:     []materialSnapshot{created},
	}

	if a.NoWait || mode == "replay" {
		report.OK = created.ID > 0
		report.Reason = "created_only"
		writeAddLiveReport(a.OutPath, report)
		fmt.Println("未轮询（-no-wait 或 replay）。请在 UI/日志中观察跟播与 ASR。")
		return
	}

	wait := a.Wait
	if wait <= 0 {
		wait = 15 * time.Minute
	}
	interval := a.PollInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	deadline := time.Now().Add(wait)
	fmt.Printf("开始轮询（间隔 %s，最长 %s）：等待 asr_cursor_ms>0 或 asr_progress>0\n", interval, wait)

	latest := created
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			report.OK = false
			report.Reason = "context_canceled"
			report.Material = latest
			writeAddLiveReport(a.OutPath, report)
			os.Exit(1)
		case <-time.After(interval):
		}
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
			report.Material = snap
			writeAddLiveReport(a.OutPath, report)
			fmt.Fprintf(os.Stderr, "跟播失败: %s\n", firstNonEmpty(snap.IngestErrorMsg, snap.ASRErrorMsg, snap.LiveStatus))
			os.Exit(1)
		}
		if snap.ASRCursorMS > 0 || snap.ASRProgress > 0 {
			report.OK = true
			report.Reason = "asr_progressed"
			report.Material = snap
			writeAddLiveReport(a.OutPath, report)
			fmt.Printf("OK：窗口 ASR 已推进 asr_cursor_ms=%d asr_progress=%d%%\n", snap.ASRCursorMS, snap.ASRProgress)
			return
		}
	}

	report.OK = false
	report.Reason = "timeout"
	report.Material = latest
	writeAddLiveReport(a.OutPath, report)
	fmt.Fprintf(os.Stderr, "超时：仍无 ASR 进度。最后状态 live_status=%s asr_progress=%d asr_cursor_ms=%d duration_ms=%d\n",
		latest.LiveStatus, latest.ASRProgress, latest.ASRCursorMS, latest.Duration)
	fmt.Fprintln(os.Stderr, "请对照 webserver 日志：跟播录像进度 / 窗口 ASR 调度到期 / 调度窗口 ASR / 窗口 ASR 步骤")
	os.Exit(1)
}

func printMaterialLine(prefix string, m materialSnapshot) {
	fmt.Printf("%s id=%d live_status=%s asr_status=%s asr_progress=%d asr_cursor_ms=%d duration_ms=%d play_url=%s\n",
		prefix, m.ID, m.LiveStatus, m.ASRStatus, m.ASRProgress, m.ASRCursorMS, m.Duration, trimURL(m.PlayURL))
}

func trimURL(u string) string {
	u = strings.TrimSpace(u)
	if len(u) <= 96 {
		return u
	}
	return u[:90] + "..."
}

func writeAddLiveReport(path string, report addLiveReport) {
	if strings.TrimSpace(path) == "" {
		return
	}
	mustWriteJSON(path, report)
	fmt.Printf("报告已写入 %s\n", path)
}

func apiLogin(ctx context.Context, client *http.Client, base, user, pass string) (string, error) {
	var data loginData
	if err := apiJSON(ctx, client, http.MethodPost, base+"/v1/auth/login", "", map[string]string{
		"username": user,
		"password": pass,
	}, &data); err != nil {
		return "", err
	}
	if strings.TrimSpace(data.Token) == "" {
		return "", fmt.Errorf("登录响应无 token")
	}
	return data.Token, nil
}

func apiCreateLiveMaterial(ctx context.Context, client *http.Client, base, token string, body map[string]any) (materialSnapshot, error) {
	var out materialSnapshot
	if err := apiJSON(ctx, client, http.MethodPost, base+"/v1/live-materials", token, body, &out); err != nil {
		return materialSnapshot{}, err
	}
	if out.ID == 0 {
		return materialSnapshot{}, fmt.Errorf("创建响应无 id")
	}
	return out, nil
}

func apiGetLiveMaterial(ctx context.Context, client *http.Client, base, token string, id uint) (materialSnapshot, error) {
	var out materialSnapshot
	if err := apiJSON(ctx, client, http.MethodGet, fmt.Sprintf("%s/v1/live-materials/%d", base, id), token, nil, &out); err != nil {
		return materialSnapshot{}, err
	}
	return out, nil
}

func apiJSON(ctx context.Context, client *http.Client, method, url, token string, reqBody any, out any) error {
	var body io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var wrap apiBody
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return fmt.Errorf("HTTP %d 非 JSON: %s", resp.StatusCode, truncate(string(raw), 240))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || wrap.Code != 0 {
		msg := firstNonEmpty(wrap.Message, truncate(string(raw), 240))
		return fmt.Errorf("%s → HTTP %d code=%d message=%s", url, resp.StatusCode, wrap.Code, msg)
	}
	if out == nil || len(wrap.Data) == 0 || string(wrap.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(wrap.Data, out); err != nil {
		return fmt.Errorf("解析 data 失败: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
