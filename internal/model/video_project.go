package model

import (
	"time"
)

// DefaultVideoProjectPromptID 剪辑项目默认提示词 ID。
const DefaultVideoProjectPromptID uint = 1

// VideoProject 剪辑项目实体。
type VideoProject struct {
	ID        uint      `gorm:"primaryKey;comment:主键" json:"id"`
	Name      string    `gorm:"size:64;comment:项目名称" json:"name"`
	Remark    string    `gorm:"size:256;comment:备注" json:"remark"`
	LiveID    uint      `gorm:"column:live_id;not null;index;comment:关联直播素材ID" json:"live_id"`
	// PromptID 提示词 ID（对应 llm_system_prompt.id），无外键约束，默认 1。
	PromptID  uint      `gorm:"column:prompt_id;not null;default:1;index;comment:提示词ID" json:"prompt_id"`
	Clips0    string    `gorm:"column:clips0;type:jsonb;not null;default:'[]';comment:视频切片列表毫秒" json:"clips0"`
	Clips1    string    `gorm:"column:clips1;type:jsonb;not null;default:'[]';comment:带文本与词级时间戳的切片列表毫秒" json:"clips1"`
	DraftURL  string    `gorm:"column:draft_url;size:1024;comment:剪映草稿URL" json:"draft_url,omitempty"`
	VideoURL  string    `gorm:"column:video_url;size:1024;comment:视频地址URL" json:"video_url,omitempty"`
	CreatedBy uint      `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	CreatedAt time.Time `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt time.Time `gorm:"comment:最后编辑时间" json:"updated_at"`
	Ext       string    `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// TableName 指定剪辑项目表名。
func (VideoProject) TableName() string {
	return "video_project"
}
