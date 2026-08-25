// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// 系统提示词业务错误，供 handler 映射 HTTP 状态码。
var (
	ErrLLMSystemPromptNotFound     = errors.New("系统提示词不存在")
	ErrLLMSystemPromptNotEditable  = errors.New("系统预置提示词不可修改")
	ErrLLMSystemPromptNotDeletable = errors.New("系统预置提示词不可删除")
	ErrLLMSystemPromptNameExists   = errors.New("提示词名称已存在")
)

// LLMSystemPromptListOptions 系统提示词列表查询选项（来自 HTTP 查询参数）。
type LLMSystemPromptListOptions struct {
	Keywords  string
	StartDate string
	EndDate   string
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
		return nil, mapLLMSystemPromptUniqueError(err)
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
		return nil, mapLLMSystemPromptUniqueError(err)
	}
	return prompt, nil
}

// mapLLMSystemPromptUniqueError 将 name 唯一约束冲突转为业务错误。
func mapLLMSystemPromptUniqueError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if (strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")) &&
		strings.Contains(msg, "name") {
		return ErrLLMSystemPromptNameExists
	}
	return err
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
	filter, err := buildLLMSystemPromptListFilter(opts)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	prompts, total, err := s.repo.List(ctx, filter, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}

	items := make([]model.LLMSystemPromptListItem, 0, len(prompts))
	for _, p := range prompts {
		items = append(items, model.LLMSystemPromptListItem{
			ID:         p.ID,
			Name:       p.Name,
			Content:    p.Content,
			Remark:     p.Remark,
			IsEditable: p.IsEditable,
			CreatedBy:  p.CreatedBy,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	return items, total, nil
}

// buildLLMSystemPromptListFilter 解析列表筛选参数并转换为仓储层筛选条件。
func buildLLMSystemPromptListFilter(opts LLMSystemPromptListOptions) (repository.LLMSystemPromptListFilter, error) {
	filter := repository.LLMSystemPromptListFilter{
		Keywords: parseKeywordExpr(opts.Keywords),
	}

	if raw := strings.TrimSpace(opts.StartDate); raw != "" {
		startAt, err := time.ParseInLocation(liveMaterialListDateLayout, raw, time.UTC)
		if err != nil {
			return filter, errors.New("start_date 格式无效，应为 YYYY-MM-DD")
		}
		filter.StartAt = &startAt
	}
	if raw := strings.TrimSpace(opts.EndDate); raw != "" {
		endDate, err := time.ParseInLocation(liveMaterialListDateLayout, raw, time.UTC)
		if err != nil {
			return filter, errors.New("end_date 格式无效，应为 YYYY-MM-DD")
		}
		endAt := endDate.Add(24 * time.Hour)
		filter.EndAt = &endAt
	}
	if filter.StartAt != nil && filter.EndAt != nil && !filter.StartAt.Before(*filter.EndAt) {
		return filter, errors.New("start_date 不能晚于 end_date")
	}
	return filter, nil
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
