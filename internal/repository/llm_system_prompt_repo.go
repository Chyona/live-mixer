// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"
	"strings"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

// LLMSystemPromptListFilter 系统提示词列表查询筛选条件。
type LLMSystemPromptListFilter struct {
	Name       string // 名称模糊匹配，空字符串表示不过滤
	IsEditable *int8  // 是否可编辑筛选，nil 表示不过滤
}

// LLMSystemPromptRepository 大模型系统提示词数据访问接口。
type LLMSystemPromptRepository interface {
	// Create 插入一条系统提示词记录。
	Create(ctx context.Context, prompt *model.LLMSystemPrompt) error
	// GetByID 根据主键查询系统提示词。
	GetByID(ctx context.Context, id uint) (*model.LLMSystemPrompt, error)
	// Update 更新可编辑字段（name、content、remark、ext），防止误改其它列。
	Update(ctx context.Context, prompt *model.LLMSystemPrompt) error
	// Delete 物理删除系统提示词记录。
	Delete(ctx context.Context, id uint) error
	// List 分页查询系统提示词，支持名称与可编辑状态筛选，按更新时间倒序。
	List(ctx context.Context, filter LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error)
}

type llmSystemPromptRepository struct {
	db *gorm.DB
}

// NewLLMSystemPromptRepository 创建系统提示词仓储实例。
func NewLLMSystemPromptRepository(db *gorm.DB) LLMSystemPromptRepository {
	return &llmSystemPromptRepository{db: db}
}

func (r *llmSystemPromptRepository) Create(ctx context.Context, prompt *model.LLMSystemPrompt) error {
	return r.db.WithContext(ctx).Create(prompt).Error
}

func (r *llmSystemPromptRepository) GetByID(ctx context.Context, id uint) (*model.LLMSystemPrompt, error) {
	var prompt model.LLMSystemPrompt
	err := r.db.WithContext(ctx).First(&prompt, id).Error
	if err != nil {
		return nil, err
	}
	return &prompt, nil
}

func (r *llmSystemPromptRepository) Update(ctx context.Context, prompt *model.LLMSystemPrompt) error {
	// Select 限定更新列，确保 is_editable、created_by 等字段不会被意外覆盖。
	return r.db.WithContext(ctx).
		Model(prompt).
		Select("name", "content", "remark", "ext").
		Updates(prompt).Error
}

func (r *llmSystemPromptRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.LLMSystemPrompt{}, id).Error
}

func (r *llmSystemPromptRepository) List(ctx context.Context, filter LLMSystemPromptListFilter, offset, limit int) ([]model.LLMSystemPrompt, int64, error) {
	var prompts []model.LLMSystemPrompt
	var total int64

	query := r.db.WithContext(ctx).Model(&model.LLMSystemPrompt{})
	if name := strings.TrimSpace(filter.Name); name != "" {
		// 使用 LOWER + LIKE，兼容 PostgreSQL 与 SQLite 测试库。
		query = query.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(name)+"%")
	}
	if filter.IsEditable != nil {
		query = query.Where("is_editable = ?", *filter.IsEditable)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("updated_at DESC, id DESC").Find(&prompts).Error; err != nil {
		return nil, 0, err
	}
	return prompts, total, nil
}
