package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

const (
	ingestHeartbeatStale = 2 * time.Minute
	// ingestProgressStuck：心跳仍在但长时间无分片进度，视为假活可抢。
	ingestProgressStuck = 5 * time.Minute
)

// LiveIngestRepository 跟播长任务仓储；录像 / 窗口 ASR / 收尾分租约。
type LiveIngestRepository interface {
	GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error)
	ClaimRecorderWork(ctx context.Context) (*model.LiveMaterial, error)
	ClaimWindowASRWork(ctx context.Context) (*model.LiveMaterial, error)
	ClaimFinalizeWork(ctx context.Context) (*model.LiveMaterial, error)
	HeartbeatIngest(ctx context.Context, id uint, epoch int64) error
	HeartbeatASR(ctx context.Context, id uint, asrEpoch int64) error
	MarkConnecting(ctx context.Context, id uint, epoch int64) error
	MarkLiveStarted(ctx context.Context, id uint, epoch int64, width, height int, resumeSeg int64) error
	UpdateRecordingProgress(ctx context.Context, id uint, epoch int64, nextSeg int64, durationMS int64, playlistURL string) error
	AppendWindowASR(ctx context.Context, id uint, asrEpoch int64, liveASR string, asrCursorMS, durationMS int64, progress int16) error
	ClearASRDue(ctx context.Context, id uint, asrEpoch int64) error
	ReleaseASRLease(ctx context.Context, id uint, asrEpoch int64) error
	MarkASRProcessing(ctx context.Context, id uint, asrEpoch int64) error
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

// ClaimRecorderWork 抢占探测/录像任务（不含 ending/ASR）。
func (r *liveMaterialRepository) ClaimRecorderWork(ctx context.Context) (*model.LiveMaterial, error) {
	tried := make(map[uint]struct{})
	now := time.Now()
	staleBefore := now.Add(-ingestHeartbeatStale)
	progressStuckBefore := now.Add(-ingestProgressStuck)
	for attempt := 0; attempt < claimOptimisticMaxAttempts; attempt++ {
		var material model.LiveMaterial
		q := r.db.WithContext(ctx).Where(
			`(live_status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?)
			 OR (live_status = ? AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?))
			 OR (live_status = ? AND (
			 		last_heartbeat_at IS NULL OR last_heartbeat_at < ?
			 		OR (last_progress_at IS NOT NULL AND last_progress_at < ?)
			 ))`,
			model.LiveStatusWaiting, now.Add(model.LiveEarlyProbe),
			model.LiveStatusConnecting, staleBefore,
			model.LiveStatusLive, staleBefore, progressStuckBefore,
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

// ClaimWindowASRWork 抢占窗口 ASR（不 bump ingest_epoch）。
func (r *liveMaterialRepository) ClaimWindowASRWork(ctx context.Context) (*model.LiveMaterial, error) {
	tried := make(map[uint]struct{})
	now := time.Now()
	staleBefore := now.Add(-ingestHeartbeatStale)
	windowMS := int64(model.LiveASRWindowDuration / time.Millisecond)
	if windowMS <= 0 {
		windowMS = 10 * 60 * 1000
	}
	chunkMS := int64(model.MaxASRTranscribeDuration / time.Millisecond)
	if chunkMS <= 0 {
		chunkMS = 2 * 60 * 1000
	}
	// 积压达到短 chunk 即可抢占，不必等满 10 分钟调度窗。
	claimThreshold := chunkMS
	if windowMS < claimThreshold {
		claimThreshold = windowMS
	}
	for attempt := 0; attempt < claimOptimisticMaxAttempts; attempt++ {
		var material model.LiveMaterial
		q := r.db.WithContext(ctx).Where(
			`live_status IN (?, ?, ?)
			 AND asr_status IN (?, ?)
			 AND (asr_due = ? OR (duration - asr_cursor_ms) >= ?)
			 AND (asr_heartbeat_at IS NULL OR asr_heartbeat_at < ?)`,
			model.LiveStatusLive, model.LiveStatusEnding, model.LiveStatusEnded,
			model.ASRStatusPending, model.ASRStatusProcessing,
			true, claimThreshold,
			staleBefore,
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
		newEpoch := material.ASREpoch + 1
		result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
			Where("id = ? AND asr_epoch = ?", material.ID, material.ASREpoch).
			Where("live_status IN ?", []string{model.LiveStatusLive, model.LiveStatusEnding, model.LiveStatusEnded}).
			Where("asr_status IN ?", []string{model.ASRStatusPending, model.ASRStatusProcessing}).
			Updates(map[string]interface{}{
				"asr_epoch":        newEpoch,
				"asr_heartbeat_at": now,
				"updated_at":       now,
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			tried[material.ID] = struct{}{}
			continue
		}
		material.ASREpoch = newEpoch
		material.ASRHeartbeatAt = &now
		return &material, nil
	}
	return nil, nil
}

// ClaimFinalizeWork 抢占关播合成 / ASR 收尾。
func (r *liveMaterialRepository) ClaimFinalizeWork(ctx context.Context) (*model.LiveMaterial, error) {
	tried := make(map[uint]struct{})
	now := time.Now()
	staleBefore := now.Add(-ingestHeartbeatStale)
	for attempt := 0; attempt < claimOptimisticMaxAttempts; attempt++ {
		var material model.LiveMaterial
		// ending：允许 last_heartbeat_at IS NULL（录像侧 MarkEnding 清心跳以便立刻交接）。
		q := r.db.WithContext(ctx).Where(
			`(live_status = ? AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?))
			 OR (live_status = ? AND asr_status = ? AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?))`,
			model.LiveStatusEnding, staleBefore,
			model.LiveStatusEnded, model.ASRStatusProcessing, staleBefore,
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
		// Finalize 不 bump ingest_epoch：本地分片目录按 e{ingest_epoch}，换代数会丢片。
		result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
			Where("id = ? AND ingest_epoch = ? AND live_status = ?", material.ID, material.IngestEpoch, material.LiveStatus).
			Where("(last_heartbeat_at IS NULL OR last_heartbeat_at < ?)", staleBefore).
			Updates(map[string]interface{}{
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

func (r *liveMaterialRepository) HeartbeatASR(ctx context.Context, id uint, asrEpoch int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_epoch = ?", id, asrEpoch).
		Updates(map[string]interface{}{
			"asr_heartbeat_at": now,
			"updated_at":       now,
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

func (r *liveMaterialRepository) MarkLiveStarted(ctx context.Context, id uint, epoch int64, width, height int, resumeSeg int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_status":        model.LiveStatusLive,
			"stream_started_at":  now,
			"width":              width,
			"height":             height,
			"ingest_resume_seg":  resumeSeg,
			"last_heartbeat_at":  now,
			"last_progress_at":   now,
			"ingest_error_msg":   "",
		}).Error
}

func (r *liveMaterialRepository) UpdateRecordingProgress(ctx context.Context, id uint, epoch int64, nextSeg int64, durationMS int64, playlistURL string) error {
	now := time.Now()
	chunkMS := int64(model.MaxASRTranscribeDuration / time.Millisecond)
	if chunkMS <= 0 {
		chunkMS = 2 * 60 * 1000
	}
	fields := map[string]interface{}{
		"next_seg":          nextSeg,
		"duration":          durationMS,
		"last_heartbeat_at": now,
		"last_progress_at":  now,
		"asr_updated_at":    now,
		"updated_at":        now,
	}
	if playlistURL != "" {
		fields["record_playlist_url"] = playlistURL
	}
	// 有足够未转写时长时置位，供 ASR Worker 抢占（按短 chunk，避免等满 10 分钟）。
	result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	_ = r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ? AND (duration - asr_cursor_ms) >= ?", id, epoch, chunkMS).
		Update("asr_due", true).Error
	return nil
}

func (r *liveMaterialRepository) AppendWindowASR(ctx context.Context, id uint, asrEpoch int64, liveASR string, asrCursorMS, durationMS int64, progress int16) error {
	now := time.Now()
	if progress < 0 {
		progress = 0
	}
	if progress > 99 {
		progress = 99
	}
	windowMS := int64(model.LiveASRWindowDuration / time.Millisecond)
	if windowMS <= 0 {
		windowMS = 10 * 60 * 1000
	}
	chunkMS := int64(model.MaxASRTranscribeDuration / time.Millisecond)
	if chunkMS <= 0 {
		chunkMS = 2 * 60 * 1000
	}
	// 剩余可转写媒体达到一个短 chunk 即继续 due，避免被 10 分钟调度窗卡住。
	dueThreshold := chunkMS
	if windowMS < dueThreshold {
		dueThreshold = windowMS
	}
	stillDue := durationMS-asrCursorMS >= dueThreshold
	result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_epoch = ?", id, asrEpoch).
		Updates(map[string]interface{}{
			"live_asr":         liveASR,
			"asr_cursor_ms":    asrCursorMS,
			"duration":         durationMS,
			"asr_progress":     progress,
			"asr_status":       model.ASRStatusProcessing,
			"asr_updated_at":   now,
			"asr_heartbeat_at": now,
			"asr_due":          stillDue,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("窗口 ASR 写库未生效（素材 %d asr_epoch %d 可能已切换）", id, asrEpoch)
	}
	return nil
}

func (r *liveMaterialRepository) ClearASRDue(ctx context.Context, id uint, asrEpoch int64) error {
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_epoch = ?", id, asrEpoch).
		Updates(map[string]interface{}{
			"asr_due":          false,
			"asr_heartbeat_at": time.Now(),
		}).Error
}

// ReleaseASRLease 主动结束本窗后清 ASR 心跳：若仍 asr_due 则置空以便立刻再抢下一窗。
func (r *liveMaterialRepository) ReleaseASRLease(ctx context.Context, id uint, asrEpoch int64) error {
	var due bool
	if err := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Select("asr_due").
		Where("id = ? AND asr_epoch = ?", id, asrEpoch).
		Scan(&due).Error; err != nil {
		return err
	}
	fields := map[string]interface{}{"updated_at": time.Now()}
	if due {
		fields["asr_heartbeat_at"] = nil
	} else {
		fields["asr_heartbeat_at"] = time.Now()
	}
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_epoch = ?", id, asrEpoch).
		Updates(fields).Error
}

func (r *liveMaterialRepository) MarkASRProcessing(ctx context.Context, id uint, asrEpoch int64) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND asr_epoch = ? AND asr_status = ?", id, asrEpoch, model.ASRStatusPending).
		Updates(map[string]interface{}{
			"asr_status":       model.ASRStatusProcessing,
			"asr_started_at":   now,
			"asr_updated_at":   now,
			"asr_heartbeat_at": now,
		}).Error
}

func (r *liveMaterialRepository) MarkEnding(ctx context.Context, id uint, epoch int64) error {
	// 清空录像心跳，便于 Finalize 立刻接手（避免等 stale）。
	return r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
		Where("id = ? AND ingest_epoch = ?", id, epoch).
		Updates(map[string]interface{}{
			"live_status":       model.LiveStatusEnding,
			"last_heartbeat_at": nil,
			"asr_due":           true,
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
			"asr_due":           true,
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
			"asr_due":           false,
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
		"live_status":         status,
		"asr_status":          model.ASRStatusPending,
		"asr_progress":        int16(0),
		"asr_error_msg":       "",
		"ingest_error_msg":    "",
		"asr_cursor_ms":       int64(0),
		"next_seg":            int64(0),
		"duration":            int64(0),
		"live_asr":            "{}",
		"asr_summaries":       "[]",
		"asr_paragraphs":      "[]",
		"stream_started_at":   nil,
		"record_playlist_url": "",
		"last_heartbeat_at":   nil,
		"asr_heartbeat_at":    nil,
		"last_progress_at":    nil,
		"asr_due":             false,
		"asr_epoch":           int64(0),
		"ingest_resume_seg":   int64(0),
		"asr_version":         gorm.Expr("asr_version + 1"),
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
	result := r.db.WithContext(ctx).Model(&model.LiveMaterial{}).
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
			"asr_due":          false,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("FinalizeASR 未生效（素材 %d epoch %d 可能已切换）", id, epoch)
	}
	return nil
}
