package repository

import (
	"context"
	"testing"
	"time"

	"live-mixer/internal/model"
)

func TestLiveIngestRepository_ClaimWaitingNearSchedule(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	soon := time.Now().Add(5 * time.Minute)
	deadline := soon.Add(model.LiveWaitGrace)
	waiting := &model.LiveMaterial{
		Name:           "即将开播",
		M3U8URL:        "https://example.com/a.m3u8",
		LiveURL:        "https://cdn.example/final.mp4",
		RecordUUID:     "abc",
		SourceMode:     model.SourceModeUpcoming,
		LiveStatus:     model.LiveStatusWaiting,
		ScheduledAt:    &soon,
		WaitDeadlineAt: &deadline,
		LiveASR:        "{}",
		ASRStatus:      model.ASRStatusPending,
		CreatedBy:      1,
	}
	if err := db.Create(waiting).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimRecorderWork(ctx)
	if err != nil {
		t.Fatalf("ClaimRecorderWork() error = %v", err)
	}
	if claimed == nil || claimed.ID != waiting.ID {
		t.Fatalf("claimed = %+v, want waiting material", claimed)
	}
}

func TestLiveIngestRepository_SkipFarFutureWaiting(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	later := time.Now().Add(3 * time.Hour)
	deadline := later.Add(model.LiveWaitGrace)
	waiting := &model.LiveMaterial{
		Name:           "很久以后",
		M3U8URL:        "https://example.com/b.m3u8",
		SourceMode:     model.SourceModeUpcoming,
		LiveStatus:     model.LiveStatusWaiting,
		ScheduledAt:    &later,
		WaitDeadlineAt: &deadline,
		LiveASR:        "{}",
		ASRStatus:      model.ASRStatusPending,
		CreatedBy:      1,
	}
	if err := db.Create(waiting).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimRecorderWork(ctx)
	if err != nil {
		t.Fatalf("ClaimRecorderWork() error = %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed = %+v, want nil for far-future waiting", claimed)
	}
}

func TestLiveIngestRepository_SkipFreshConnectingHeartbeat(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	now := time.Now()
	connecting := &model.LiveMaterial{
		Name:            "连接中有心跳",
		M3U8URL:         "https://example.com/connecting.m3u8",
		SourceMode:      model.SourceModeLive,
		LiveStatus:      model.LiveStatusConnecting,
		LastHeartbeatAt: &now,
		LiveASR:         "{}",
		ASRStatus:       model.ASRStatusPending,
		CreatedBy:       1,
	}
	if err := db.Create(connecting).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimRecorderWork(ctx)
	if err != nil {
		t.Fatalf("ClaimRecorderWork() error = %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed = %+v, want nil while connecting heartbeat is fresh", claimed)
	}
}

func TestLiveIngestRepository_ClaimStaleConnecting(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	stale := time.Now().Add(-3 * time.Minute)
	connecting := &model.LiveMaterial{
		Name:            "连接中心跳过期",
		M3U8URL:         "https://example.com/stale-connecting.m3u8",
		SourceMode:      model.SourceModeLive,
		LiveStatus:      model.LiveStatusConnecting,
		LastHeartbeatAt: &stale,
		LiveASR:         "{}",
		ASRStatus:       model.ASRStatusPending,
		CreatedBy:       1,
	}
	if err := db.Create(connecting).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimRecorderWork(ctx)
	if err != nil {
		t.Fatalf("ClaimRecorderWork() error = %v", err)
	}
	if claimed == nil || claimed.ID != connecting.ID {
		t.Fatalf("claimed = %+v, want stale connecting material", claimed)
	}
}

func TestLiveIngestRepository_ClaimProgressStuckLive(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	now := time.Now()
	stuck := now.Add(-6 * time.Minute)
	live := &model.LiveMaterial{
		Name:            "live假活",
		M3U8URL:         "https://example.com/stuck-live.m3u8",
		SourceMode:      model.SourceModeLive,
		LiveStatus:      model.LiveStatusLive,
		LastHeartbeatAt: &now,
		LastProgressAt:  &stuck,
		LiveASR:         "{}",
		ASRStatus:       model.ASRStatusProcessing,
		CreatedBy:       1,
	}
	if err := db.Create(live).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimRecorderWork(ctx)
	if err != nil {
		t.Fatalf("ClaimRecorderWork() error = %v", err)
	}
	if claimed == nil || claimed.ID != live.ID {
		t.Fatalf("claimed = %+v, want progress-stuck live", claimed)
	}
}

func TestLiveIngestRepository_ClaimWindowASRDoesNotBumpIngestEpoch(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	now := time.Now()
	mat := &model.LiveMaterial{
		Name:            "asr-due",
		M3U8URL:         "https://example.com/asr.m3u8",
		SourceMode:      model.SourceModeLive,
		LiveStatus:      model.LiveStatusLive,
		IngestEpoch:     7,
		ASREpoch:        2,
		LastHeartbeatAt: &now,
		LastProgressAt:  &now,
		ASRDue:          true,
		Duration:        700_000,
		ASRCursorMS:     0,
		LiveASR:         "{}",
		ASRStatus:       model.ASRStatusPending,
		CreatedBy:       1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimWindowASRWork(ctx)
	if err != nil {
		t.Fatalf("ClaimWindowASRWork() error = %v", err)
	}
	if claimed == nil || claimed.ID != mat.ID {
		t.Fatalf("claimed = %+v, want asr-due material", claimed)
	}
	if claimed.IngestEpoch != 7 {
		t.Fatalf("ingest_epoch = %d, want unchanged 7", claimed.IngestEpoch)
	}
	if claimed.ASREpoch != 3 {
		t.Fatalf("asr_epoch = %d, want 3", claimed.ASREpoch)
	}

	rec, err := repo.ClaimRecorderWork(ctx)
	if err != nil {
		t.Fatalf("ClaimRecorderWork() error = %v", err)
	}
	if rec != nil {
		t.Fatalf("recorder claimed = %+v, want nil while heartbeat/progress fresh", rec)
	}
}

func TestLiveIngestRepository_SkipFreshASRHeartbeat(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	now := time.Now()
	mat := &model.LiveMaterial{
		Name:           "asr-busy",
		M3U8URL:        "https://example.com/asr-busy.m3u8",
		SourceMode:     model.SourceModeLive,
		LiveStatus:     model.LiveStatusLive,
		ASRDue:         true,
		ASRHeartbeatAt: &now,
		Duration:       700_000,
		LiveASR:        "{}",
		ASRStatus:      model.ASRStatusProcessing,
		CreatedBy:      1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimWindowASRWork(ctx)
	if err != nil {
		t.Fatalf("ClaimWindowASRWork() error = %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed = %+v, want nil while asr heartbeat fresh", claimed)
	}
}

func TestLiveIngestRepository_ClaimFinalizeEnding(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	ending := &model.LiveMaterial{
		Name:       "ending-handoff",
		M3U8URL:    "https://example.com/ending.m3u8",
		SourceMode: model.SourceModeLive,
		LiveStatus: model.LiveStatusEnding,
		// last_heartbeat_at NULL → Finalize 可立刻接手
		LiveASR:    "{}",
		ASRStatus:  model.ASRStatusProcessing,
		CreatedBy:  1,
	}
	if err := db.Create(ending).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimFinalizeWork(ctx)
	if err != nil {
		t.Fatalf("ClaimFinalizeWork() error = %v", err)
	}
	if claimed == nil || claimed.ID != ending.ID {
		t.Fatalf("claimed = %+v, want ending material", claimed)
	}
}

func TestLiveIngestRepository_ClaimEndedASRProcessing(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	ended := &model.LiveMaterial{
		Name:        "已关播未 Finalize",
		M3U8URL:     "https://example.com/ended.m3u8",
		LiveURL:     "https://cdn.example/final.mp4",
		RecordUUID:  "ended1",
		SourceMode:  model.SourceModeLive,
		LiveStatus:  model.LiveStatusEnded,
		LiveASR:     `{"result":{"utterances":[]}}`,
		ASRStatus:   model.ASRStatusProcessing,
		ASRProgress: 90,
		Duration:    1000,
		CreatedBy:   1,
	}
	if err := db.Create(ended).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimFinalizeWork(ctx)
	if err != nil {
		t.Fatalf("ClaimFinalizeWork() error = %v", err)
	}
	if claimed == nil || claimed.ID != ended.ID {
		t.Fatalf("claimed = %+v, want ended processing material", claimed)
	}
}
