package model

import "time"

// LLMSystemPromptEditable 表示用户可编辑的系统提示词。
const LLMSystemPromptEditable int8 = 1

// LLMSystemPromptNotEditable 表示系统预置、不可修改或删除的提示词。
const LLMSystemPromptNotEditable int8 = 0

// LLMSystemPromptListItem 系统提示词列表项。
type LLMSystemPromptListItem struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Content    string    `json:"content"`
	Remark     string    `json:"remark"`
	IsEditable int8      `json:"is_editable"`
	CreatedBy  uint      `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
