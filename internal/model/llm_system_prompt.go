package model

import (
	"time"

	"gorm.io/gorm"
)

// LLMSystemPrompt 大模型系统提示词实体。
type LLMSystemPrompt struct {
	ID         uint           `gorm:"primaryKey;comment:主键" json:"id"`
	Name       string         `gorm:"size:128;not null;comment:提示词名称" json:"name"`
	Content    string         `gorm:"type:text;not null;comment:提示词内容" json:"content"`
	IsSystem   bool           `gorm:"not null;default:false;index;comment:是否系统默认提示词" json:"is_system"`
	IsEditable bool           `gorm:"not null;default:true;comment:是否允许修改" json:"is_editable"`
	CreatedBy  uint           `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	UpdatedBy  uint           `gorm:"not null;comment:最后修改人账号ID" json:"updated_by"`
	CreatedAt  time.Time      `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"comment:更新时间" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index;comment:软删除时间" json:"-"`
}

// TableName 指定大模型系统提示词表名。
func (LLMSystemPrompt) TableName() string {
	return "llm_system_prompt"
}
