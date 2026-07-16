package model

import (
	"time"
)

// 任务类型常量（字符串标识）。
const (
	TaskTypeAISlice      = "ai_slice"       // AI 切片任务
	TaskTypeDraft        = "draft"          // 剪映草稿生成任务
	TaskTypeAISliceDraft = "ai_slice_draft" // AI 切片 + 剪映草稿生成任务
)

// 任务状态常量（字符串标识）。
const (
	TaskStatusPending    = "pending"    // 待处理
	TaskStatusProcessing = "processing" // 执行中
	TaskStatusCompleted  = "completed"  // 已完成
	TaskStatusFailed     = "failed"     // 失败
)

// Task 异步任务实体。
type Task struct {
	ID             uint       `gorm:"primaryKey;comment:主键" json:"id"`
	Type           string     `gorm:"size:32;not null;index;comment:任务类型" json:"type"`
	Status         string     `gorm:"size:32;not null;default:pending;index;comment:任务状态" json:"status"`
	Progress       int16      `gorm:"not null;default:0;comment:任务进度0-100" json:"progress"`
	SysPrompt      string     `gorm:"column:sys_prompt;type:text;comment:系统提示词" json:"sys_prompt,omitempty"`
	UsrPrompt      string     `gorm:"column:usr_prompt;type:text;comment:用户提示词" json:"usr_prompt,omitempty"`
	ErrorMessage     string     `gorm:"type:text;comment:失败原因" json:"error_message,omitempty"`
	VideoProjectID   *uint      `gorm:"column:video_project_id;index;comment:关联剪辑项目ID" json:"video_project_id,omitempty"`
	VideoProjectName string     `gorm:"column:video_project_name;size:64;not null;default:'';comment:关联剪辑项目名称" json:"video_project_name"`
	// DraftURL 剪映草稿地址：草稿生成与一键成片 Worker 成功后回写。
	DraftURL string `gorm:"column:draft_url;size:1024;comment:剪映草稿URL" json:"draft_url"`
	// VideoURL 成片/导出视频地址，可由客户端通过更新接口写入。
	VideoURL  string `gorm:"column:video_url;size:1024;comment:视频地址URL" json:"video_url"`
	CreatedBy uint   `gorm:"not null;index;comment:任务创建人账号ID" json:"created_by"`
	CreatedAt      time.Time  `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"comment:更新时间" json:"updated_at"`
	StartedAt      *time.Time `gorm:"comment:开始执行时间" json:"started_at,omitempty"`
	CompletedAt    *time.Time `gorm:"column:completed_at;comment:完成时间" json:"completed_at,omitempty"`
	Ext            string     `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// TableName 指定任务表名。
func (Task) TableName() string {
	return "task"
}

// NewUintPtr 返回非零 uint 的指针；v 为 0 时返回 nil（用于可空外键）。
func NewUintPtr(v uint) *uint {
	if v == 0 {
		return nil
	}
	return &v
}

// UintValue 解引用可空 uint；nil 时返回 0。
func UintValue(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}

// ClipRange 视频时间片段（毫秒）。
type ClipRange struct {
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
}

// ClipWord 词级时间戳（毫秒），用于 clips1 中可选的 words 列表。
type ClipWord struct {
	Text      string `json:"text"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

// ClipWithText 带文本的切片片段（毫秒），对应 video_project.clips1。
// Words 可选：创建/更新接口可不传；AI 切片 Worker 会写入词级时间戳。
type ClipWithText struct {
	Text      string     `json:"text"`
	StartTime int64      `json:"start_time"`
	EndTime   int64      `json:"end_time"`
	Words     []ClipWord `json:"words,omitempty"`
}
