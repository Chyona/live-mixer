// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// 系统提示词业务错误，供 handler 映射 HTTP 状态码。
var (
	ErrLLMSystemPromptNotFound     = errors.New("系统提示词不存在")
	ErrLLMSystemPromptNotEditable  = errors.New("系统预置提示词不可修改")
	ErrLLMSystemPromptNotDeletable = errors.New("系统预置提示词不可删除")
)

// llmSystemPromptContentPreviewRunes 列表接口中 content 预览的最大字符数（按 rune 计）。
const llmSystemPromptContentPreviewRunes = 100

// LLMSystemPromptListOptions 系统提示词列表查询选项。
type LLMSystemPromptListOptions struct {
	Name       string
	IsEditable *int8
}

// LLMSystemPromptService 大模型系统提示词业务接口。
type LLMSystemPromptService interface {
	// Create 创建系统提示词，createdBy 来自 JWT 当前用户。
	Create(ctx context.Context, createdBy uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error)
	// Update 更新系统提示词，is_editable=0 时拒绝修改。
	Update(ctx context.Context, id uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error)
	// Delete 删除系统提示词，is_editable=0 时拒绝删除。
	Delete(ctx context.Context, id uint) error
	// List 分页查询系统提示词列表，返回预览内容而非全文。
	List(ctx context.Context, page, pageSize int, opts LLMSystemPromptListOptions) ([]model.LLMSystemPromptListItem, int64, error)
	// Get 根据 ID 获取系统提示词完整信息。
	Get(ctx context.Context, id uint) (*model.LLMSystemPrompt, error)
}

type llmSystemPromptService struct {
	repo repository.LLMSystemPromptRepository
}

// NewLLMSystemPromptService 创建系统提示词业务服务实例。
func NewLLMSystemPromptService(repo repository.LLMSystemPromptRepository) LLMSystemPromptService {
	return &llmSystemPromptService{repo: repo}
}

func (s *llmSystemPromptService) Create(ctx context.Context, createdBy uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error) {
	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)
	if name == "" {
		return nil, errors.New("提示词名称不能为空")
	}
	if content == "" {
		return nil, errors.New("提示词内容不能为空")
	}

	prompt := &model.LLMSystemPrompt{
		Name:       name,
		Content:    content,
		Remark:     remark,
		Ext:        ext,
		IsEditable: model.LLMSystemPromptEditable,
		CreatedBy:  createdBy,
	}
	if err := s.repo.Create(ctx, prompt); err != nil {
		return nil, err
	}
	return prompt, nil
}

func (s *llmSystemPromptService) Update(ctx context.Context, id uint, name, content, remark, ext string) (*model.LLMSystemPrompt, error) {
	prompt, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLLMSystemPromptNotFound
		}
		return nil, err
	}
	if prompt.IsEditable == model.LLMSystemPromptNotEditable {
		return nil, ErrLLMSystemPromptNotEditable
	}

	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)
	if name == "" {
		return nil, errors.New("提示词名称不能为空")
	}
	if content == "" {
		return nil, errors.New("提示词内容不能为空")
	}

	prompt.Name = name
	prompt.Content = content
	prompt.Remark = remark
	prompt.Ext = ext

	if err := s.repo.Update(ctx, prompt); err != nil {
		return nil, err
	}
	return prompt, nil
}

func (s *llmSystemPromptService) Delete(ctx context.Context, id uint) error {
	prompt, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLLMSystemPromptNotFound
		}
		return err
	}
	if prompt.IsEditable == model.LLMSystemPromptNotEditable {
		return ErrLLMSystemPromptNotDeletable
	}
	return s.repo.Delete(ctx, id)
}

func (s *llmSystemPromptService) List(ctx context.Context, page, pageSize int, opts LLMSystemPromptListOptions) ([]model.LLMSystemPromptListItem, int64, error) {
	offset := (page - 1) * pageSize
	filter := repository.LLMSystemPromptListFilter{
		Name:       opts.Name,
		IsEditable: opts.IsEditable,
	}
	prompts, total, err := s.repo.List(ctx, filter, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}

	items := make([]model.LLMSystemPromptListItem, 0, len(prompts))
	for _, p := range prompts {
		items = append(items, model.LLMSystemPromptListItem{
			ID:             p.ID,
			Name:           p.Name,
			ContentPreview: llmSystemPromptContentPreview(p.Content),
			Remark:         p.Remark,
			IsEditable:     p.IsEditable,
			CreatedBy:      p.CreatedBy,
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
		})
	}
	return items, total, nil
}

func (s *llmSystemPromptService) Get(ctx context.Context, id uint) (*model.LLMSystemPrompt, error) {
	prompt, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLLMSystemPromptNotFound
		}
		return nil, err
	}
	return prompt, nil
}

// llmSystemPromptContentPreview 截取提示词内容预览，超出长度时追加省略号。
func llmSystemPromptContentPreview(content string) string {
	if utf8.RuneCountInString(content) <= llmSystemPromptContentPreviewRunes {
		return content
	}
	runes := []rune(content)
	return string(runes[:llmSystemPromptContentPreviewRunes]) + "…"
}
