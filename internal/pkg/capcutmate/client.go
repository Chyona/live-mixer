// Package capcutmate 封装剪映草稿服务（capcut-mate）的 HTTP 调用。
package capcutmate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultBaseURL capcut-mate 服务默认地址（经网关或直接访问均可）。
	DefaultBaseURL = "http://192.168.3.219:81"
	// DefaultTimeout 单次 HTTP 请求超时。
	DefaultTimeout = 120 * time.Second

	pathCreateDraft = "/openapi/capcut-mate/v1/create_draft"
	pathAddVideos   = "/openapi/capcut-mate/v1/add_videos"
)

// Config capcut-mate 客户端配置。
type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// Client 调用 capcut-mate REST API 的客户端。
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient 创建 capcut-mate 客户端；未设置的字段使用默认值。
func NewClient(cfg Config) *Client {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{baseURL: base, http: httpClient}
}

// CreateDraftRequest 创建剪映草稿请求体。
type CreateDraftRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// CreateDraftResponse 创建剪映草稿响应。
type CreateDraftResponse struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	DraftURL string `json:"draft_url"`
	TipURL   string `json:"tip_url,omitempty"`
}

// VideoInfo 单条待添加到草稿的视频片段信息（时间单位：微秒）。
type VideoInfo struct {
	VideoURL           string  `json:"video_url"`
	Start              int64   `json:"start"`
	End                int64   `json:"end"`
	Transition         string  `json:"transition,omitempty"`
	TransitionDuration int64   `json:"transition_duration,omitempty"`
	Volume             float64 `json:"volume,omitempty"`
}

// AddVideosRequest 批量向草稿添加视频请求体。
// 注意：API 要求 video_infos 为 JSON 字符串，而非嵌套数组。
type AddVideosRequest struct {
	Alpha          float64 `json:"alpha"`
	DraftURL       string  `json:"draft_url"`
	ScaleX         float64 `json:"scale_x"`
	ScaleY         float64 `json:"scale_y"`
	SceneTimelines []any   `json:"scene_timelines"`
	TransformX     float64 `json:"transform_x"`
	TransformY     float64 `json:"transform_y"`
	VideoInfos     string  `json:"video_infos"`
}

// AddVideosResponse 批量添加视频响应。
type AddVideosResponse struct {
	Code         int            `json:"code"`
	Message      string         `json:"message"`
	DraftURL     string         `json:"draft_url"`
	TrackID      string         `json:"track_id,omitempty"`
	VideoIDs     []string       `json:"video_ids,omitempty"`
	SegmentIDs   []string       `json:"segment_ids,omitempty"`
	SegmentInfos []SegmentInfo  `json:"segment_infos,omitempty"`
}

// SegmentInfo 时间轴片段信息。
type SegmentInfo struct {
	ID    string `json:"id"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

// apiCallRecord 落盘到 staging/{task_id}/capcut_mate 的请求/响应记录结构。
type apiCallRecord struct {
	Seq                int             `json:"seq"`
	RecordedAt         string          `json:"recorded_at"`
	Method             string          `json:"method"`
	Path               string          `json:"path"`
	Request            json.RawMessage `json:"request"`
	ResponseHTTPStatus int             `json:"response_http_status"`
	Response           json.RawMessage `json:"response"`
}

// CreateDraft 调用 /create_draft 创建空白剪映草稿。
// recordDir 非空时，将请求与响应写入该目录（如 staging/{task_id}/capcut_mate）。
func (c *Client) CreateDraft(ctx context.Context, width, height int, recordDir string) (*CreateDraftResponse, error) {
	reqBody := CreateDraftRequest{Width: width, Height: height}
	var resp CreateDraftResponse
	if err := c.doJSON(ctx, pathCreateDraft, "create_draft", reqBody, &resp, recordDir); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("create_draft 业务失败: code=%d message=%s", resp.Code, resp.Message)
	}
	if strings.TrimSpace(resp.DraftURL) == "" {
		return &resp, fmt.Errorf("create_draft 未返回 draft_url")
	}
	return &resp, nil
}

// AddVideos 调用 /add_videos 向草稿批量添加视频切片。
// recordDir 非空时落盘请求/响应，便于排查。
func (c *Client) AddVideos(ctx context.Context, req AddVideosRequest, recordDir string) (*AddVideosResponse, error) {
	// 补齐与文档一致的默认变换参数，避免调用方遗漏。
	if req.Alpha == 0 {
		req.Alpha = 1
	}
	if req.ScaleX == 0 {
		req.ScaleX = 1
	}
	if req.ScaleY == 0 {
		req.ScaleY = 1
	}
	if req.SceneTimelines == nil {
		req.SceneTimelines = []any{}
	}

	var resp AddVideosResponse
	if err := c.doJSON(ctx, pathAddVideos, "add_videos", req, &resp, recordDir); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("add_videos 业务失败: code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp, nil
}

// BuildVideoInfosJSON 将视频片段列表序列化为 API 所需的 JSON 字符串。
func BuildVideoInfosJSON(infos []VideoInfo) (string, error) {
	if infos == nil {
		infos = []VideoInfo{}
	}
	raw, err := json.Marshal(infos)
	if err != nil {
		return "", fmt.Errorf("序列化 video_infos 失败: %w", err)
	}
	return string(raw), nil
}

// doJSON 发送 JSON POST，可选落盘完整请求/响应记录。
func (c *Client) doJSON(ctx context.Context, path, name string, reqBody any, respBody any, recordDir string) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	url := c.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.http.Do(httpReq)
	status := 0
	var respBytes []byte
	if err != nil {
		_ = c.writeRecord(recordDir, name, path, payload, status, map[string]string{
			"_error": err.Error(),
		})
		return fmt.Errorf("请求 capcut-mate %s 失败: %w", path, err)
	}
	defer httpResp.Body.Close()
	status = httpResp.StatusCode

	respBytes, readErr := io.ReadAll(httpResp.Body)
	if readErr != nil {
		_ = c.writeRecord(recordDir, name, path, payload, status, map[string]string{
			"_raw_text": "",
			"_error":    readErr.Error(),
		})
		return fmt.Errorf("读取 capcut-mate 响应失败: %w", readErr)
	}

	// 优先按 JSON 解析；失败则将原始文本写入记录，便于排查网关/502 问题。
	var recordedResp any
	if len(bytes.TrimSpace(respBytes)) == 0 {
		recordedResp = map[string]string{"_raw_text": ""}
	} else if json.Valid(respBytes) {
		recordedResp = json.RawMessage(respBytes)
	} else {
		recordedResp = map[string]string{"_raw_text": string(respBytes)}
	}
	_ = c.writeRecord(recordDir, name, path, payload, status, recordedResp)

	if status < 200 || status >= 300 {
		return fmt.Errorf("capcut-mate %s HTTP %d: %s", path, status, strings.TrimSpace(string(respBytes)))
	}
	if err := json.Unmarshal(respBytes, respBody); err != nil {
		return fmt.Errorf("解析 capcut-mate 响应失败: %w, body=%s", err, strings.TrimSpace(string(respBytes)))
	}
	return nil
}

// writeRecord 将一次 API 调用写入 recordDir，文件名形如 001_create_draft.json。
// 序号按目录内已有记录文件数递增，保证同一 task 的 staging 目录从 001 起算。
func (c *Client) writeRecord(recordDir, name, path string, requestJSON []byte, httpStatus int, response any) error {
	if strings.TrimSpace(recordDir) == "" {
		return nil
	}
	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		return err
	}

	seq := 1
	if entries, err := os.ReadDir(recordDir); err == nil {
		seq = len(entries) + 1
	}

	respRaw, err := json.Marshal(response)
	if err != nil {
		respRaw = []byte(`{"_error":"marshal response failed"}`)
	}

	rec := apiCallRecord{
		Seq:                seq,
		RecordedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		Method:             http.MethodPost,
		Path:               path,
		Request:            json.RawMessage(requestJSON),
		ResponseHTTPStatus: httpStatus,
		Response:           json.RawMessage(respRaw),
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	filename := filepath.Join(recordDir, fmt.Sprintf("%03d_%s.json", seq, name))
	return os.WriteFile(filename, data, 0o644)
}
