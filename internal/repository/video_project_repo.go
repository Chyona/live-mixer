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
	Keywords KeywordGroups // 关键词表达式：组内 AND、组间 OR；匹配 name/remark/live_name
	LiveID   *uint         // 按关联直播素材 ID 精确筛选
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
	// List 分页查询剪辑项目，LEFT JOIN live_material 取素材名称，支持日期与关键词筛选，按更新时间倒序。
	List(ctx context.Context, filter VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error)
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
		Select("name", "remark", "prompt_id", "clips0", "clips1", "width", "height", "project_source", "enable_captions").
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

func (r *videoProjectRepository) List(ctx context.Context, filter VideoProjectListFilter, offset, limit int) ([]model.VideoProjectListItem, int64, error) {
	var items []model.VideoProjectListItem
	var total int64

	// Count 与列表共用 JOIN，以便 keywords 可匹配 live_material.name。
	countQuery := r.db.WithContext(ctx).
		Table("video_project").
		Joins("LEFT JOIN live_material ON live_material.id = video_project.live_id")
	countQuery = applyVideoProjectListFilter(countQuery, filter)
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query := r.db.WithContext(ctx).
		Table("video_project").
		Select("video_project.*, live_material.name AS live_name, (SELECT COUNT(*) FROM task WHERE task.video_project_id = video_project.id) AS task_count").
		Joins("LEFT JOIN live_material ON live_material.id = video_project.live_id")
	query = applyVideoProjectListFilter(query, filter)
	if err := query.Offset(offset).Limit(limit).Order("video_project.updated_at DESC, video_project.id DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// applyVideoProjectListFilter 将列表筛选条件应用到 GORM 查询（列名带表前缀，避免 JOIN 歧义）。
// 调用方需已 LEFT JOIN live_material，以便 keywords 可匹配源视频名。
func applyVideoProjectListFilter(query *gorm.DB, filter VideoProjectListFilter) *gorm.DB {
	const table = "video_project"
	if filter.StartAt != nil {
		query = query.Where(table+".created_at >= ?", *filter.StartAt)
	}
	if filter.EndAt != nil {
		query = query.Where(table+".created_at < ?", *filter.EndAt)
	}
	if filter.LiveID != nil {
		query = query.Where(table+".live_id = ?", *filter.LiveID)
	}
	query = applyKeywordGroups(
		query,
		filter.Keywords,
		"LOWER("+table+".name) LIKE ? OR LOWER("+table+".remark) LIKE ? OR LOWER(live_material.name) LIKE ?",
		3,
	)
	return query
}
