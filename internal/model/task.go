package model

import (
	"time"
)

// 任务类型常量。
const (
	TaskTypeJianyingDraft      int8 = 1 // 剪映草稿生成任务
	TaskTypeAISlice            int8 = 2 // AI 切片任务
	TaskTypeAISliceJianying    int8 = 3 // AI 切片 + 剪映草稿生成任务
)

// 任务状态常量。
const (
	TaskStatusPending   int8 = 0 // 待处理
	TaskStatusRunning   int8 = 1 // 执行中
	TaskStatusSuccess   int8 = 2 // 成功
	TaskStatusFailed    int8 = 3 // 失败
	TaskStatusCancelled int8 = 4 // 已取消
)

// Task 异步任务实体。
type Task struct {
	ID                  uint       `gorm:"primaryKey;comment:主键" json:"id"`
	Type                int8       `gorm:"not null;index;comment:任务类型" json:"type"`
	Status              int8       `gorm:"not null;default:0;index;comment:任务状态" json:"status"`
	LiveMaterialID      *uint      `gorm:"index;comment:源直播素材ID" json:"live_material_id,omitempty"`
	EditProjectID       *uint      `gorm:"index;comment:源剪辑项目ID" json:"edit_project_id,omitempty"`
	ResultEditProjectID *uint      `gorm:"comment:产出剪辑项目ID" json:"result_edit_project_id,omitempty"`
	ResultDraftURL      string     `gorm:"size:1024;comment:产出剪映草稿地址" json:"result_draft_url,omitempty"`
	PromptID            *uint      `gorm:"comment:使用的大模型提示词ID" json:"prompt_id,omitempty"`
	ErrorMessage        string     `gorm:"type:text;comment:失败原因" json:"error_message,omitempty"`
	CreatedBy           uint       `gorm:"not null;index;comment:任务创建人账号ID" json:"created_by"`
	CreatedAt           time.Time  `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"comment:更新时间" json:"updated_at"`
	StartedAt           *time.Time `gorm:"comment:开始执行时间" json:"started_at,omitempty"`
	FinishedAt          *time.Time `gorm:"comment:结束时间" json:"finished_at,omitempty"`
}

// TableName 指定任务表名。
func (Task) TableName() string {
	return "task"
}
