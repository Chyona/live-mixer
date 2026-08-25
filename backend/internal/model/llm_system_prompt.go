package model

import (
	"time"
)

// LLMSystemPrompt 大模型系统提示词实体。
type LLMSystemPrompt struct {
	ID         uint           `gorm:"primaryKey;comment:主键" json:"id"`
	Name       string         `gorm:"size:128;not null;uniqueIndex;comment:提示词名称" json:"name"`
	Content    string         `gorm:"type:text;not null;comment:提示词内容" json:"content"`
	Remark     string    `gorm:"size:256;comment:备注" json:"remark"`
	IsEditable int8      `gorm:"not null;default:1;comment:是否允许修改0否1是" json:"is_editable"`
	CreatedBy  uint      `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	CreatedAt  time.Time `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt  time.Time `gorm:"comment:更新时间" json:"updated_at"`
	Ext        string    `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// TableName 指定大模型系统提示词表名。
func (LLMSystemPrompt) TableName() string {
	return "llm_system_prompt"
}
