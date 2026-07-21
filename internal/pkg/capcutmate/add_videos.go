package capcutmate

import (
	"context"
	"encoding/json"
	"fmt"
)

const pathAddVideos = "/openapi/capcut-mate/v1/add_videos"

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
	Code         int           `json:"code"`
	Message      string        `json:"message"`
	DraftURL     string        `json:"draft_url"`
	TrackID      string        `json:"track_id,omitempty"`
	VideoIDs     []string      `json:"video_ids,omitempty"`
	SegmentIDs   []string      `json:"segment_ids,omitempty"`
	SegmentInfos []SegmentInfo `json:"segment_infos,omitempty"`
}

// SegmentInfo 时间轴片段信息。
type SegmentInfo struct {
	ID    string `json:"id"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
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
