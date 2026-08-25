package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultBaseURL 豆包 BigModel ASR API 默认地址。
	DefaultBaseURL = "https://openspeech.bytedance.com/api/v3/auc/bigmodel"
	// DefaultResourceID 豆包录音文件识别模型 2.0 资源 ID。
	DefaultResourceID = "volc.seedasr.auc"

	headerStatusCode = "X-Api-Status-Code"
	headerMessage    = "X-Api-Message"

	statusSuccess      = "20000000"
	statusProcessing1  = "20000001"
	statusProcessing2  = "20000002"
	defaultPollInterval = 10 * time.Second
	defaultMaxPolls     = 360
)

// Config 豆包 ASR 客户端配置。
type Config struct {
	APIKey       string
	BaseURL      string
	ResourceID   string
	PollInterval time.Duration
	MaxPolls     int
	HTTPClient   *http.Client
}

// Client 豆包 ASR HTTP 客户端，负责提交任务并轮询完整结果。
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient 创建豆包 ASR 客户端；未设置的字段使用默认值。
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.ResourceID == "" {
		cfg.ResourceID = DefaultResourceID
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.MaxPolls <= 0 {
		cfg.MaxPolls = defaultMaxPolls
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{cfg: cfg, http: httpClient}
}

type submitPayload struct {
	User struct {
		UID string `json:"uid"`
	} `json:"user"`
	Audio struct {
		Format string `json:"format"`
		URL    string `json:"url"`
	} `json:"audio"`
	Request struct {
		ModelName         string `json:"model_name"`
		ShowUtterances    bool   `json:"show_utterances"`
		EnableITN         bool   `json:"enable_itn"`
		EnablePunc        bool   `json:"enable_punc"`
		EnableSpeakerInfo bool   `json:"enable_speaker_info"`
	} `json:"request"`
}

// ProgressCallback 轮询进度回调，progress 取值 5~95。
type ProgressCallback func(progress int)

// Transcribe 同步转写：提交 audioURL 对应任务并轮询，返回豆包 ASR 完整 JSON 结果。
func (c *Client) Transcribe(ctx context.Context, audioURL string) (json.RawMessage, error) {
	return c.TranscribeWithProgress(ctx, audioURL, nil)
}

// TranscribeWithProgress 同步转写并在轮询时回调估算进度。
func (c *Client) TranscribeWithProgress(ctx context.Context, audioURL string, onProgress ProgressCallback) (json.RawMessage, error) {
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return nil, fmt.Errorf("ASR API Key 未配置")
	}

	format, err := DetectFormat(audioURL)
	if err != nil {
		return nil, err
	}

	taskID := uuid.NewString()
	if err := c.submit(ctx, taskID, audioURL, format); err != nil {
		return nil, err
	}
	return c.poll(ctx, taskID, onProgress)
}

// submit 向豆包 ASR 提交异步识别任务。
func (c *Client) submit(ctx context.Context, taskID, audioURL, format string) error {
	var body submitPayload
	body.User.UID = "live-mixer"
	body.Audio.Format = format
	body.Audio.URL = audioURL
	body.Request.ModelName = "bigmodel"
	body.Request.ShowUtterances = true
	body.Request.EnableITN = true
	body.Request.EnablePunc = true
	body.Request.EnableSpeakerInfo = true

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化 ASR 提交请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/submit", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("创建 ASR 提交请求失败: %w", err)
	}
	c.setCommonHeaders(req.Header, taskID)
	req.Header.Set("X-Api-Sequence", "-1")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ASR 提交请求失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	code := resp.Header.Get(headerStatusCode)
	if code != statusSuccess {
		return fmt.Errorf("ASR 提交失败: %s %s", code, resp.Header.Get(headerMessage))
	}
	return nil
}

// calcPollProgress 根据轮询次数估算进度（5~95）。
func calcPollProgress(pollIndex, maxPolls int) int {
	if maxPolls <= 0 {
		return 5
	}
	progress := 5 + pollIndex*90/maxPolls
	if progress > 95 {
		return 95
	}
	return progress
}

// poll 轮询 ASR 任务状态，直至成功或超时/失败。
func (c *Client) poll(ctx context.Context, taskID string, onProgress ProgressCallback) (json.RawMessage, error) {
	for i := 0; i < c.cfg.MaxPolls; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if i > 0 {
			if err := sleepWithContext(ctx, c.cfg.PollInterval); err != nil {
				return nil, err
			}
		}

		if onProgress != nil {
			onProgress(calcPollProgress(i, c.cfg.MaxPolls))
		}

		result, done, err := c.queryOnce(ctx, taskID)
		if err != nil {
			// 单次查询传输/读超时等瞬时错误：继续轮询，不立刻整单失败。
			if isTransientQueryError(ctx, err) {
				continue
			}
			return nil, err
		}
		if done {
			return result, nil
		}
	}
	return nil, fmt.Errorf("ASR 任务轮询超时，已尝试 %d 次", c.cfg.MaxPolls)
}

// isTransientQueryError 判断 ASR 查询错误是否为可软重试的传输层问题。
// 父 ctx 已取消/超时、业务失败码等返回 false；http.Client 自身 Timeout 导致的
// DeadlineExceeded（父 ctx 仍有效）、EOF / UnexpectedEOF、net.Error 超时等视为瞬时错误。
func isTransientQueryError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isTransientQueryError(ctx, urlErr.Err)
	}
	return false
}

// queryOnce 查询一次 ASR 任务状态；done 为 true 时表示已获得完整结果。
func (c *Client) queryOnce(ctx context.Context, taskID string) (json.RawMessage, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/query", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, false, fmt.Errorf("创建 ASR 查询请求失败: %w", err)
	}
	c.setCommonHeaders(req.Header, taskID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("ASR 查询请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("读取 ASR 查询响应失败: %w", err)
	}

	code := resp.Header.Get(headerStatusCode)
	switch code {
	case statusSuccess:
		if !json.Valid(body) {
			return nil, false, fmt.Errorf("ASR 查询响应不是合法 JSON")
		}
		return json.RawMessage(body), true, nil
	case statusProcessing1, statusProcessing2:
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("ASR 查询失败: %s %s", code, resp.Header.Get(headerMessage))
	}
}

func (c *Client) setCommonHeaders(header http.Header, taskID string) {
	header.Set("X-Api-Key", c.cfg.APIKey)
	header.Set("X-Api-Resource-Id", c.cfg.ResourceID)
	header.Set("X-Api-Request-Id", taskID)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
