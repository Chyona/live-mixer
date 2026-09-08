package repository

import (
	"context"
	"errors"
	"time"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

const ingestHeartbeatStale = 2 * time.Minute

// LiveIngestRepository 跟播长任务仓储；与一次性 ASR 抢占隔离。
type LiveIngestRepository interface {
	GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error)
	ClaimIngestWork(ctx context.Context) (*model.LiveMaterial, error)
	HeartbeatIngest(ctx context.Context, id uint, epoch int64) error
	MarkConnecting(ctx context.Context, id uint, epoch int64) error
	MarkLiveStarted(ctx context.Context, id uint, epoch int64, width, height int) error
	UpdateRecordingProgress(ctx context.Context, id uint, epoch int64, nextSeg int64, durationMS int64, playlistURL string) error
	AppendWindowASR(ctx context.Context, id uint, epoch int64, liveASR string, asrCursorMS, durationMS int64, progress int16) error
	MarkEnding(ctx context.Context, id uint, epoch int64) error
	MarkEnded(ctx context.Context, id uint, epoch int64, durationMS int64) error
	MarkIngestFailed(ctx context.Context, id uint, epoch int64, msg string) error
	ResetFailedIngest(ctx context.Context, id uint, m3u8URL string) error
	UpdateM3U8URL(ctx context.Context, id uint, m3u8URL string) error
	GetByM3U8URL(ctx context.Context, m3u8URL string) (*model.LiveMaterial, error)
	FinalizeASR(ctx context.Context, id uint, epoch int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error
}

// NewLiveIngestRepository 与 LiveMaterialRepository 共用同一实现，供跟播 Worker 注入。
func NewLiveIngestRepository(db *gorm.DB) LiveIngestRepository {
	return &liveMaterialRepository{db: db}
}

func (r *liveMaterialRepository) GetByM3U8URL(ctx context.Context, m3u8URL string) (*model.LiveMaterial, error) {
	var material model.LiveMaterial
	err := r.db.WithContext(ctx).Where("m3u8_url = ? AND m3u8_url <> ''", m3u8URL).First(&material).Error
	if err != nil {
		return nil, err
	}
	return &material, nil
}

func (r *liveMaterialRepository) UpdateM3U8URL(ctx context.Context, id uint, m3u8URL string) error {
	return r.db.WithContext(ctx).
		Model(&model.LiveMaterial{}).
		Where("id = ? AND live_status IN ?", id, []string{model.LiveStatusWaiting, model.LiveStatusConnecting, model.LiveStatusFailed}).
		Updates(map[string]interface{}{
			"m3u8_url":   m3u8URL,
			"updated_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) ClaimIngestWork(ctx context.Context) (*model.LiveMaterial, error) {
	tried := make(map[uint]struct{})
	now := time.Now()
	staleBefore := now.Add(-ingestHeartbeatStale)
	for attempt := 0; attempt < claimOptimisticMaxAttempts; attempt++ {
		var material model.LiveMaterial
		q := r.db.WithContext(ctx).Where(
			`(live_status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?)
			 OR live_status = ?
			 OR (live_status = ? AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?))
			 OR live_status = ?`,
			model.LiveStatusWaiting, now.Add(model.LiveEarlyProbe),
			model.LiveStatusConnecting,
			model.LiveStatusLive, staleBefore,
			model.LiveStatusEnding,
		)
		if len(tried) > 0 {
			q = q.Where("id NOT IN ?", uintSetKeys(tried))
		}
		err := q.Order("id ASC").First(&material).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		newEpoch := material.IngestEpoch + 1
		result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
			Where("id = ? AND ingest_epoch = ? AND live_status = ?", material.ID, material.IngestEpoch, material.LiveStatus).
			Updates(map[string]interface{}{
				"ingest_epoch":      newEpoch,
				"last_heartbeat_at": now,
				"updated_at":        now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			tried[material.ID] = struct{}{}
			continue
		}
		material.IngestEpoch = newEpoch
		material.LastHeartbeatAt = &now
		return &material, nil
	}
	return nil, nil
}

func (r *liveMaterialRepository) HeartbeatIngest(ctx context.Context, id uint, epoch int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"last_heartbeat_at": now,
			"updated_at":        now,
		}).Error
}

func (r *liveMaterialRepository) MarkConnecting(ctx context.Context, id uint, epoch int64) error {
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ? AND live_status IN ?", id, epoch, []string{model.LiveStatusWaiting, model.LiveStatusConnecting}).
		Updates(map[string]interface{}{
			"live_status":       model.LiveStatusConnecting,
			"last_heartbeat_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) MarkLiveStarted(ctx context.Context, id uint, epoch int64, width, height int) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_status":       model.LiveStatusLive,
			"stream_started_at": now,
			"asr_status":        model.ASRStatusProcessing,
			"asr_started_at":    now,
			"asr_updated_at":    now,
			"width":             width,
			"height":            height,
			"last_heartbeat_at": now,
			"ingest_error_msg":  "",
		}).Error
}

func (r *liveMaterialRepository) UpdateRecordingProgress(ctx context.Context, id uint, epoch int64, nextSeg int64, durationMS int64, playlistURL string) error {
	now := time.Now()
	fields := map[string]interface{}{
		"next_seg":          nextSeg,
		"duration":          durationMS,
		"last_heartbeat_at": now,
		"asr_updated_at":    now,
		"updated_at":        now,
	}
	if playlistURL != "" {
		fields["record_playlist_url"] = playlistURL
	}
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(fields).Error
}

func (r *liveMaterialRepository) AppendWindowASR(ctx context.Context, id uint, epoch int64, liveASR string, asrCursorMS, durationMS int64, progress int16) error {
	now := time.Now()
	if progress < 0 {
		progress = 0
	}
	if progress > 99 {
		progress = 99
	}
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_asr":          liveASR,
			"asr_cursor_ms":     asrCursorMS,
			"duration":          durationMS,
			"asr_progress":      progress,
			"asr_status":        model.ASRStatusProcessing,
			"asr_updated_at":    now,
			"last_heartbeat_at": now,
		}).Error
}

func (r *liveMaterialRepository) MarkEnding(ctx context.Context, id uint, epoch int64) error {
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_status":       model.LiveStatusEnding,
			"last_heartbeat_at": time.Now(),
		}).Error
}

func (r *liveMaterialRepository) MarkEnded(ctx context.Context, id uint, epoch int64, durationMS int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_status":       model.LiveStatusEnded,
			"url_type":          model.URLTypeFile,
			"duration":          durationMS,
			"last_heartbeat_at": now,
			"updated_at":        now,
		}).Error
}

func (r *liveMaterialRepository) MarkIngestFailed(ctx context.Context, id uint, epoch int64, msg string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_status":       model.LiveStatusFailed,
			"asr_status":        model.ASRStatusFailed,
			"asr_error_msg":     msg,
			"ingest_error_msg":  msg,
			"asr_updated_at":    now,
			"last_heartbeat_at": now,
		}).Error
}

func (r *liveMaterialRepository) ResetFailedIngest(ctx context.Context, id uint, m3u8URL string) error {
	now := time.Now()
	material, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	status := model.LiveStatusConnecting
	if material.SourceMode == model.SourceModeUpcoming {
		status = model.LiveStatusWaiting
	}
	fields := map[string]interface{}{
		"live_status":       status,
		"asr_status":        model.ASRStatusPending,
		"asr_progress":      int16(0),
		"asr_error_msg":     "",
		"ingest_error_msg":  "",
		"asr_cursor_ms":     int64(0),
		"next_seg":          int64(0),
		"duration":          int64(0),
		"live_asr":          "{}",
		"asr_summaries":     "[]",
		"asr_paragraphs":    "[]",
		"stream_started_at": nil,
		"record_playlist_url": "",
		"last_heartbeat_at": now,
		"asr_version":       gorm.Expr("asr_version + 1"),
	}
	if m3u8URL != "" {
		fields["m3u8_url"] = m3u8URL
	}
	if material.SourceMode == model.SourceModeUpcoming && material.ScheduledAt != nil {
		deadline := material.ScheduledAt.Add(model.LiveWaitGrace)
		fields["wait_deadline_at"] = deadline
	}
	if material.SourceMode == model.SourceModeLive {
		deadline := now.Add(model.LiveConnectGrace)
		fields["connect_deadline_at"] = deadline
	}
	result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND live_status = ?", id, model.LiveStatusFailed).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *liveMaterialRepository) FinalizeASR(ctx context.Context, id uint, epoch int64, liveASR string, duration int64, width, height int, summaries []model.ASRSummarySegment, paragraphs []model.ASRParagraph) error {
	summariesJSON, err := marshalASRJSONArray(summaries)
	if err != nil {
		return err
	}
	paragraphsJSON, err := marshalASRJSONArray(paragraphs)
	if err != nil {
		return err
	}
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
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
			"asr_cursor_ms":    duration,
		}).Error
}
