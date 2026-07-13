// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"
	"time"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

// LiveMaterialRepository 直播素材数据访问接口。
type LiveMaterialRepository interface {
	// Create 插入一条直播素材记录。
	Create(ctx context.Context, material *model.LiveMaterial) error
	// GetByID 根据主键查询直播素材。
	GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error)
	// UpdateNameRemark 仅更新素材名称与备注，防止误改其它字段。
	UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error
	// UpdateASRProcessing 标记 ASR 开始识别。
	UpdateASRProcessing(ctx context.Context, id uint) error
	// UpdateASRProgress 更新 ASR 识别进度。
	UpdateASRProgress(ctx context.Context, id uint, progress int16) error
	// UpdateASRCompleted 写入 ASR 成功结果。
	UpdateASRCompleted(ctx context.Context, id uint, liveASR string, duration int64) error
	// UpdateASRFailed 标记 ASR 识别失败。
	UpdateASRFailed(ctx context.Context, id uint, progress int16, errorMsg string) error
	// List 分页查询直播素材列表，支持日期与关键词筛选，按 id 倒序，不含 live_asr 字段。
	List(ctx context.Context, filter LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error)
	// Delete 物理删除直播素材，并级联删除关联的 video_project 记录。
	Delete(ctx context.Context, id uint) error
}

// LiveMaterialListFilter 直播素材列表查询筛选条件。
type LiveMaterialListFilter struct {
	StartAt        *time.Time // 开始日期（含），按 created_at 筛选
	EndAt          *time.Time // 结束日期次日零点（不含），按 created_at 筛选
	TitleKeywords  []string   // 标题关键词，每个词匹配 name 或 remark，词之间为「与」
	GlobalKeywords []string   // 全局关键词，每个词匹配 live_url/asr_error_msg/name/remark，词之间为「与」
}

type liveMaterialRepository struct {
	db *gorm.DB
}

// NewLiveMaterialRepository 创建直播素材仓储实例。
func NewLiveMaterialRepository(db *gorm.DB) LiveMaterialRepository {
	return &liveMaterialRepository{db: db}
}

func (r *liveMaterialRepository) Create(ctx context.Context, material *model.LiveMaterial) error {
	return r.db.WithContext(ctx).Create(material).Error
}

func (r *liveMaterialRepository) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	var material model.LiveMaterial
	err := r.db.WithContext(ctx).First(&material, id).Error
	if err != nil {
		return nil, err
	}
	return &material, nil
}

func (r *liveMaterialRepository) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	// Select 限定更新列，确保 live_url、ASR 等字段不会被意外覆盖。
	return r.db.WithContext(ctx).
		Model(material).
		Select("name", "remark").
		Updates(material).Error
}

func (r *liveMaterialRepository) UpdateASRProcessing(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"asr_status":     model.ASRStatusProcessing,
			"asr_progress":   int16(5),
			"asr_error_msg":  "",
			"asr_started_at": now,
			"asr_updated_at": now,
		}).Error
}

func (r *liveMaterialRepository) UpdateASRProgress(ctx context.Context, id uint, progress int16) error {
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"asr_progress":   progress,
			"asr_updated_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) UpdateASRCompleted(ctx context.Context, id uint, liveASR string, duration int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"asr_status":     model.ASRStatusCompleted,
			"asr_progress":   int16(100),
			"live_asr":       liveASR,
			"duration":       duration,
			"asr_error_msg":  "",
			"asr_updated_at": now,
		}).Error
}

func (r *liveMaterialRepository) UpdateASRFailed(ctx context.Context, id uint, progress int16, errorMsg string) error {
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"asr_status":     model.ASRStatusFailed,
			"asr_progress":   progress,
			"asr_error_msg":  errorMsg,
			"asr_updated_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) List(ctx context.Context, filter LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	var materials []model.LiveMaterialListItem
	var total int64

	// 使用列表专用结构体，GORM 不会查询 live_asr 列，减少 IO 与响应体积。
	query := r.db.WithContext(ctx).Model(&model.LiveMaterialListItem{})
	query = applyLiveMaterialListFilter(query, filter)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("id DESC").Find(&materials).Error; err != nil {
		return nil, 0, err
	}
	return materials, total, nil
}

// applyLiveMaterialListFilter 将列表筛选条件应用到 GORM 查询。
func applyLiveMaterialListFilter(query *gorm.DB, filter LiveMaterialListFilter) *gorm.DB {
	if filter.StartAt != nil {
		query = query.Where("created_at >= ?", *filter.StartAt)
	}
	if filter.EndAt != nil {
		query = query.Where("created_at < ?", *filter.EndAt)
	}
	for _, kw := range filter.TitleKeywords {
		pattern := "%" + kw + "%"
		// 单个关键词命中 name 或 remark 即可，多个关键词之间为「与」。
		query = query.Where("LOWER(name) LIKE ? OR LOWER(remark) LIKE ?", pattern, pattern)
	}
	for _, kw := range filter.GlobalKeywords {
		pattern := "%" + kw + "%"
		// 全局关键词覆盖链接、错误信息，并包含标题字段的匹配能力。
		query = query.Where(
			"LOWER(live_url) LIKE ? OR LOWER(asr_error_msg) LIKE ? OR LOWER(name) LIKE ? OR LOWER(remark) LIKE ?",
			pattern, pattern, pattern, pattern,
		)
	}
	return query
}

// Delete 物理删除直播素材，并在同一事务内级联删除 live_id 关联的剪辑项目。
func (r *liveMaterialRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("live_id = ?", id).Delete(&model.VideoProject{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.LiveMaterial{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
