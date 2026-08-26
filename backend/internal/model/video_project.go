package model

import (
	"time"
)

// DefaultVideoProjectPromptID 剪辑项目默认提示词 ID。
const DefaultVideoProjectPromptID uint = 1

// VideoProject 剪辑项目实体。
type VideoProject struct {
	ID     uint   `gorm:"primaryKey;comment:主键" json:"id"`
	Name   string `gorm:"size:64;uniqueIndex;comment:项目名称" json:"name"`
	Remark string `gorm:"size:256;comment:备注" json:"remark"`
	LiveID uint   `gorm:"column:live_id;not null;index;comment:关联直播素材ID" json:"live_id"`
	// PromptID 提示词 ID（对应 llm_system_prompt.id），无外键约束，默认 1。
	PromptID uint           `gorm:"column:prompt_id;not null;default:1;index;comment:提示词ID" json:"prompt_id"`
	Clips0   []ClipRange    `gorm:"column:clips0;serializer:json;type:jsonb;not null;default:'[]';comment:视频切片列表毫秒" json:"clips0"`
	Clips1   []ClipWithText `gorm:"column:clips1;serializer:json;type:jsonb;not null;default:'[]';comment:带文本与词级时间戳的切片列表毫秒" json:"clips1"`
	// Width / Height 剪映草稿工程画布分辨率（像素）；0 表示未设置。
	Width         int    `gorm:"not null;default:0;comment:剪映草稿工程宽度像素" json:"width"`
	Height        int    `gorm:"not null;default:0;comment:剪映草稿工程高度像素" json:"height"`
	ProjectSource string `gorm:"column:project_source;size:32;not null;default:'';comment:项目来源" json:"project_source"`
	// EnableCaptions 是否添加字幕：0否 1是，默认 1。
	EnableCaptions int `gorm:"column:enable_captions;not null;default:1;comment:是否添加字幕0否1是" json:"enable_captions"`
	// Title 短视频标题，2～12 个汉字；空表示尚未生成。
	Title string `gorm:"size:48;not null;default:'';comment:短视频标题" json:"title"`
	// Description 短视频内容介绍，128 个字以内；空表示尚未生成。
	Description string `gorm:"size:384;not null;default:'';comment:短视频内容介绍" json:"description"`
	// Topics 短视频话题，每个 2～12 个汉字，共 2～6 个；空数组表示尚未生成。
	Topics    []string  `gorm:"column:topics;serializer:json;type:jsonb;not null;default:'[]';comment:短视频话题" json:"topics"`
	CreatedBy uint      `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	CreatedAt time.Time `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt time.Time `gorm:"comment:最后编辑时间" json:"updated_at"`
	Ext       string    `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// EnableCaptions 取值：0=不添加字幕，1=添加字幕。
const (
	EnableCaptionsOff = 0
	EnableCaptionsOn  = 1
)

// 短视频标题 / 描述 / 话题的字数与数量约束（按 Unicode 码点计）。
const (
	VideoProjectTitleMinRunes       = 2
	VideoProjectTitleMaxRunes       = 12
	VideoProjectDescriptionMaxRunes = 128
	VideoProjectTopicMinRunes       = 2
	VideoProjectTopicMaxRunes       = 12
	VideoProjectTopicsMinCount      = 2
	VideoProjectTopicsMaxCount      = 6
)

// TableName 指定剪辑项目表名。
func (VideoProject) TableName() string {
	return "video_project"
}
