package model

import (
	"time"
)

// VideoProjectListItem 剪辑项目列表项。
// 在 live_id 之外额外带上关联直播素材名称 live_name。
type VideoProjectListItem struct {
	ID            uint           `json:"id"`
	Name          string         `json:"name"`
	Remark        string         `json:"remark"`
	LiveID        uint           `json:"live_id"`
	LiveName      string         `gorm:"column:live_name" json:"live_name"`
	PromptID      uint           `json:"prompt_id"`
	Clips0        []ClipRange    `gorm:"serializer:json" json:"clips0"`
	Clips1        []ClipWithText `gorm:"serializer:json" json:"clips1"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	ProjectSource string         `json:"project_source"`
	EnableCaptions int           `json:"enable_captions"`
	CreatedBy     uint           `json:"created_by"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Ext           string         `json:"ext"`
	// TaskCount 关联 task 数量（列表查询时由子查询填充，非表字段）。
	TaskCount int64 `gorm:"->" json:"task_count"`
}
