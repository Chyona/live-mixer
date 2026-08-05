package capcutmate

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	pathGenVideo       = "/openapi/capcut-mate/v1/gen_video"
	pathGenVideoStatus = "/openapi/capcut-mate/v1/gen_video_status"

	// GenVideoStatus* 与 capcut-mate gen_video_status 返回的 status 对齐。
	GenVideoStatusPending    = "pending"
	GenVideoStatusProcessing = "processing"
	GenVideoStatusCompleted  = "completed"
	GenVideoStatusFailed     = "failed"
)

// GenVideoRequest 提交视频生成任务请求体。
type GenVideoRequest struct {
	APIKey   string `json:"apiKey"`
	DraftURL string `json:"draft_url"`
}

// GenVideoResponse 提交视频生成任务响应。
type GenVideoResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// GenVideoStatusRequest 查询视频生成进度请求体。
type GenVideoStatusRequest struct {
	DraftURL string `json:"draft_url"`
}

// GenVideoStatusResponse 查询视频生成进度响应。
type GenVideoStatusResponse struct {
	Code         int     `json:"code"`
	Message      string  `json:"message"`
	DraftURL     string  `json:"draft_url"`
	Status       string  `json:"status"`
	Progress     int     `json:"progress"`
	VideoURL     string  `json:"video_url"`
	ErrorMessage string  `json:"error_message"`
	CreatedAt    string  `json:"created_at,omitempty"`
	StartedAt    string  `json:"started_at,omitempty"`
	CompletedAt  *string `json:"completed_at,omitempty"`
}

// VideoProgressCallback 视频生成轮询进度回调，progress 为远端 0–100。
type VideoProgressCallback func(progress int, status string)

// GenVideo 调用 /gen_video 提交视频生成任务。
func (c *Client) GenVideo(ctx context.Context, draftURL, recordDir string) (*GenVideoResponse, error) {
	apiKey := strings.TrimSpace(c.apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("capcut-mate API Key 未配置")
	}
	draftURL = strings.TrimSpace(draftURL)
	if draftURL == "" {
		return nil, fmt.Errorf("draft_url 为空")
	}

	reqBody := GenVideoRequest{APIKey: apiKey, DraftURL: draftURL}
	var resp GenVideoResponse
	url := c.genVideoEndpoint(pathGenVideo)
	if err := c.doJSONURL(ctx, url, pathGenVideo, "gen_video", reqBody, &resp, recordDir); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("gen_video 业务失败: code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp, nil
}

// GenVideoStatus 调用 /gen_video_status 查询视频生成进度。
func (c *Client) GenVideoStatus(ctx context.Context, draftURL, recordDir string) (*GenVideoStatusResponse, error) {
	draftURL = strings.TrimSpace(draftURL)
	if draftURL == "" {
		return nil, fmt.Errorf("draft_url 为空")
	}

	reqBody := GenVideoStatusRequest{DraftURL: draftURL}
	var resp GenVideoStatusResponse
	url := c.genVideoEndpoint(pathGenVideoStatus)
	if err := c.doJSONURL(ctx, url, pathGenVideoStatus, "gen_video_status", reqBody, &resp, recordDir); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("gen_video_status 业务失败: code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp, nil
}

// GenerateVideoAndWait 提交视频生成并轮询直至完成/失败/超时，成功时返回 video_url。
func (c *Client) GenerateVideoAndWait(ctx context.Context, draftURL, recordDir string, onProgress VideoProgressCallback) (string, error) {
	if _, err := c.GenVideo(ctx, draftURL, recordDir); err != nil {
		return "", err
	}

	for i := 0; i < c.maxPolls; i++ {
		if i > 0 {
			if err := sleepWithContext(ctx, c.pollInterval); err != nil {
				return "", err
			}
		}

		statusResp, err := c.GenVideoStatus(ctx, draftURL, recordDir)
		if err != nil {
			return "", err
		}
		if onProgress != nil {
			onProgress(statusResp.Progress, statusResp.Status)
		}

		switch strings.ToLower(strings.TrimSpace(statusResp.Status)) {
		case GenVideoStatusCompleted:
			videoURL := strings.TrimSpace(statusResp.VideoURL)
			if videoURL == "" {
				return "", fmt.Errorf("gen_video 完成但未返回 video_url")
			}
			return videoURL, nil
		case GenVideoStatusFailed:
			msg := strings.TrimSpace(statusResp.ErrorMessage)
			if msg == "" {
				msg = strings.TrimSpace(statusResp.Message)
			}
			if msg == "" {
				msg = "视频生成失败"
			}
			return "", fmt.Errorf("gen_video 失败: %s", msg)
		case GenVideoStatusPending, GenVideoStatusProcessing, "":
			// 继续轮询。
		default:
			return "", fmt.Errorf("gen_video 未知状态: %s", statusResp.Status)
		}
	}
	return "", fmt.Errorf("gen_video 轮询超时，已尝试 %d 次", c.maxPolls)
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
