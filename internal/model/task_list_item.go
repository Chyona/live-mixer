package model

import (
	"time"
)

// TaskListItem 任务列表项。
// 在 task 字段之外，通过 JOIN 附带 live_material.live_url 与 video_project.width/height。
type TaskListItem struct {
	ID               string     `json:"id"`
	Type             string     `json:"type"`
	Status           string     `json:"status"`
	Progress         int16      `json:"progress"`
	Version          int64      `json:"version"`
	SysPrompt        string     `gorm:"column:sys_prompt" json:"sys_prompt,omitempty"`
	UsrPrompt        string     `gorm:"column:usr_prompt" json:"usr_prompt,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	VideoProjectID   *uint      `gorm:"column:video_project_id" json:"video_project_id,omitempty"`
	VideoProjectName string     `gorm:"column:video_project_name" json:"video_project_name"`
	DraftURL         string     `gorm:"column:draft_url" json:"draft_url"`
	VideoURL         string     `gorm:"column:video_url" json:"video_url"`
	// LiveURL 关联直播素材链接（live_material.live_url）；无关联项目或素材已删时为空。
	LiveURL string `gorm:"column:live_url" json:"live_url"`
	// Width / Height 关联剪辑项目画布尺寸（video_project.width/height）；无关联时为 0。
	Width       int        `gorm:"column:width" json:"width"`
	Height      int        `gorm:"column:height" json:"height"`
	CreatedBy   uint       `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
	Ext         string     `json:"ext"`
}
