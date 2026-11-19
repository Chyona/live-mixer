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
		SourceMode:     model.SourceModeLive,
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
		SourceMode:     model.SourceModeLive,
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

func TestLiveIngestRepository_AppendWindowASRWritesParagraphs(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	mat := &model.LiveMaterial{
		Name:       "append-paras",
		M3U8URL:    "https://example.com/append.m3u8",
		LiveURL:    "https://cdn.example/final.mp4",
		RecordUUID: "ap1",
		SourceMode: model.SourceModeLive,
		LiveStatus: model.LiveStatusLive,
		ASREpoch:   2,
		LiveASR:    "{}",
		ASRStatus:  model.ASRStatusPending,
		CreatedBy:  1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	liveASR := `{"result":{"utterances":[{"start_time":0,"end_time":100,"text":"hi","additions":{"speaker":"1"},"words":[{"text":"hi","start_time":0,"end_time":100}]}]}}`
	paras := []model.ASRParagraph{{
		Speaker: "1", Text: "hi", StartTime: 0, EndTime: 100,
		Words: []model.ClipWord{{Text: "hi", StartTime: 0, EndTime: 100}},
	}}
	if err := repo.AppendWindowASR(ctx, mat.ID, 2, liveASR, 100, 100, 50, true, paras); err != nil {
		t.Fatalf("AppendWindowASR: %v", err)
	}
	got, err := repo.GetByID(ctx, mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRCursorMS != 100 || got.ASRProgress != 50 || !got.ASRDue {
		t.Fatalf("cursor/progress/due = %d/%d/%v", got.ASRCursorMS, got.ASRProgress, got.ASRDue)
	}
	if got.ASRStatus != model.ASRStatusProcessing {
		t.Fatalf("asr_status=%q", got.ASRStatus)
	}
	if len(got.ASRParagraphs) != 1 || got.ASRParagraphs[0].Text != "hi" {
		t.Fatalf("ASRParagraphs=%+v", got.ASRParagraphs)
	}
	if got.LiveASR != liveASR {
		t.Fatalf("LiveASR not updated")
	}
}

// newLiveASRGateMaterial 造一条可直接被 ClaimWindowASRWork 抢占的直播素材。
func newLiveASRGateMaterial(name string) *model.LiveMaterial {
	return &model.LiveMaterial{
		Name:         name,
		M3U8URL:      "https://example.com/" + name + ".m3u8",
		LiveURL:      "https://cdn.example/" + name + ".mp4",
		RecordUUID:   name,
		SourceMode:   model.SourceModeLive,
		LiveStatus:   model.LiveStatusLive,
		LiveASR:      "{}",
		MediaWindows: "[]",
		ASRStatus:    model.ASRStatusPending,
		ASRDue:       true,
		CreatedBy:    1,
	}
}

// TestLiveIngestRepository_ClaimWindowASRWorkRespectsNextAttemptGate 重试时间门未到时不可抢占，
// 到点后必须可抢占（避免推迟后 ASR 永久停摆）。
func TestLiveIngestRepository_ClaimWindowASRWorkRespectsNextAttemptGate(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	future := time.Now().Add(5 * time.Minute)
	mat := newLiveASRGateMaterial("asr-gate")
	mat.ASRNextAttemptAt = &future
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	claimed, err := repo.ClaimWindowASRWork(ctx)
	if err != nil {
		t.Fatalf("ClaimWindowASRWork: %v", err)
	}
	if claimed != nil {
		t.Fatalf("时间门未到时不应被抢占，got id=%d", claimed.ID)
	}

	past := time.Now().Add(-time.Second)
	if err := db.Model(&model.LiveMaterial{}).Where("id = ?", mat.ID).
		Update("asr_next_attempt_at", past).Error; err != nil {
		t.Fatalf("set past gate: %v", err)
	}
	claimed, err = repo.ClaimWindowASRWork(ctx)
	if err != nil {
		t.Fatalf("ClaimWindowASRWork: %v", err)
	}
	if claimed == nil || claimed.ID != mat.ID {
		t.Fatalf("时间门过后应可抢占，got %+v", claimed)
	}
}

// TestLiveIngestRepository_DeferASRDueKeepsStreakAnchor 推迟必须保留 asr_due、清心跳，
// 且连续推迟起点只写一次（否则保底计时每次重试都被清零，永远等不到强制跑）。
func TestLiveIngestRepository_DeferASRDueKeepsStreakAnchor(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	mat := newLiveASRGateMaterial("asr-defer")
	mat.ASRDue = false
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	first := time.Now().Add(90 * time.Second)
	if err := repo.DeferASRDue(ctx, mat.ID, 0, first); err != nil {
		t.Fatalf("DeferASRDue: %v", err)
	}
	got, err := repo.GetByID(ctx, mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.ASRDue {
		t.Fatal("推迟不应清掉 asr_due（否则丢掉待跑状态）")
	}
	if got.ASRNextAttemptAt == nil || !got.ASRNextAttemptAt.After(time.Now()) {
		t.Fatalf("asr_next_attempt_at 未写为未来时间: %+v", got.ASRNextAttemptAt)
	}
	if got.ASRHeartbeatAt != nil {
		t.Fatalf("推迟应清 ASR 心跳以免被心跳门挡住，got %+v", got.ASRHeartbeatAt)
	}
	if got.ASRDeferredSince == nil {
		t.Fatal("推迟链首应写入 asr_deferred_since")
	}
	anchor := *got.ASRDeferredSince

	second := first.Add(90 * time.Second)
	if err := repo.DeferASRDue(ctx, mat.ID, 0, second); err != nil {
		t.Fatalf("DeferASRDue(second): %v", err)
	}
	got, err = repo.GetByID(ctx, mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRDeferredSince == nil || !got.ASRDeferredSince.Equal(anchor) {
		t.Fatalf("连续推迟起点被刷新：got %+v want %s", got.ASRDeferredSince, anchor)
	}
	if got.ASRNextAttemptAt == nil || !got.ASRNextAttemptAt.After(first) {
		t.Fatalf("重试时间门未前移：got %+v want after %s", got.ASRNextAttemptAt, first)
	}

	if err := repo.ClearASRDefer(ctx, mat.ID, 0); err != nil {
		t.Fatalf("ClearASRDefer: %v", err)
	}
	got, err = repo.GetByID(ctx, mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRNextAttemptAt != nil || got.ASRDeferredSince != nil {
		t.Fatalf("ClearASRDefer 未清干净: %+v / %+v", got.ASRNextAttemptAt, got.ASRDeferredSince)
	}
	if !got.ASRDue {
		t.Fatal("ClearASRDefer 不应动 asr_due")
	}
}

// TestLiveIngestRepository_MarkEndingClearsASRDeferGate 关播收尾的全量补跑不能被直播期的
// 重试时间门挡住（这是新引入的时间门最容易踩的坑）。
func TestLiveIngestRepository_MarkEndingClearsASRDeferGate(t *testing.T) {
	db := setupLiveMaterialTestDB(t)
	repo := NewLiveIngestRepository(db)
	ctx := context.Background()

	future := time.Now().Add(5 * time.Minute)
	mat := newLiveASRGateMaterial("asr-ending")
	mat.ASRDue = false
	mat.ASRNextAttemptAt = &future
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.MarkEnding(ctx, mat.ID, 0); err != nil {
		t.Fatalf("MarkEnding: %v", err)
	}
	got, err := repo.GetByID(ctx, mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.LiveStatus != model.LiveStatusEnding || !got.ASRDue {
		t.Fatalf("MarkEnding 后 status/due = %q/%v", got.LiveStatus, got.ASRDue)
	}
	if got.ASRNextAttemptAt != nil || got.ASRDeferredSince != nil {
		t.Fatalf("MarkEnding 应清 ASR 推迟门: %+v / %+v", got.ASRNextAttemptAt, got.ASRDeferredSince)
	}

	claimed, err := repo.ClaimWindowASRWork(ctx)
	if err != nil {
		t.Fatalf("ClaimWindowASRWork: %v", err)
	}
	if claimed == nil || claimed.ID != mat.ID {
		t.Fatalf("关播后必须立刻可抢 ASR，got %+v", claimed)
	}
}

