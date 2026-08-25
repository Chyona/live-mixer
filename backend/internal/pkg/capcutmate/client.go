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
	// DefaultGenVideoPollInterval 视频生成状态轮询间隔。
	DefaultGenVideoPollInterval = 5 * time.Second
	// DefaultGenVideoMaxPolls 视频生成最大轮询次数（默认约 30 分钟）。
	DefaultGenVideoMaxPolls = 360
)

// Config capcut-mate 客户端配置。
type Config struct {
	BaseURL string
	// APIKey 视频生成（gen_video）所需密钥；create_draft 等草稿接口可不填。
	APIKey string
	// GenVideoBaseURL 可选：gen_video / gen_video_status 共用根地址；为空则使用 BaseURL。
	GenVideoBaseURL string
	HTTPClient      *http.Client
	Timeout         time.Duration
	// PollInterval / MaxPolls 控制 GenerateVideoAndWait 轮询行为。
	PollInterval time.Duration
	MaxPolls     int
}

// Client 调用 capcut-mate REST API 的客户端。
type Client struct {
	baseURL         string
	apiKey          string
	genVideoBaseURL string
	http            *http.Client
	pollInterval    time.Duration
	maxPolls        int
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
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultGenVideoPollInterval
	}
	maxPolls := cfg.MaxPolls
	if maxPolls <= 0 {
		maxPolls = DefaultGenVideoMaxPolls
	}
	return &Client{
		baseURL:         base,
		apiKey:          strings.TrimSpace(cfg.APIKey),
		genVideoBaseURL: strings.TrimRight(strings.TrimSpace(cfg.GenVideoBaseURL), "/"),
		http:            httpClient,
		pollInterval:    pollInterval,
		maxPolls:        maxPolls,
	}
}

// genVideoEndpoint 解析视频生成相关接口地址：优先 GenVideoBaseURL，否则 BaseURL。
func (c *Client) genVideoEndpoint(path string) string {
	base := c.genVideoBaseURL
	if base == "" {
		base = c.baseURL
	}
	return base + path
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

// doJSON 发送 JSON POST（地址为 baseURL + path），可选落盘完整请求/响应记录。
func (c *Client) doJSON(ctx context.Context, path, name string, reqBody any, respBody any, recordDir string) error {
	return c.doJSONURL(ctx, c.baseURL+path, path, name, reqBody, respBody, recordDir)
}

// doJSONURL 向指定 URL 发送 JSON POST；recordPath 用于落盘记录中的 path 字段。
func (c *Client) doJSONURL(ctx context.Context, url, recordPath, name string, reqBody any, respBody any, recordDir string) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	label := recordPath
	if label == "" {
		label = url
	}

	httpResp, err := c.http.Do(httpReq)
	status := 0
	var respBytes []byte
	if err != nil {
		_ = c.writeRecord(recordDir, name, label, payload, status, map[string]string{
			"_error": err.Error(),
		})
		return fmt.Errorf("请求 capcut-mate %s 失败: %w", label, err)
	}
	defer httpResp.Body.Close()
	status = httpResp.StatusCode

	respBytes, readErr := io.ReadAll(httpResp.Body)
	if readErr != nil {
		_ = c.writeRecord(recordDir, name, label, payload, status, map[string]string{
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
	_ = c.writeRecord(recordDir, name, label, payload, status, recordedResp)

	if status < 200 || status >= 300 {
		return fmt.Errorf("capcut-mate %s HTTP %d: %s", label, status, strings.TrimSpace(string(respBytes)))
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
