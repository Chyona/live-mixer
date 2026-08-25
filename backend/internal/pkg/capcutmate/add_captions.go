package capcutmate

import (
	"context"
	"encoding/json"
	"fmt"
)

const pathAddCaptions = "/openapi/capcut-mate/v1/add_captions"

// CaptionItem 单条字幕（时间单位：微秒）。
// keyword 相关字段可选；有关键词高亮需求时再填充。
type CaptionItem struct {
	Start               int64  `json:"start"`
	End                 int64  `json:"end"`
	Text                string `json:"text"`
	Keyword             string `json:"keyword,omitempty"`
	KeywordColor        string `json:"keyword_color,omitempty"`
	KeywordBorderColor  string `json:"keyword_border_color,omitempty"`
	KeywordFontSize     int    `json:"keyword_font_size,omitempty"`
	FontSize            int    `json:"font_size,omitempty"`
	InAnimation         string `json:"in_animation,omitempty"`
	InAnimationDuration int64  `json:"in_animation_duration,omitempty"`
}

// AddCaptionsRequest 向草稿添加字幕请求体。
// 注意：API 要求 captions 为 JSON 字符串，而非嵌套数组。
type AddCaptionsRequest struct {
	DraftURL    string  `json:"draft_url"`
	Captions    string  `json:"captions"`
	Alignment   int     `json:"alignment"`
	Alpha       float64 `json:"alpha"`
	TextColor   string  `json:"text_color"`
	BorderColor string  `json:"border_color"`
	FontSize    int     `json:"font_size"`
	ScaleX      float64 `json:"scale_x"`
	ScaleY      float64 `json:"scale_y"`
	TransformX  float64 `json:"transform_x"`
	TransformY  float64 `json:"transform_y"`
	StyleText   bool    `json:"style_text"`
	Underline   bool    `json:"underline"`
	Italic      bool    `json:"italic"`
	Bold        bool    `json:"bold"`
	TextEffect  string  `json:"text_effect"`
	Font        string  `json:"font"`
}

// AddCaptionsResponse 添加字幕响应。
type AddCaptionsResponse struct {
	Code         int           `json:"code"`
	Message      string        `json:"message"`
	DraftURL     string        `json:"draft_url"`
	TrackID      string        `json:"track_id,omitempty"`
	TextIDs      []string      `json:"text_ids,omitempty"`
	SegmentIDs   []string      `json:"segment_ids,omitempty"`
	SegmentInfos []SegmentInfo `json:"segment_infos,omitempty"`
}

// AddCaptions 调用 /add_captions 向草稿批量添加字幕。
// recordDir 非空时落盘请求/响应，便于排查。
func (c *Client) AddCaptions(ctx context.Context, req AddCaptionsRequest, recordDir string) (*AddCaptionsResponse, error) {
	applyAddCaptionsDefaults(&req)

	var resp AddCaptionsResponse
	if err := c.doJSON(ctx, pathAddCaptions, "add_captions", req, &resp, recordDir); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return &resp, fmt.Errorf("add_captions 业务失败: code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp, nil
}

// applyAddCaptionsDefaults 补齐与文档一致的默认样式参数。
// Alpha/Scale 为 0 时按文档默认值 1.0 处理（调用方若需透明应显式传极小正数）。
func applyAddCaptionsDefaults(req *AddCaptionsRequest) {
	if req.Alpha == 0 {
		req.Alpha = 1
	}
	if req.ScaleX == 0 {
		req.ScaleX = 1
	}
	if req.ScaleY == 0 {
		req.ScaleY = 1
	}
	if req.FontSize == 0 {
		req.FontSize = 13
	}
	if req.Font == "" {
		req.Font = "得意黑"
	}
	if req.TextColor == "" {
		req.TextColor = "#ffde00"
	}
	if req.BorderColor == "" {
		req.BorderColor = "#000000"
	}
}

// BuildCaptionsJSON 将字幕列表序列化为 API 所需的 JSON 字符串。
func BuildCaptionsJSON(items []CaptionItem) (string, error) {
	if items == nil {
		items = []CaptionItem{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("序列化 captions 失败: %w", err)
	}
	return string(raw), nil
}
