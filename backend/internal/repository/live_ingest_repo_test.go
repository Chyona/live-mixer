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

	claimed, err := repo.ClaimIngestWork(ctx)
	if err != nil {
		t.Fatalf("ClaimIngestWork() error = %v", err)
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

	claimed, err := repo.ClaimIngestWork(ctx)
	if err != nil {
		t.Fatalf("ClaimIngestWork() error = %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed = %+v, want nil for far-future waiting", claimed)
	}
}
