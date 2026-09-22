package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestSealMediaWindow_DoesNotSetASRDueOrDuration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	ctx := context.Background()

	m := &model.LiveMaterial{
		Name:         "seal-test",
		M3U8URL:      "https://example.com/a.m3u8",
		LiveURL:      "https://cdn.example/pre.mp4",
		RecordUUID:   "sealuuid1",
		SourceMode:   model.SourceModeLive,
		LiveStatus:   model.LiveStatusLive,
		IngestEpoch:  1,
		MediaWindows: "[]",
		Duration:     0,
		ASRDue:       false,
		LiveASR:      "{}",
		ASRStatus:    model.ASRStatusPending,
		CreatedBy:    1,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	windows := model.MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true, ObjectKey: "k0"},
	}.Marshal()
	if err := repo.SealMediaWindow(ctx, m.ID, 1, windows, 1); err != nil {
		t.Fatalf("SealMediaWindow: %v", err)
	}

	got, err := repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ASRDue {
		t.Fatal("seal must not set asr_due")
	}
	if got.Duration != 0 {
		t.Fatalf("seal must not set duration, got %d", got.Duration)
	}
	if got.NextWindowSeg != 1 {
		t.Fatalf("NextWindowSeg=%d, want 1", got.NextWindowSeg)
	}
	if got.ParsedMediaWindows().ReadyCount() != 1 {
		t.Fatalf("ready windows=%d, want 1", got.ParsedMediaWindows().ReadyCount())
	}
}

func TestCommitMasterMP4_SetsASRDue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	ctx := context.Background()

	m := &model.LiveMaterial{
		Name:         "master-commit",
		M3U8URL:      "https://example.com/b.m3u8",
		LiveURL:      "https://cdn.example/pre.mp4",
		RecordUUID:   "masteruuid1",
		SourceMode:   model.SourceModeLive,
		LiveStatus:   model.LiveStatusLive,
		IngestEpoch:  2,
		MediaWindows: "[]",
		LiveASR:      "{}",
		ASRStatus:    model.ASRStatusPending,
		CreatedBy:    1,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	windows := model.MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true, URL: "https://cdn/w0.mp4"},
	}.Marshal()
	if err := repo.CommitMasterMP4(ctx, m.ID, 2, windows, 1, 600000, "https://cdn/master.mp4", true); err != nil {
		t.Fatalf("CommitMasterMP4: %v", err)
	}
	got, err := repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !got.ASRDue {
		t.Fatal("CommitMasterMP4 should set asr_due")
	}
	if got.Duration != 600000 {
		t.Fatalf("Duration=%d, want 600000", got.Duration)
	}
}

func TestCommitMasterMP4_MergesWindowsAndDoesNotRewindCursor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.LiveMaterial{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewLiveIngestRepository(db)
	ctx := context.Background()

	existing := model.MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true, URL: "https://cdn/w0.mp4"},
		{Index: 1, StartMS: 600000, EndMS: 1200000, DurMS: 600000, Ready: true, ObjectKey: "k1"},
	}.Marshal()
	m := &model.LiveMaterial{
		Name:          "merge-commit",
		M3U8URL:       "https://example.com/c.m3u8",
		LiveURL:       "https://cdn.example/old.mp4",
		RecordUUID:    "mergeuuid1",
		SourceMode:    model.SourceModeLive,
		LiveStatus:    model.LiveStatusLive,
		IngestEpoch:   3,
		MediaWindows:  existing,
		NextWindowSeg: 2,
		NextSeg:       2,
		Duration:      600000,
		ASRDue:        true,
		LiveASR:       "{}",
		ASRStatus:     model.ASRStatusPending,
		CreatedBy:     1,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// 旧 master 任务快照：只有窗 0，且 next 回拨到 1、asrDue=false
	stale := model.MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true},
	}.Marshal()
	if err := repo.CommitMasterMP4(ctx, m.ID, 3, stale, 1, 600000, "https://cdn/master.mp4", false); err != nil {
		t.Fatalf("CommitMasterMP4: %v", err)
	}
	got, err := repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	wins := got.ParsedMediaWindows()
	if wins.ReadyCount() != 2 {
		t.Fatalf("ReadyCount=%d, want 2 (must keep window 1)", wins.ReadyCount())
	}
	if wins[0].URL != "https://cdn/w0.mp4" {
		t.Fatalf("window0 URL wiped: %q", wins[0].URL)
	}
	if got.NextWindowSeg != 2 {
		t.Fatalf("NextWindowSeg=%d, want 2 (must not rewind)", got.NextWindowSeg)
	}
	if !got.ASRDue {
		t.Fatal("asr_due must stay true when commit passes asrDue=false")
	}
	if got.LiveURL != "https://cdn/master.mp4" {
		t.Fatalf("LiveURL=%q", got.LiveURL)
	}
}

func TestLiveASRDueForMasterJob_Throttle(t *testing.T) {
	live := &model.LiveMaterial{LiveStatus: model.LiveStatusLive}
	if !liveASRDueForMasterJob(live, masterJob{winIdx: 0, partial: false}) {
		t.Fatal("win0 should always set ASR while live")
	}
	if liveASRDueForMasterJob(live, masterJob{winIdx: 1, partial: false}) {
		t.Fatal("win1 should skip ASR while live")
	}
	if !liveASRDueForMasterJob(live, masterJob{winIdx: 2, partial: false}) {
		t.Fatal("win2 should set ASR while live")
	}
	if !liveASRDueForMasterJob(live, masterJob{winIdx: 1, partial: true}) {
		t.Fatal("partial should always set ASR")
	}
	ending := &model.LiveMaterial{LiveStatus: model.LiveStatusEnding}
	if !liveASRDueForMasterJob(ending, masterJob{winIdx: 1, partial: false}) {
		t.Fatal("non-live should always set ASR")
	}
}

func TestShouldDeferLiveASR_Lag(t *testing.T) {
	w := &liveIngestWorker{
		logger:       zap.NewNop(),
		masterQueues: make(map[uint]*materialMasterQueue),
	}
	m := &model.LiveMaterial{
		LiveStatus:    model.LiveStatusLive,
		MediaWindowMS: 600000,
		Duration:      600000,
		MediaWindows: model.MediaWindowList{
			{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true},
			{Index: 1, StartMS: 600000, EndMS: 1200000, DurMS: 600000, Ready: true},
			{Index: 2, StartMS: 1200000, EndMS: 1800000, DurMS: 600000, Ready: true},
		}.Marshal(),
	}
	// sealed 30min, master 10min → lag 2 windows > 1 → defer
	if !w.shouldDeferLiveASR(m) {
		t.Fatal("expected defer when lag > 1 window")
	}
	m.Duration = 1200000 // lag = 1 window exactly → not defer
	if w.shouldDeferLiveASR(m) {
		t.Fatal("lag == 1 window should not defer")
	}
	m.LiveStatus = model.LiveStatusEnding
	m.Duration = 600000
	if w.shouldDeferLiveASR(m) {
		t.Fatal("ending should not defer")
	}
}

// TestShouldDeferLiveASR_RealWindowDrift 回归线上事故：真实媒体窗落盘时长比标称 600000ms
// 多出几十毫秒，累计进窗的 EndMS（TotalReadyMS 取的是 EndMS，不是标称窗数×步长），
// 于是「master 恰好落后 1 个窗」这个直播稳态被判成落后 2 个窗，
// 每个节流点都推迟、ASR 整场停在 30 分钟。数值取自事故日志。
func TestShouldDeferLiveASR_RealWindowDrift(t *testing.T) {
	w := &liveIngestWorker{
		logger:       zap.NewNop(),
		masterQueues: make(map[uint]*materialMasterQueue),
	}
	// 每窗真实落盘 ~600037ms：窗的 StartMS/EndMS 按真实时长推进，累计漂移落在 EndMS 上。
	const realWindowMS int64 = 600037
	windows := make(model.MediaWindowList, 0, 7)
	for i := 0; i < 7; i++ {
		windows = append(windows, model.MediaWindow{
			Index:   i,
			StartMS: int64(i) * realWindowMS,
			EndMS:   int64(i+1) * realWindowMS,
			DurMS:   realWindowMS,
			Ready:   true,
		})
	}
	// sealed 7 窗 = 4200259ms，master 6 窗 = 3600210ms → lag = 600049（1 个窗 + 49ms 漂移）。
	m := &model.LiveMaterial{
		LiveStatus:    model.LiveStatusLive,
		MediaWindowMS: 600000,
		Duration:      3600210,
		MediaWindows:  windows.Marshal(),
	}
	if got := m.ParsedMediaWindows().TotalReadyMS(); got != 4200259 {
		t.Fatalf("sealed_ms=%d, want 4200259（本用例依赖累计真实时长，不是标称窗宽）", got)
	}
	if w.shouldDeferLiveASR(m) {
		t.Fatal("lag of one window plus sub-second drift must not defer")
	}
	// 落后 2 个窗（master 5 窗 = 3000184ms）才推迟。
	m.Duration = 3000184
	if !w.shouldDeferLiveASR(m) {
		t.Fatal("expected defer when master lags two windows")
	}
}

func TestMasterAppendDurationSlackConstant(t *testing.T) {
	if masterAppendDurationSlackMS < 500 {
		t.Fatalf("slack too tight: %d", masterAppendDurationSlackMS)
	}
}

func TestWaitMasterQueueDrain_Empty(t *testing.T) {
	raw := NewLiveIngestWorker(nil, nil, nil, nil, nil, webroot.Config{}, zap.NewNop(), 1).(*liveIngestWorker)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := raw.waitMasterQueueDrain(ctx, 42); err != nil {
		t.Fatalf("empty drain: %v", err)
	}
}

func TestSnapshotMasterMP4(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "master.mp4")
	if err := os.WriteFile(src, []byte("fake-mp4-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := &liveIngestWorker{logger: zap.NewNop()}
	snap, err := w.snapshotMasterMP4(src, 9, 12345)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(snap)
	got, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fake-mp4-bytes" {
		t.Fatalf("snapshot content mismatch: %q", got)
	}
}
