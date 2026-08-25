// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"
	"errors"
	"time"

	"live-mixer/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// claimOptimisticMaxAttempts 乐观锁抢占最大重试次数。
// 多 Worker 同时选中同一 pending 行时，仅一个 CAS 成功，其余需改抢下一条；
// 上限用于避免极端竞争下空转，正常场景远低于该值。
const claimOptimisticMaxAttempts = 64

// TaskListFilter 任务列表查询筛选条件。
type TaskListFilter struct {
	Type           string     // 任务类型，空表示不限
	Status         string     // 任务状态，空表示不限；与 Statuses 互斥，Statuses 优先
	Statuses       []string   // 多状态 IN 查询；非空时忽略 Status
	VideoProjectID *uint      // 关联剪辑项目 ID，nil 表示不限
	StartAt        *time.Time // 开始日期（含），按 created_at 筛选
	EndAt          *time.Time // 结束日期次日零点（不含），按 created_at 筛选
	Keywords       KeywordGroups // 关键词表达式（已规范化小写）：组内 AND、组间 OR；模糊匹配 video_project_name
}

// TaskRepository 异步任务数据访问接口。
type TaskRepository interface {
	// Create 插入一条任务记录；若 ID 为空则自动生成 UUID。
	Create(ctx context.Context, task *model.Task) error
	// GetByID 根据主键查询任务。
	GetByID(ctx context.Context, id string) (*model.Task, error)
	// List 分页查询任务列表，按创建时间倒序；width/height/live_url/live_name 直接读 task 冗余字段。
	List(ctx context.Context, filter TaskListFilter, offset, limit int) ([]model.TaskListItem, int64, error)
	// ClaimPendingByType 多实例安全地抢占一条指定类型的 pending 任务。
	// 使用乐观锁（version CAS）：将状态改为 processing 并返回；无待处理任务时返回 nil。
	ClaimPendingByType(ctx context.Context, taskType string) (*model.Task, error)
	// UpdateProgress 更新任务进度（0-100），仅 processing 状态时生效。
	UpdateProgress(ctx context.Context, id string, progress int16) error
	// MarkCompleted 标记任务成功完成，并写入最终进度与扩展字段。
	MarkCompleted(ctx context.Context, id string, progress int16, ext string) error
	// MarkFailed 标记任务失败，写入错误信息与当前进度。
	MarkFailed(ctx context.Context, id string, progress int16, errorMsg string) error
	// UpdateExt 仅更新 ext 字段。
	UpdateExt(ctx context.Context, id string, ext string) error
	// UpdateDraftURL 回写剪映草稿 URL（草稿生成/一键成片成功后调用）。
	UpdateDraftURL(ctx context.Context, id string, draftURL string) error
	// UpdateVideoURL 回写成片视频 URL（草稿成功后 gen_video 完成时调用）。
	UpdateVideoURL(ctx context.Context, id string, videoURL string) error
	// UpdateClipsTarURL 回写切片 tar 包下载地址（草稿类任务打包上传后调用）。
	UpdateClipsTarURL(ctx context.Context, id string, clipsTarURL string) error
	// UpdateErrorMessage 写入错误/警告信息（例如草稿成功但视频生成失败的部分成功场景）。
	UpdateErrorMessage(ctx context.Context, id string, errorMsg string) error
	// UpdatePrompts 回写本次任务实际使用的系统/用户提示词。
	UpdatePrompts(ctx context.Context, id string, sysPrompt, usrPrompt string) error
	// UpdateVideoProjectID 更新关联的剪辑项目 ID。
	UpdateVideoProjectID(ctx context.Context, id string, videoProjectID uint) error
	// CountProcessingByTypes 统计指定类型中处于 processing 的任务数（供 draft 槽位限流复用）。
	CountProcessingByTypes(ctx context.Context, types []string) (int64, error)
	// RequeueStaleProcessingByType 将指定类型下超时未更新的 processing 任务重置为 pending，供崩溃/重启恢复。
	// 以 updated_at 为心跳：正常执行会通过 UpdateProgress 等刷新；超时未刷新则视为孤儿任务。
	RequeueStaleProcessingByType(ctx context.Context, taskType string, olderThan time.Duration) (int64, error)
}

type taskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 创建任务仓储实例。
func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(ctx context.Context, task *model.Task) error {
	// 全新部署使用 UUID 主键；调用方未填时由仓储统一生成，避免业务层遗漏。
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *taskRepository) GetByID(ctx context.Context, id string) (*model.Task, error) {
	var task model.Task
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *taskRepository) List(ctx context.Context, filter TaskListFilter, offset, limit int) ([]model.TaskListItem, int64, error) {
	var items []model.TaskListItem
	var total int64

	countQuery := r.db.WithContext(ctx).Model(&model.Task{})
	countQuery = applyTaskListFilter(countQuery, filter)
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 直接查 task 表：创建时已快照 width/height/live_url/live_name，无需再 JOIN 关联表。
	query := r.db.WithContext(ctx).Model(&model.Task{})
	query = applyTaskListFilter(query, filter)
	// UUID 主键无序，列表按创建时间倒序保证「新任务在前」。
	if err := query.Offset(offset).Limit(limit).Order("created_at DESC, id DESC").Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// applyTaskListFilter 将列表筛选条件应用到 GORM 查询。
func applyTaskListFilter(query *gorm.DB, filter TaskListFilter) *gorm.DB {
	if filter.Type != "" {
		query = query.Where("type = ?", filter.Type)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	} else if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.VideoProjectID != nil {
		query = query.Where("video_project_id = ?", *filter.VideoProjectID)
	}
	if filter.StartAt != nil {
		query = query.Where("created_at >= ?", *filter.StartAt)
	}
	if filter.EndAt != nil {
		query = query.Where("created_at < ?", *filter.EndAt)
	}
	query = applyKeywordGroups(query, filter.Keywords, "LOWER(video_project_name) LIKE ?", 1)
	return query
}

// ClaimPendingByType 乐观锁抢占：选最早一条 pending 任务，用 version CAS 改为 processing。
//
// 关键流程：
//  1. 按 created_at ASC 取出一条 pending（不持有行锁，多实例可并发读到同一行）；
//  2. UPDATE ... WHERE id=? AND status=pending AND version=?，成功则 version+1；
//  3. RowsAffected=0 表示被其它 Worker 抢先，继续尝试下一条，避免误判队列为空。
func (r *taskRepository) ClaimPendingByType(ctx context.Context, taskType string) (*model.Task, error) {
	for attempt := 0; attempt < claimOptimisticMaxAttempts; attempt++ {
		var task model.Task
		err := r.db.WithContext(ctx).
			Where("status = ? AND type = ?", model.TaskStatusPending, taskType).
			Order("created_at ASC, id ASC").
			First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		now := time.Now()
		newVersion := task.Version + 1
		// CAS：仅当 status 仍为 pending 且 version 未被他人改写时抢占成功。
		result := r.db.WithContext(ctx).Model(&model.Task{}).
			Where("id = ? AND status = ? AND version = ?", task.ID, model.TaskStatusPending, task.Version).
			Updates(map[string]interface{}{
				"status":     model.TaskStatusProcessing,
				"progress":   int16(0),
				"started_at": now,
				"updated_at": now,
				"version":    newVersion,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			// 乐观锁冲突：该任务已被其它实例抢走，继续抢下一条。
			continue
		}

		task.Status = model.TaskStatusProcessing
		task.Progress = 0
		task.StartedAt = &now
		task.UpdatedAt = now
		task.Version = newVersion
		return &task, nil
	}
	// 短时间内大量冲突仍未抢到：返回空，由 Worker 下一轮 poll/唤醒再试。
	return nil, nil
}

func (r *taskRepository) UpdateProgress(ctx context.Context, id string, progress int16) error {
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

func (r *taskRepository) MarkCompleted(ctx context.Context, id string, progress int16, ext string) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status":        model.TaskStatusCompleted,
		"progress":      progress,
		"completed_at":  now,
		"updated_at":    now,
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

func (r *taskRepository) MarkFailed(ctx context.Context, id string, progress int16, errorMsg string) error {
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

func (r *taskRepository) UpdateExt(ctx context.Context, id string, ext string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"ext":        ext,
			"updated_at": time.Now(),
		}).Error
}

// UpdateDraftURL 将草稿生成结果写入 task.draft_url。
func (r *taskRepository) UpdateDraftURL(ctx context.Context, id string, draftURL string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"draft_url":  draftURL,
			"updated_at": time.Now(),
		}).Error
}

// UpdateVideoURL 将成片视频地址写入 task.video_url。
func (r *taskRepository) UpdateVideoURL(ctx context.Context, id string, videoURL string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"video_url":  videoURL,
			"updated_at": time.Now(),
		}).Error
}

// UpdateClipsTarURL 将切片 tar 包下载地址写入 task.clips_tar_url。
func (r *taskRepository) UpdateClipsTarURL(ctx context.Context, id string, clipsTarURL string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"clips_tar_url": clipsTarURL,
			"updated_at":    time.Now(),
		}).Error
}

// UpdateErrorMessage 写入错误/警告信息，不改变任务状态。
func (r *taskRepository) UpdateErrorMessage(ctx context.Context, id string, errorMsg string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"error_message": errorMsg,
			"updated_at":    time.Now(),
		}).Error
}

// UpdatePrompts 将本次任务使用的系统提示词与用户提示词写入 task 表。
func (r *taskRepository) UpdatePrompts(ctx context.Context, id string, sysPrompt, usrPrompt string) error {
	return r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"sys_prompt": sysPrompt,
			"usr_prompt": usrPrompt,
			"updated_at": time.Now(),
		}).Error
}

func (r *taskRepository) UpdateVideoProjectID(ctx context.Context, id string, videoProjectID uint) error {
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

// RequeueStaleProcessingByType 将 type 匹配且 updated_at 早于阈值的 processing 任务改回 pending。
// 同时递增 version，避免与仍在执行的旧 Worker 进度回写产生语义混淆。
// olderThan <= 0 或 taskType 为空时不执行任何更新，直接返回 0。
func (r *taskRepository) RequeueStaleProcessingByType(ctx context.Context, taskType string, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 || taskType == "" {
		return 0, nil
	}
	cutoff := time.Now().Add(-olderThan)
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("status = ? AND type = ? AND updated_at < ?", model.TaskStatusProcessing, taskType, cutoff).
		Updates(map[string]interface{}{
			"status":        model.TaskStatusPending,
			"progress":      int16(0),
			"error_message": "任务处理超时，已自动重新排队",
			"started_at":    nil,
			"updated_at":    now,
			"version":       gorm.Expr("version + 1"),
		})
	return result.RowsAffected, result.Error
}
