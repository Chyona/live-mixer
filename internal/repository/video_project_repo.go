// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"
	"time"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

// VideoProjectListFilter 剪辑项目列表查询筛选条件。
type VideoProjectListFilter struct {
	StartAt  *time.Time // 开始日期（含），按 created_at 筛选
	EndAt    *time.Time // 结束日期次日零点（不含），按 created_at 筛选
	Keywords []string   // 关键词列表，每个词匹配 name/remark，词之间为「与」
}

// VideoProjectRepository 剪辑项目数据访问接口。
type VideoProjectRepository interface {
	// Create 插入一条剪辑项目记录。
	Create(ctx context.Context, project *model.VideoProject) error
	// GetByID 根据主键查询剪辑项目。
	GetByID(ctx context.Context, id uint) (*model.VideoProject, error)
	// Update 更新可编辑字段，防止误改 live_id、created_by 等列。
	Update(ctx context.Context, project *model.VideoProject) error
	// Delete 物理删除剪辑项目记录。
	Delete(ctx context.Context, id uint) error
	// List 分页查询剪辑项目，支持日期与关键词筛选，按更新时间倒序。
	List(ctx context.Context, filter VideoProjectListFilter, offset, limit int) ([]model.VideoProject, int64, error)
}

type videoProjectRepository struct {
	db *gorm.DB
}

// NewVideoProjectRepository 创建剪辑项目仓储实例。
func NewVideoProjectRepository(db *gorm.DB) VideoProjectRepository {
	return &videoProjectRepository{db: db}
}

func (r *videoProjectRepository) Create(ctx context.Context, project *model.VideoProject) error {
	return r.db.WithContext(ctx).Create(project).Error
}

func (r *videoProjectRepository) GetByID(ctx context.Context, id uint) (*model.VideoProject, error) {
	var project model.VideoProject
	err := r.db.WithContext(ctx).First(&project, id).Error
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *videoProjectRepository) Update(ctx context.Context, project *model.VideoProject) error {
	return r.db.WithContext(ctx).
		Model(project).
		Select("name", "remark", "prompt_id", "clips0", "clips1", "draft_url", "video_url").
		Updates(project).Error
}

func (r *videoProjectRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&model.VideoProject{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *videoProjectRepository) List(ctx context.Context, filter VideoProjectListFilter, offset, limit int) ([]model.VideoProject, int64, error) {
	var projects []model.VideoProject
	var total int64

	query := r.db.WithContext(ctx).Model(&model.VideoProject{})
	query = applyVideoProjectListFilter(query, filter)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("updated_at DESC, id DESC").Find(&projects).Error; err != nil {
		return nil, 0, err
	}
	return projects, total, nil
}

// applyVideoProjectListFilter 将列表筛选条件应用到 GORM 查询。
func applyVideoProjectListFilter(query *gorm.DB, filter VideoProjectListFilter) *gorm.DB {
	if filter.StartAt != nil {
		query = query.Where("created_at >= ?", *filter.StartAt)
	}
	if filter.EndAt != nil {
		query = query.Where("created_at < ?", *filter.EndAt)
	}
	for _, kw := range filter.Keywords {
		pattern := "%" + kw + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(remark) LIKE ?", pattern, pattern)
	}
	return query
}
