// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"encoding/json"
	"errors"
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

// VideoProjectUpdateInput 剪辑项目更新入参，nil 表示不修改对应字段。
type VideoProjectUpdateInput struct {
	Name     *string
	Remark   *string
	Clips0   *string
	Clips1   *string
	DraftURL *string
	VideoURL *string
}

// VideoProjectService 剪辑项目业务接口。
type VideoProjectService interface {
	// Create 创建剪辑项目，createdBy 来自 JWT 当前用户。
	Create(ctx context.Context, createdBy uint, name, remark string, liveID uint, clips0, clips1 string) (*model.VideoProject, error)
	// Update 更新剪辑项目可编辑字段。
	Update(ctx context.Context, id uint, input VideoProjectUpdateInput) (*model.VideoProject, error)
	// Delete 删除剪辑项目。
	Delete(ctx context.Context, id uint) error
	// List 分页查询剪辑项目列表。
	List(ctx context.Context, page, pageSize int, opts VideoProjectListOptions) ([]model.VideoProject, int64, error)
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

func (s *videoProjectService) Create(ctx context.Context, createdBy uint, name, remark string, liveID uint, clips0, clips1 string) (*model.VideoProject, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("项目名称不能为空")
	}
	if liveID == 0 {
		return nil, errors.New("直播素材 ID 不能为空")
	}
	if _, err := s.liveMaterialRepo.GetByID(ctx, liveID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFoundForProject
		}
		return nil, err
	}

	clips0 = defaultJSONArray(clips0)
	clips1 = defaultJSONArray(clips1)
	if err := validateJSONClipArray("clips0", clips0); err != nil {
		return nil, err
	}
	if err := validateJSONClipArray("clips1", clips1); err != nil {
		return nil, err
	}

	project := &model.VideoProject{
		Name:      name,
		Remark:    remark,
		LiveID:    liveID,
		Clips0:    clips0,
		Clips1:    clips1,
		CreatedBy: createdBy,
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
	if input.Clips0 != nil {
		clips0 := defaultJSONArray(*input.Clips0)
		if err := validateJSONClipArray("clips0", clips0); err != nil {
			return nil, err
		}
		project.Clips0 = clips0
	}
	if input.Clips1 != nil {
		clips1 := defaultJSONArray(*input.Clips1)
		if err := validateJSONClipArray("clips1", clips1); err != nil {
			return nil, err
		}
		project.Clips1 = clips1
	}
	if input.DraftURL != nil {
		project.DraftURL = *input.DraftURL
	}
	if input.VideoURL != nil {
		project.VideoURL = *input.VideoURL
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

func (s *videoProjectService) List(ctx context.Context, page, pageSize int, opts VideoProjectListOptions) ([]model.VideoProject, int64, error) {
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

// defaultJSONArray 将空 clips 归一化为 JSON 空数组。
func defaultJSONArray(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "[]"
	}
	return raw
}

// validateJSONClipArray 校验 clips 字段为合法 JSON 数组。
func validateJSONClipArray(field, raw string) error {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return errors.New(field + " 必须是合法的 JSON 数组")
	}
	return nil
}
