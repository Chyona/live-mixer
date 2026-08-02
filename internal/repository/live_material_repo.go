// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	// GetByName 根据素材名称精确查询。
	GetByName(ctx context.Context, name string) (*model.LiveMaterial, error)
	// GetByLiveURL 根据直播链接精确查询。
	GetByLiveURL(ctx context.Context, liveURL string) (*model.LiveMaterial, error)
	// UpdateNameRemark 仅更新素材名称与备注，防止误改其它字段。
	UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error
	// ClaimPendingASR 多实例安全地抢占一条 pending ASR 任务。
	// 使用乐观锁（asr_version CAS）：将状态改为 processing 并返回；无待处理时返回 nil。
	ClaimPendingASR(ctx context.Context) (*model.LiveMaterial, error)
	// RequeueStaleProcessingASR 将超时未更新的 processing 任务重置为 pending，供崩溃恢复。
	RequeueStaleProcessingASR(ctx context.Context, olderThan time.Duration) (int64, error)
	// UpdateASRProcessing 标记 ASR 开始识别。
	UpdateASRProcessing(ctx context.Context, id uint) error
	// UpdateASRProgress 更新 ASR 识别进度。
	// 仅当 asr_status=processing 且 asr_version 匹配时生效，避免超时回收后旧 Worker 继续写回。
	UpdateASRProgress(ctx context.Context, id uint, asrVersion int64, progress int16) error
	// UpdateASRCompleted 写入 ASR 成功结果（含 summaries/paragraphs）并回写分辨率。
	// 仅当 asr_status=processing 且 asr_version 匹配时生效。
	UpdateASRCompleted(ctx context.Context, id uint, asrVersion int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error
	// UpdateASRFailed 标记 ASR 识别失败。
	// 仅当 asr_status=processing 且 asr_version 匹配时生效。
	UpdateASRFailed(ctx context.Context, id uint, asrVersion int64, progress int16, errorMsg string) error
	// ResetASRToPending 将失败的 ASR 重置为待处理（仅 failed 生效）。
	ResetASRToPending(ctx context.Context, id uint) error
	// List 分页查询直播素材列表，支持日期与关键词筛选，按 id 倒序，不含 live_asr 字段。
	List(ctx context.Context, filter LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error)
	// Delete 物理删除直播素材，并级联删除关联的 video_project 记录。
	Delete(ctx context.Context, id uint) error
}

// LiveMaterialListFilter 直播素材列表查询筛选条件。
type LiveMaterialListFilter struct {
	StartAt        *time.Time // 开始日期（含），按 created_at 筛选
	EndAt          *time.Time // 结束日期次日零点（不含），按 created_at 筛选
	Keywords    KeywordGroups // 标题/备注关键词：组内 AND、组间 OR；单组内词匹配 name 或 remark
	ASRKeywords KeywordGroups // ASR 段落关键词：组内 AND、组间 OR；匹配 asr_paragraphs 文本
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

func (r *liveMaterialRepository) GetByName(ctx context.Context, name string) (*model.LiveMaterial, error) {
	var material model.LiveMaterial
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&material).Error
	if err != nil {
		return nil, err
	}
	return &material, nil
}

func (r *liveMaterialRepository) GetByLiveURL(ctx context.Context, liveURL string) (*model.LiveMaterial, error) {
	var material model.LiveMaterial
	err := r.db.WithContext(ctx).Where("live_url = ?", liveURL).First(&material).Error
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

// ClaimPendingASR 乐观锁抢占：选最早一条 pending 素材，用 asr_version CAS 改为 processing。
//
// 关键流程与任务表 ClaimPendingByType 一致：
//  1. 按 created_at ASC 取出一条 asr_status=pending（不持行锁）；
//  2. UPDATE ... WHERE id=? AND asr_status=pending AND asr_version=?；
//  3. 冲突则继续尝试下一条，避免误判队列为空。
func (r *liveMaterialRepository) ClaimPendingASR(ctx context.Context) (*model.LiveMaterial, error) {
	for attempt := 0; attempt < claimOptimisticMaxAttempts; attempt++ {
		var material model.LiveMaterial
		err := r.db.WithContext(ctx).
			Where("asr_status = ?", model.ASRStatusPending).
			Order("created_at ASC, id ASC").
			First(&material).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		now := time.Now()
		newVersion := material.ASRVersion + 1
		// CAS：仅当仍为 pending 且 asr_version 未被改写时抢占成功。
		result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
			Where("id = ? AND asr_status = ? AND asr_version = ?", material.ID, model.ASRStatusPending, material.ASRVersion).
			Updates(map[string]interface{}{
				"asr_status":       model.ASRStatusProcessing,
				"asr_progress":     int16(5),
				"asr_error_msg":    "",
				"asr_started_at":   now,
				"asr_updated_at":   now,
				"asr_completed_at": nil,
				"asr_version":      newVersion,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			// 乐观锁冲突：已被其它实例抢走，继续抢下一条。
			continue
		}

		material.ASRStatus = model.ASRStatusProcessing
		material.ASRProgress = 5
		material.ASRErrorMsg = ""
		material.ASRStartedAt = &now
		material.ASRUpdatedAt = &now
		material.ASRCompletedAt = nil
		material.ASRVersion = newVersion
		return &material, nil
	}
	return nil, nil
}

// RequeueStaleProcessingASR 将 asr_updated_at 早于阈值的 processing 任务改回 pending。
// 同时递增 asr_version，便于新一轮抢占与旧 Worker 写回区分。
func (r *liveMaterialRepository) RequeueStaleProcessingASR(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-olderThan)
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("asr_status = ? AND asr_updated_at IS NOT NULL AND asr_updated_at < ?", model.ASRStatusProcessing, cutoff).
		Updates(map[string]interface{}{
			"asr_status":       model.ASRStatusPending,
			"asr_progress":     int16(0),
			"asr_error_msg":    "ASR 处理超时，已自动重新排队",
			// 保留 asr_started_at：pending 窗口可继续展示上次开始时间；新一轮由 ClaimPendingASR 覆盖。
			"asr_updated_at":   now,
			"asr_completed_at": nil,
			"asr_version":      gorm.Expr("asr_version + 1"),
		})
	return result.RowsAffected, result.Error
}

func (r *liveMaterialRepository) UpdateASRProcessing(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"asr_status":       model.ASRStatusProcessing,
			"asr_progress":     int16(5),
			"asr_error_msg":    "",
			"asr_started_at":   now,
			"asr_updated_at":   now,
			"asr_completed_at": nil,
		}).Error
}

func (r *liveMaterialRepository) UpdateASRProgress(ctx context.Context, id uint, asrVersion int64, progress int16) error {
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_status = ? AND asr_version = ?", id, model.ASRStatusProcessing, asrVersion).
		Updates(map[string]interface{}{
			"asr_progress":   progress,
			"asr_updated_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) UpdateASRCompleted(ctx context.Context, id uint, asrVersion int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	summariesJSON, err := marshalASRJSONArray(summaries)
	if err != nil {
		return err
	}
	paragraphsJSON, err := marshalASRJSONArray(paragraphs)
	if err != nil {
		return err
	}
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_status = ? AND asr_version = ?", id, model.ASRStatusProcessing, asrVersion).
		Updates(map[string]interface{}{
			"asr_status":       model.ASRStatusCompleted,
			"asr_progress":     int16(100),
			"live_asr":         liveASR,
			"duration":         duration,
			"width":            width,
			"height":           height,
			"asr_summaries":    summariesJSON,
			"asr_paragraphs":   paragraphsJSON,
			"asr_error_msg":    "",
			"asr_updated_at":   now,
			"asr_completed_at": now,
		}).Error
}

func (r *liveMaterialRepository) UpdateASRFailed(ctx context.Context, id uint, asrVersion int64, progress int16, errorMsg string) error {
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_status = ? AND asr_version = ?", id, model.ASRStatusProcessing, asrVersion).
		Updates(map[string]interface{}{
			"asr_status":     model.ASRStatusFailed,
			"asr_progress":   progress,
			"asr_error_msg":  errorMsg,
			"asr_updated_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) ResetASRToPending(ctx context.Context, id uint) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_status = ?", id, model.ASRStatusFailed).
		Updates(map[string]interface{}{
			"asr_status":       model.ASRStatusPending,
			"asr_progress":     int16(0),
			"live_asr":         "{}",
			"asr_summaries":    "[]",
			"asr_paragraphs":   "[]",
			"asr_error_msg":    "",
			"asr_started_at":   nil,
			"asr_updated_at":   now,
			"asr_completed_at": nil,
			// 重试入队同样递增版本，保证后续抢占 CAS 基于最新版本。
			"asr_version": gorm.Expr("asr_version + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *liveMaterialRepository) List(ctx context.Context, filter LiveMaterialListFilter, offset, limit int) ([]model.LiveMaterialListItem, int64, error) {
	var total int64

	// 使用列表专用结构体，GORM 不会查询 live_asr 列，减少 IO 与响应体积。
	query := r.db.WithContext(ctx).Model(&model.LiveMaterialListItem{})
	query = applyLiveMaterialListFilter(query, filter)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	needParagraphs := !KeywordGroupsEmpty(filter.ASRKeywords)
	const projectCountExpr = "(SELECT COUNT(*) FROM video_project WHERE video_project.live_id = live_material.id) AS project_count"
	if needParagraphs {
		// 带关键词时额外取出 asr_paragraphs，供内存侧筛出命中段落。
		type listRow struct {
			model.LiveMaterialListItem
			ASRParagraphs []model.ASRParagraph `gorm:"column:asr_paragraphs;serializer:json"`
		}
		var rows []listRow
		listSelect := "live_material.*, " + projectCountExpr
		if err := query.Select(listSelect).Offset(offset).Limit(limit).Order("live_material.id DESC").Find(&rows).Error; err != nil {
			return nil, 0, err
		}
		materials := make([]model.LiveMaterialListItem, 0, len(rows))
		for i := range rows {
			item := rows[i].LiveMaterialListItem
			item.MatchedParagraphs = filterMatchedASRParagraphs(rows[i].ASRParagraphs, filter.ASRKeywords)
			materials = append(materials, item)
		}
		return materials, total, nil
	}

	var materials []model.LiveMaterialListItem
	listSelect := "live_material.*, " + projectCountExpr
	if err := query.Select(listSelect).Offset(offset).Limit(limit).Order("live_material.id DESC").Find(&materials).Error; err != nil {
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
	// 单组内：词匹配 name 或 remark；组内 AND、组间 OR。
	query = applyKeywordGroups(query, filter.Keywords, "LOWER(name) LIKE ? OR LOWER(remark) LIKE ?", 2)
	// ASR：仅在 asr_paragraphs JSON 文本中匹配；组内 AND、组间 OR。
	query = applyKeywordGroups(query, filter.ASRKeywords, "LOWER(CAST(asr_paragraphs AS TEXT)) LIKE ?", 1)
	return query
}

// filterMatchedASRParagraphs 返回满足关键词表达式的段落（不含 words，减小列表体积）。
// 段落命中规则与列表筛选一致：任一组（OR）内全部词（AND）均出现在正文中。
func filterMatchedASRParagraphs(paragraphs []model.ASRParagraph, groups KeywordGroups) []model.ASRParagraph {
	if len(paragraphs) == 0 || KeywordGroupsEmpty(groups) {
		return nil
	}
	out := make([]model.ASRParagraph, 0)
	for _, p := range paragraphs {
		text := strings.ToLower(p.Text)
		if !keywordGroupsMatchText(groups, text) {
			continue
		}
		out = append(out, model.ASRParagraph{
			Speaker:   p.Speaker,
			Text:      p.Text,
			StartTime: p.StartTime,
			EndTime:   p.EndTime,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// keywordGroupsMatchText 判断文本是否满足关键词表达式（组间 OR、组内 AND）。
func keywordGroupsMatchText(groups KeywordGroups, text string) bool {
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		all := true
		for _, kw := range group {
			if kw == "" || !strings.Contains(text, kw) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
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

// marshalASRJSONArray 将 summaries/paragraphs 序列化为 JSON 数组字符串，供 JSONB 写入。
func marshalASRJSONArray(v any) (string, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(b) == 0 || string(b) == "null" {
		return "[]", nil
	}
	return string(b), nil
}
