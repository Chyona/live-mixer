package model

import (
	"time"
)

// 任务类型常量（字符串标识）。
const (
	TaskTypeJianyingDraft   = "jianying_draft"    // 剪映草稿生成任务
	TaskTypeAISlice         = "ai_slice"          // AI 切片任务
	TaskTypeAISliceJianying = "ai_slice_jianying" // AI 切片 + 剪映草稿生成任务
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
	ID           uint       `gorm:"primaryKey;comment:主键" json:"id"`
	Type         string     `gorm:"size:32;not null;index;comment:任务类型" json:"type"`
	Status       string     `gorm:"size:32;not null;default:pending;index;comment:任务状态" json:"status"`
	SysPrompt    string     `gorm:"column:sys_prompt;type:text;comment:系统提示词" json:"sys_prompt,omitempty"`
	UsrPrompt    string     `gorm:"column:usr_prompt;type:text;comment:用户提示词" json:"usr_prompt,omitempty"`
	ErrorMessage string     `gorm:"type:text;comment:失败原因" json:"error_message,omitempty"`
	CreatedBy    uint       `gorm:"not null;index;comment:任务创建人账号ID" json:"created_by"`
	CreatedAt    time.Time  `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"comment:更新时间" json:"updated_at"`
	StartedAt    *time.Time `gorm:"comment:开始执行时间" json:"started_at,omitempty"`
	CompletedAt  *time.Time `gorm:"column:completed_at;comment:完成时间" json:"completed_at,omitempty"`
}

// TableName 指定任务表名。
func (Task) TableName() string {
	return "task"
}
