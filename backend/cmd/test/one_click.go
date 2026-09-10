package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type oneClickArgs struct {
	BaseURL      string
	ConfigPath   string
	Username     string
	Password     string
	Token        string
	UserID       uint
	ForceLogin   bool
	LiveID       uint
	PromptID     uint
	Clips        []clipRangeMS
	AutoClip     bool
	MaxClipMS    int64
	ProjectSrc   string
	CanvasW      int
	CanvasH      int
	PollInterval time.Duration
	Wait         time.Duration
	NoWait       bool
	OutPath      string
}

type clipRangeMS struct {
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
}

type taskCreateItem struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Progress  int16     `json:"progress"`
	CreatedAt time.Time `json:"created_at"`
}

type taskBatchCreateData struct {
	List  []taskCreateItem `json:"list"`
	Total int              `json:"total"`
}

type taskDetail struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Progress         int16  `json:"progress"`
	ErrorMessage     string `json:"error_message"`
	VideoProjectName string `json:"video_project_name"`
	DraftURL         string `json:"draft_url"`
	VideoURL         string `json:"video_url"`
	ClipsTarURL      string `json:"clips_tar_url"`
	LiveName         string `json:"live_name"`
}

type oneClickReport struct {
	CreatedAt time.Time        `json:"created_at"`
	BaseURL   string           `json:"base_url"`
	Request   map[string]any   `json:"request"`
	Material  materialSnapshot `json:"material"`
	Tasks     []taskDetail     `json:"tasks"`
	OK        bool             `json:"ok"`
	Reason    string           `json:"reason"`
}

func runOneClickMode(a oneClickArgs) {
	base := strings.TrimRight(strings.TrimSpace(a.BaseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:30000"
	}
	if a.LiveID == 0 {
		fmt.Fprintln(os.Stderr, "请用 -live-id 指定源视频 ID（add-live 创建后返回的 id）")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	ctx := context.Background()

	token, err := resolveHTTPToken(ctx, client, base, a.Token, a.Username, a.Password, a.ConfigPath, a.UserID, a.ForceLogin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取 token 失败: %v\n", err)
		os.Exit(1)
	}

	material, err := apiGetLiveMaterial(ctx, client, base, token, a.LiveID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取源视频失败: %v\n", err)
		os.Exit(1)
	}
	printMaterialLine("源视频", material)

	clips := a.Clips
	if len(clips) == 0 {
		if !a.AutoClip {
			fmt.Fprintln(os.Stderr, "clips0 为空：请传 -clip / -clip-sec，或使用默认 -auto-clip")
			os.Exit(2)
		}
		auto, err := autoClips0FromMaterial(material, a.MaxClipMS)
		if err != nil {
			fmt.Fprintf(os.Stderr, "自动选区失败: %v\n", err)
			os.Exit(1)
		}
		clips = auto
		fmt.Printf("自动 clips0: [%d, %d) ms（覆盖已解析 ASR，上限 max-clip-ms）\n", clips[0].StartTime, clips[0].EndTime)
	}
	if err := validateClips0(clips); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	promptID := a.PromptID
	if promptID == 0 {
		promptID = 1
	}
	projectSrc := strings.TrimSpace(a.ProjectSrc)
	if projectSrc == "" {
		projectSrc = "timeline"
	}

	reqBody := map[string]any{
		"live_id":        a.LiveID,
		"prompt_id":      promptID,
		"clips0":         clips,
		"project_source": projectSrc,
	}
	if a.CanvasW > 0 {
		reqBody["canvas_width"] = a.CanvasW
	}
	if a.CanvasH > 0 {
		reqBody["canvas_height"] = a.CanvasH
	}

	fmt.Printf("提交一键成片 live_id=%d prompt_id=%d clips=%d\n", a.LiveID, promptID, len(clips))
	batch, err := apiCreateAISliceDraft(ctx, client, base, token, reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建一键成片失败: %v\n", err)
		os.Exit(1)
	}
	if len(batch.List) == 0 {
		fmt.Fprintln(os.Stderr, "创建成功但未返回任务列表")
		os.Exit(1)
	}
	for _, t := range batch.List {
		fmt.Printf("已创建任务 id=%s type=%s status=%s\n", t.ID, t.Type, t.Status)
	}

	report := oneClickReport{
		CreatedAt: time.Now(),
		BaseURL:   base,
		Request:   reqBody,
		Material:  material,
		OK:        true,
		Reason:    "created_only",
	}

	if a.NoWait {
		for _, t := range batch.List {
			report.Tasks = append(report.Tasks, taskDetail{ID: t.ID, Type: t.Type, Status: t.Status, Progress: t.Progress})
		}
		writeOneClickReport(a.OutPath, report)
		fmt.Println("未轮询（-no-wait）。请在任务中心查看进度。")
		return
	}

	wait := a.Wait
	if wait <= 0 {
		wait = 30 * time.Minute
	}
	interval := a.PollInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	deadline := time.Now().Add(wait)
	fmt.Printf("开始轮询任务（间隔 %s，最长 %s）\n", interval, wait)

	ids := make([]string, 0, len(batch.List))
	for _, t := range batch.List {
		ids = append(ids, t.ID)
	}

	var latest []taskDetail
	for time.Now().Before(deadline) {
		latest = latest[:0]
		allDone := true
		anyFailed := false
		for _, id := range ids {
			detail, err := apiGetTask(ctx, client, base, token, id)
			if err != nil {
				fmt.Fprintf(os.Stderr, "轮询任务 %s 失败: %v\n", id, err)
				allDone = false
				continue
			}
			latest = append(latest, detail)
			fmt.Printf("轮询 task=%s status=%s progress=%d%% draft=%s err=%s\n",
				detail.ID, detail.Status, detail.Progress, trimURL(detail.DraftURL), trimErr(detail.ErrorMessage))
			switch detail.Status {
			case "completed":
			case "failed":
				anyFailed = true
			default:
				allDone = false
			}
		}
		report.Tasks = append([]taskDetail(nil), latest...)
		if anyFailed {
			report.OK = false
			report.Reason = "task_failed"
			writeOneClickReport(a.OutPath, report)
			fmt.Fprintln(os.Stderr, "一键成片任务失败")
			os.Exit(1)
		}
		if allDone && len(latest) == len(ids) {
			report.OK = true
			report.Reason = "all_completed"
			writeOneClickReport(a.OutPath, report)
			fmt.Printf("OK：%d 个一键成片任务均已完成\n", len(latest))
			for _, d := range latest {
				fmt.Printf("  task=%s draft_url=%s video_url=%s\n", d.ID, d.DraftURL, d.VideoURL)
			}
			return
		}
		time.Sleep(interval)
	}

	report.OK = false
	report.Reason = "timeout"
	report.Tasks = latest
	writeOneClickReport(a.OutPath, report)
	fmt.Fprintln(os.Stderr, "超时：任务尚未全部完成")
	os.Exit(1)
}

func autoClips0FromMaterial(m materialSnapshot, maxClipMS int64) ([]clipRangeMS, error) {
	end := m.ASRCursorMS
	if m.ASRStatus == "completed" && m.Duration > end {
		end = m.Duration
	}
	if end <= 0 {
		return nil, fmt.Errorf("源视频尚无可用 ASR（asr_cursor_ms=%d asr_progress=%d），请先等 add-live / 窗口 ASR 推进", m.ASRCursorMS, m.ASRProgress)
	}
	if maxClipMS > 0 && end > maxClipMS {
		end = maxClipMS
	}
	// 与 UI 一致：尽量不少于 5 分钟；不足则用已有覆盖（后端不强制 5 分钟）
	const uiMinMS = 5 * 60 * 1000
	if end < uiMinMS {
		fmt.Fprintf(os.Stderr, "警告: 自动选区仅 %dms（<5 分钟 UI 下限），后端仍可能接受\n", end)
	}
	return []clipRangeMS{{StartTime: 0, EndTime: end}}, nil
}

func validateClips0(clips []clipRangeMS) error {
	if len(clips) == 0 {
		return fmt.Errorf("clips0 为空：请传 -clip 或依赖 -auto-clip")
	}
	for i, c := range clips {
		if c.StartTime < 0 || c.EndTime <= c.StartTime {
			return fmt.Errorf("clips0[%d] 无效: start=%d end=%d", i, c.StartTime, c.EndTime)
		}
	}
	return nil
}

func parseClipList(raw string, seconds bool) ([]clipRangeMS, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	out := make([]clipRangeMS, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		se := strings.Split(part, "-")
		if len(se) != 2 {
			return nil, fmt.Errorf("clip 格式应为 start-end，收到 %q", part)
		}
		start, err1 := strconv.ParseInt(strings.TrimSpace(se[0]), 10, 64)
		end, err2 := strconv.ParseInt(strings.TrimSpace(se[1]), 10, 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("clip 无法解析数字: %q", part)
		}
		if seconds {
			start *= 1000
			end *= 1000
		}
		out = append(out, clipRangeMS{StartTime: start, EndTime: end})
	}
	return out, nil
}

func writeOneClickReport(path string, report oneClickReport) {
	if strings.TrimSpace(path) == "" {
		return
	}
	mustWriteJSON(path, report)
	fmt.Printf("报告已写入 %s\n", path)
}

func trimErr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return truncate(s, 80)
}

func apiCreateAISliceDraft(ctx context.Context, client *http.Client, base, token string, body map[string]any) (taskBatchCreateData, error) {
	var out taskBatchCreateData
	if err := apiJSON(ctx, client, http.MethodPost, apiURL(base, "/v1/tasks/ai-slice-draft"), token, body, &out); err != nil {
		return taskBatchCreateData{}, err
	}
	return out, nil
}

func apiGetTask(ctx context.Context, client *http.Client, base, token, id string) (taskDetail, error) {
	var out taskDetail
	if err := apiJSON(ctx, client, http.MethodGet, apiURL(base, "/v1/tasks/"+id), token, nil, &out); err != nil {
		return taskDetail{}, err
	}
	return out, nil
}
