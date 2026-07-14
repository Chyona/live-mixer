// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

// TaskListFilter 任务列表查询筛选条件。
type TaskListFilter struct {
	Type   string // 任务类型，空表示不限
	Status string // 任务状态，空表示不限
}

// TaskRepository 异步任务数据访问接口。
type TaskRepository interface {
	// Create 插入一条任务记录。
	Create(ctx context.Context, task *model.Task) error
	// GetByID 根据主键查询任务。
	GetByID(ctx context.Context, id uint) (*model.Task, error)
	// List 分页查询任务列表，按 id 倒序。
	List(ctx context.Context, filter TaskListFilter, offset, limit int) ([]model.Task, int64, error)
}

type taskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 创建任务仓储实例。
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(ctx context.Context, task *model.Task) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *taskRepository) GetByID(ctx context.Context, id uint) (*model.Task, error) {
	var task model.Task
	err := r.db.WithContext(ctx).First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepository) List(ctx context.Context, filter TaskListFilter, offset, limit int) ([]model.Task, int64, error) {
	var tasks []model.Task
	var total int64

	query := r.db.WithContext(ctx).Model(&model.Task{})
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("id DESC").Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}
