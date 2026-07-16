// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// ErrVideoProjectNotFound 剪辑项目不存在。
var ErrVideoProjectNotFound = errors.New("剪辑项目不存在")

// ErrLiveMaterialNotFoundForProject 创建剪辑项目时关联的直播素材不存在。
var ErrLiveMaterialNotFoundForProject = errors.New("关联的直播素材不存在")

// VideoProjectListOptions 剪辑项目列表查询选项（来自 HTTP 查询参数）。
type VideoProjectListOptions struct {
	Keywords  string
	StartDate string
	EndDate   string
}

// CreateVideoProjectInput 创建剪辑项目入参。
// Clips0 / Clips1 为可选：nil 或空切片均写入 JSON 空数组 []。
// ProjectSource 可选，未传时为空字符串。
type CreateVideoProjectInput struct {
	Name          string
	Remark        string
	LiveID        uint
	PromptID      uint
	Clips0        []model.ClipRange
	Clips1        []model.ClipWithText
	ProjectSource string
}

// VideoProjectUpdateInput 剪辑项目更新入参。
// 指针字段为 nil 表示「请求未传该字段，保持原值」；非 nil 则校验通过后写入。
type VideoProjectUpdateInput struct {
	Name          *string
	Remark        *string
	PromptID      *uint
	Clips0        *[]model.ClipRange
	Clips1        *[]model.ClipWithText
	ProjectSource *string
}

// VideoProjectService 剪辑项目业务接口。
type VideoProjectService interface {
	// Create 创建剪辑项目，createdBy 来自 JWT 当前用户；promptID 为 0 时使用默认值 1。
	Create(ctx context.Context, createdBy uint, input CreateVideoProjectInput) (*model.VideoProject, error)
	// Update 更新剪辑项目可编辑字段（仅更新请求中显式传入且合法的字段）。
	Update(ctx context.Context, id uint, input VideoProjectUpdateInput) (*model.VideoProject, error)
	// Delete 删除剪辑项目。
	Delete(ctx context.Context, id uint) error
	// List 分页查询剪辑项目列表（含关联直播素材名称 live_name）。
	List(ctx context.Context, page, pageSize int, opts VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error)
	// Get 根据 ID 获取剪辑项目详情。
	Get(ctx context.Context, id uint) (*model.VideoProject, error)
}

type videoProjectService struct {
	videoProjectRepo repository.VideoProjectRepository
	liveMaterialRepo repository.LiveMaterialRepository
}

// NewVideoProjectService 创建剪辑项目业务服务实例。
func NewVideoProjectService(videoProjectRepo repository.VideoProjectRepository, liveMaterialRepo repository.LiveMaterialRepository) VideoProjectService {
	return &videoProjectService{
		videoProjectRepo: videoProjectRepo,
		liveMaterialRepo: liveMaterialRepo,
	}
}

func (s *videoProjectService) Create(ctx context.Context, createdBy uint, input CreateVideoProjectInput) (*model.VideoProject, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("项目名称不能为空")
	}
	if input.LiveID == 0 {
		return nil, errors.New("直播素材 ID 不能为空")
	}
	if _, err := s.liveMaterialRepo.GetByID(ctx, input.LiveID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFoundForProject
		}
		return nil, err
	}

	promptID := input.PromptID
	if promptID == 0 {
		promptID = model.DefaultVideoProjectPromptID
	}

	clips0, err := normalizeAndValidateClips0(input.Clips0)
	if err != nil {
		return nil, err
	}
	clips1, err := normalizeAndValidateClips1(input.Clips1)
	if err != nil {
		return nil, err
	}

	project := &model.VideoProject{
		Name:          name,
		Remark:        input.Remark,
		LiveID:        input.LiveID,
		PromptID:      promptID,
		Clips0:        clips0,
		Clips1:        clips1,
		ProjectSource: strings.TrimSpace(input.ProjectSource),
		CreatedBy:     createdBy,
	}
	if err := s.videoProjectRepo.Create(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *videoProjectService) Update(ctx context.Context, id uint, input VideoProjectUpdateInput) (*model.VideoProject, error) {
	project, err := s.videoProjectRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}

	// 以下各字段均为「传了才更新」：指针 nil 表示请求未携带该字段。
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, errors.New("项目名称不能为空")
		}
		project.Name = name
	}
	if input.Remark != nil {
		project.Remark = *input.Remark
	}
	if input.PromptID != nil {
		promptID := *input.PromptID
		if promptID == 0 {
			promptID = model.DefaultVideoProjectPromptID
		}
		project.PromptID = promptID
	}
	if input.Clips0 != nil {
		clips0, err := normalizeAndValidateClips0(*input.Clips0)
		if err != nil {
			return nil, err
		}
		project.Clips0 = clips0
	}
	if input.Clips1 != nil {
		clips1, err := normalizeAndValidateClips1(*input.Clips1)
		if err != nil {
			return nil, err
		}
		project.Clips1 = clips1
	}
	if input.ProjectSource != nil {
		project.ProjectSource = strings.TrimSpace(*input.ProjectSource)
	}

	if err := s.videoProjectRepo.Update(ctx, project); err != nil {
		return nil, err
	}
	return project, nil
}

func (s *videoProjectService) Delete(ctx context.Context, id uint) error {
	if err := s.videoProjectRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoProjectNotFound
		}
		return err
	}
	return nil
}

func (s *videoProjectService) List(ctx context.Context, page, pageSize int, opts VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error) {
	filter, err := buildVideoProjectListFilter(opts)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	return s.videoProjectRepo.List(ctx, filter, offset, pageSize)
}

func (s *videoProjectService) Get(ctx context.Context, id uint) (*model.VideoProject, error) {
	project, err := s.videoProjectRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}
	return project, nil
}

// buildVideoProjectListFilter 解析列表筛选参数并转换为仓储层筛选条件。
func buildVideoProjectListFilter(opts VideoProjectListOptions) (repository.VideoProjectListFilter, error) {
	filter := repository.VideoProjectListFilter{
		Keywords: parseCommaKeywords(opts.Keywords),
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

// normalizeAndValidateClips0 校验并规范化 clips0（nil 转为空切片）。
func normalizeAndValidateClips0(clips []model.ClipRange) ([]model.ClipRange, error) {
	if clips == nil {
		clips = []model.ClipRange{}
	}
	for i, clip := range clips {
		if clip.StartTime < 0 || clip.EndTime <= clip.StartTime {
			return nil, fmt.Errorf("clips0[%d] 时间段无效：start_time 须小于 end_time 且均非负", i)
		}
	}
	return clips, nil
}

// normalizeAndValidateClips1 校验并规范化 clips1（nil 转为空切片）。
func normalizeAndValidateClips1(clips []model.ClipWithText) ([]model.ClipWithText, error) {
	if clips == nil {
		clips = []model.ClipWithText{}
	}
	for i, clip := range clips {
		if clip.StartTime < 0 || clip.EndTime <= clip.StartTime {
			return nil, fmt.Errorf("clips1[%d] 时间段无效：start_time 须小于 end_time 且均非负", i)
		}
	}
	return clips, nil
}
