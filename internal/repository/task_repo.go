// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"
	"errors"
	"time"

	"live-mixer/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TaskListFilter 任务列表查询筛选条件。
type TaskListFilter struct {
	Type     string     // 任务类型，空表示不限
	Status   string     // 任务状态，空表示不限
	StartAt  *time.Time // 开始日期（含），按 created_at 筛选
	EndAt    *time.Time // 结束日期次日零点（不含），按 created_at 筛选
	Keywords []string   // 关键词（已规范化小写），模糊匹配 video_project_name，多词 AND
}

// TaskRepository 异步任务数据访问接口。
type TaskRepository interface {
	// Create 插入一条任务记录。
	Create(ctx context.Context, task *model.Task) error
	// GetByID 根据主键查询任务。
	GetByID(ctx context.Context, id uint) (*model.Task, error)
	// List 分页查询任务列表，按 id 倒序。
	List(ctx context.Context, filter TaskListFilter, offset, limit int) ([]model.Task, int64, error)
	// ClaimPendingByType 多实例安全地抢占一条指定类型的 pending 任务。
	// 使用事务 + FOR UPDATE SKIP LOCKED（Postgres），将状态改为 processing 并返回；无待处理任务时返回 nil。
	ClaimPendingByType(ctx context.Context, taskType string) (*model.Task, error)
	// UpdateProgress 更新任务进度（0-100），仅 processing 状态时生效。
	UpdateProgress(ctx context.Context, id uint, progress int16) error
	// MarkCompleted 标记任务成功完成，并写入最终进度与扩展字段。
	MarkCompleted(ctx context.Context, id uint, progress int16, ext string) error
	// MarkFailed 标记任务失败，写入错误信息与当前进度。
	MarkFailed(ctx context.Context, id uint, progress int16, errorMsg string) error
	// UpdateExt 仅更新 ext 字段。
	UpdateExt(ctx context.Context, id uint, ext string) error
	// UpdateDraftURL 回写剪映草稿 URL（草稿生成/一键成片成功后调用）。
	UpdateDraftURL(ctx context.Context, id uint, draftURL string) error
	// UpdateURLs 按需更新 draft_url / video_url；指针为 nil 表示不更新该字段。
	UpdateURLs(ctx context.Context, id uint, draftURL, videoURL *string) error
	// UpdatePrompts 回写本次任务实际使用的系统/用户提示词。
	UpdatePrompts(ctx context.Context, id uint, sysPrompt, usrPrompt string) error
	// UpdateVideoProjectID 更新关联的剪辑项目 ID。
	UpdateVideoProjectID(ctx context.Context, id uint, videoProjectID uint) error
	// CountProcessingByTypes 统计指定类型中处于 processing 的任务数（供 draft 槽位限流复用）。
	CountProcessingByTypes(ctx context.Context, types []string) (int64, error)
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
	query = applyTaskListFilter(query, filter)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("id DESC").Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

// applyTaskListFilter 将列表筛选条件应用到 GORM 查询。
func applyTaskListFilter(query *gorm.DB, filter TaskListFilter) *gorm.DB {
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.StartAt != nil {
		query = query.Where("created_at >= ?", *filter.StartAt)
	}
	if filter.EndAt != nil {
		query = query.Where("created_at < ?", *filter.EndAt)
	}
	for _, kw := range filter.Keywords {
		pattern := "%" + kw + "%"
		query = query.Where("LOWER(video_project_name) LIKE ?", pattern)
	}
	return query
}

// ClaimPendingByType 原子抢占：选最早一条 pending 任务并改为 processing。
// 多实例下依赖 SKIP LOCKED 避免重复领取；SQLite 单测无 SKIP LOCKED 时退化为行锁。
func (r *taskRepository) ClaimPendingByType(ctx context.Context, taskType string) (*model.Task, error) {
	var claimed *model.Task
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.Task
		q := tx.Where("status = ? AND type = ?", model.TaskStatusPending, taskType).
			Order("id ASC").
			Limit(1)
		// Postgres 使用 SKIP LOCKED，保证多 Worker 互不阻塞、不重复抢占。
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := q.First(&task).Error; err != nil {
			return err
		}

		now := time.Now()
		result := tx.Model(&model.Task{}).
			Where("id = ? AND status = ?", task.ID, model.TaskStatusPending).
			Updates(map[string]interface{}{
				"status":     model.TaskStatusProcessing,
				"progress":   int16(0),
				"started_at": now,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		// 并发下若已被其它实例抢走则视为本次未抢到。
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		task.Status = model.TaskStatusProcessing
		task.Progress = 0
		task.StartedAt = &now
		task.UpdatedAt = now
		claimed = &task
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *taskRepository) UpdateProgress(ctx context.Context, id uint, progress int16) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ? AND status = ?", id, model.TaskStatusProcessing).
		Updates(map[string]interface{}{
			"progress":   progress,
			"updated_at": time.Now(),
		}).Error
}

func (r *taskRepository) MarkCompleted(ctx context.Context, id uint, progress int16, ext string) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status":       model.TaskStatusCompleted,
		"progress":     progress,
		"completed_at": now,
		"updated_at":   now,
		"error_message": "",
	}
	if ext != "" {
		updates["ext"] = ext
	}
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *taskRepository) MarkFailed(ctx context.Context, id uint, progress int16, errorMsg string) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        model.TaskStatusFailed,
			"progress":      progress,
			"error_message": errorMsg,
			"completed_at":  now,
			"updated_at":    now,
		}).Error
}

func (r *taskRepository) UpdateExt(ctx context.Context, id uint, ext string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"ext":        ext,
			"updated_at": time.Now(),
		}).Error
}

// UpdateDraftURL 将草稿生成结果写入 task.draft_url。
func (r *taskRepository) UpdateDraftURL(ctx context.Context, id uint, draftURL string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"draft_url":  draftURL,
			"updated_at": time.Now(),
		}).Error
}

// UpdateURLs 仅更新请求中显式传入的 URL 字段。
func (r *taskRepository) UpdateURLs(ctx context.Context, id uint, draftURL, videoURL *string) error {
	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}
	if draftURL != nil {
		updates["draft_url"] = *draftURL
	}
	if videoURL != nil {
		updates["video_url"] = *videoURL
	}
	// 未传任何 URL 时无需写库。
	if len(updates) == 1 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// UpdatePrompts 将本次任务使用的系统提示词与用户提示词写入 task 表。
func (r *taskRepository) UpdatePrompts(ctx context.Context, id uint, sysPrompt, usrPrompt string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"sys_prompt": sysPrompt,
			"usr_prompt": usrPrompt,
			"updated_at": time.Now(),
		}).Error
}

func (r *taskRepository) UpdateVideoProjectID(ctx context.Context, id uint, videoProjectID uint) error {
	if videoProjectID == 0 {
		return r.db.WithContext(ctx).
			Model(&model.Task{}).
			Where("id = ?", id).
			Updates(map[string]interface{}{
				"video_project_id": nil,
				"updated_at":       time.Now(),
			}).Error
	}
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"video_project_id": videoProjectID,
			"updated_at":       time.Now(),
		}).Error
}

func (r *taskRepository) CountProcessingByTypes(ctx context.Context, types []string) (int64, error) {
	if len(types) == 0 {
		return 0, nil
	}
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("status = ? AND type IN ?", model.TaskStatusProcessing, types).
		Count(&count).Error
	return count, err
}
