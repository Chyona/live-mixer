package capcutmate

import (
	"context"
	"fmt"
	"strings"
)

const pathCreateDraft = "/openapi/capcut-mate/v1/create_draft"

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
