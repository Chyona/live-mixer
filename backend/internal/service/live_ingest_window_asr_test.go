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
	// 校验失败（text 有内容但 words 空）→ 隔离为空，不 panic
	bad := `{
		"result":{"utterances":[
			{"start_time":0,"end_time":100,"text":"有字无词","additions":{"speaker":"1"},"words":[]}
		]}
	}`
	if got := w.rebuildLiveASRParagraphs(bad, 100); len(got) != 0 {
		t.Fatalf("invalid words should yield empty on isolation, got %+v", got)
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
	if got.ASRCursorMS != 100 {
		t.Fatalf("cursor=%d want 100", got.ASRCursorMS)
	}
	if len(got.ASRParagraphs) != 1 || got.ASRParagraphs[0].Text != "甲乙" {
		t.Fatalf("ASRParagraphs=%+v", got.ASRParagraphs)
	}
	if len(mat.ASRParagraphs) != 1 || mat.ASRParagraphs[0].Text != "甲乙" {
		t.Fatalf("in-memory ASRParagraphs=%+v", mat.ASRParagraphs)
	}
}
