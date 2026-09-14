package service

import (
	"context"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestRebuildLiveASRParagraphs_MergesSameSpeaker(t *testing.T) {
	w := &liveIngestWorker{logger: zap.NewNop()}
	liveASR := `{
		"audio_info":{"duration":5000},
		"result":{"utterances":[
			{"start_time":0,"end_time":2000,"text":"你好世界","additions":{"speaker":"1"},
				"words":[{"text":"你","start_time":0,"end_time":500},{"text":"好","start_time":500,"end_time":1000},{"text":"世","start_time":1000,"end_time":1500},{"text":"界","start_time":1500,"end_time":2000}]},
			{"start_time":2100,"end_time":4000,"text":"欢迎收看","additions":{"speaker":"1"},
				"words":[{"text":"欢","start_time":2100,"end_time":2500},{"text":"迎","start_time":2500,"end_time":3000},{"text":"收","start_time":3000,"end_time":3500},{"text":"看","start_time":3500,"end_time":4000}]}
		]}
	}`
	paras := w.rebuildLiveASRParagraphs(liveASR, 4000)
	if len(paras) != 1 {
		t.Fatalf("paras=%d want 1, texts=%v", len(paras), paras)
	}
	if paras[0].Text != "你好世界欢迎收看" {
		t.Fatalf("text=%q", paras[0].Text)
	}
}

func TestRebuildLiveASRParagraphs_EmptyOrInvalid(t *testing.T) {
	w := &liveIngestWorker{logger: zap.NewNop()}
	if got := w.rebuildLiveASRParagraphs("{}", 0); len(got) != 0 {
		t.Fatalf("empty asr: %+v", got)
	}
	// 校验失败（text 有内容但 words 空）→ 降级保留段落，禁止写空 []
	bad := `{
		"result":{"utterances":[
			{"start_time":0,"end_time":100,"text":"有字无词","additions":{"speaker":"1"},"words":[]}
		]}
	}`
	got := w.rebuildLiveASRParagraphs(bad, 100)
	if len(got) != 1 || got[0].Text != "有字无词" {
		t.Fatalf("invalid words should degrade-keep paragraph, got %+v", got)
	}
}

func TestCommitFullMasterASR_OverwritesAndClearsDue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	mat := &model.LiveMaterial{
		Name:         "full-asr",
		LiveURL:      "https://example.com/master.mp4",
		RecordUUID:   "u-full",
		M3U8URL:      "https://example.com/x.m3u8",
		SourceMode:   model.SourceModeLive,
		LiveStatus:   model.LiveStatusLive,
		ASREpoch:     3,
		LiveASR:      `{"result":{"utterances":[{"text":"旧"}]}}`,
		ASRStatus:    model.ASRStatusProcessing,
		ASRCursorMS:  0,
		Duration:     200,
		ASRDue:       true,
		CreatedBy:    1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	w := &liveIngestWorker{repo: repo, logger: zap.NewNop()}
	liveASR := `{
		"result":{"utterances":[
			{"start_time":0,"end_time":100,"text":"甲","additions":{"speaker":"1"},
				"words":[{"text":"甲","start_time":0,"end_time":100}]},
			{"start_time":120,"end_time":200,"text":"乙","additions":{"speaker":"1"},
				"words":[{"text":"乙","start_time":120,"end_time":200}]}
		]}
	}`
	if err := w.commitFullMasterASR(context.Background(), mat, 3, 200, liveASR, 200, 1.0, fullMasterASRTiming{
		StartedAt:     time.Now(),
		TargetReadyMS: 200,
		Outcome:       "ok",
	}); err != nil {
		t.Fatalf("commitFullMasterASR: %v", err)
	}
	got, err := repo.GetByID(context.Background(), mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRCursorMS != 200 {
		t.Fatalf("cursor=%d want 200 (readyMS)", got.ASRCursorMS)
	}
	if got.ASRDue {
		t.Fatal("asr_due should be false when master unchanged")
	}
	if got.LiveASR != liveASR {
		t.Fatalf("live_asr not overwritten")
	}
	if len(got.ASRParagraphs) != 1 || got.ASRParagraphs[0].Text != "甲乙" {
		t.Fatalf("ASRParagraphs=%+v", got.ASRParagraphs)
	}
}

func TestCommitFullMasterASR_StaleKeepsDue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	mat := &model.LiveMaterial{
		Name:       "stale-asr",
		LiveURL:    "https://example.com/master.mp4",
		RecordUUID: "u-stale",
		M3U8URL:    "https://example.com/x.m3u8",
		SourceMode: model.SourceModeLive,
		LiveStatus: model.LiveStatusLive,
		ASREpoch:   1,
		LiveASR:    "{}",
		ASRStatus:  model.ASRStatusProcessing,
		Duration:   600000, // commit 前已变长到 10 分钟
		CreatedBy:  1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	w := &liveIngestWorker{repo: repo, logger: zap.NewNop()}
	liveASR := `{
		"result":{"utterances":[
			{"start_time":0,"end_time":100,"text":"过渡","additions":{"speaker":"1"},
				"words":[{"text":"过","start_time":0,"end_time":50},{"text":"渡","start_time":50,"end_time":100}]}
		]}
	}`
	const targetReadyMS int64 = 300000 // 任务开始时是 5 分钟
	if err := w.commitFullMasterASR(context.Background(), mat, 1, targetReadyMS, liveASR, targetReadyMS, 1.0, fullMasterASRTiming{
		StartedAt:     time.Now(),
		TargetReadyMS: targetReadyMS,
		WindowCount:   1,
		Outcome:       "ok",
	}); err != nil {
		t.Fatalf("commitFullMasterASR: %v", err)
	}
	got, err := repo.GetByID(context.Background(), mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRCursorMS != targetReadyMS {
		t.Fatalf("cursor=%d want %d", got.ASRCursorMS, targetReadyMS)
	}
	if !got.ASRDue {
		t.Fatal("asr_due should stay true when master grew past targetReadyMS")
	}
	if got.Duration != 600000 {
		t.Fatalf("duration=%d want 600000 (current ready)", got.Duration)
	}
	if got.LiveASR != liveASR {
		t.Fatal("stale job should still write transitional live_asr")
	}
}

func TestCommitMasterASRProgress_RebuildsParagraphs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	mat := &model.LiveMaterial{
		Name:       "chunk-paras",
		LiveURL:    "https://example.com/x.mp4",
		RecordUUID: "u1",
		M3U8URL:    "https://example.com/x.m3u8",
		SourceMode: model.SourceModeLive,
		LiveStatus: model.LiveStatusLive,
		ASREpoch:   3,
		LiveASR:    "{}",
		ASRStatus:  model.ASRStatusProcessing,
		Duration:   200,
		CreatedBy:  1,
	}
	if err := db.Create(mat).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	w := &liveIngestWorker{repo: repo, logger: zap.NewNop()}
	liveASR := `{
		"result":{"utterances":[
			{"start_time":0,"end_time":100,"text":"甲","additions":{"speaker":"1"},
				"words":[{"text":"甲","start_time":0,"end_time":100}]},
			{"start_time":120,"end_time":200,"text":"乙","additions":{"speaker":"1"},
				"words":[{"text":"乙","start_time":120,"end_time":200}]}
		]}
	}`
	if err := w.commitMasterASRProgress(context.Background(), mat, 3, 200, 100, 0, liveASR, 100, 1.0, time.Now()); err != nil {
		t.Fatalf("commitMasterASRProgress: %v", err)
	}
	got, err := repo.GetByID(context.Background(), mat.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRCursorMS != 200 {
		t.Fatalf("cursor=%d want 200 (full master readyMS)", got.ASRCursorMS)
	}
	if len(got.ASRParagraphs) != 1 || got.ASRParagraphs[0].Text != "甲乙" {
		t.Fatalf("ASRParagraphs=%+v", got.ASRParagraphs)
	}
	if len(mat.ASRParagraphs) != 1 || mat.ASRParagraphs[0].Text != "甲乙" {
		t.Fatalf("in-memory ASRParagraphs=%+v", mat.ASRParagraphs)
	}
}
