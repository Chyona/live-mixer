package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestUpdateRecordingProgress_DoesNotChangeDuration(t *testing.T) {
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
		Name:         "progress",
		M3U8URL:      "https://example.com/a.m3u8",
		LiveURL:      "https://cdn.example/master.mp4",
		RecordUUID:   "progress-uuid",
		SourceMode:   model.SourceModeLive,
		LiveStatus:   model.LiveStatusLive,
		IngestEpoch:  1,
		MediaWindows: "[]",
		Duration:     600000,
		LiveASR:      "{}",
		ASRStatus:    model.ASRStatusPending,
		CreatedBy:    1,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.UpdateRecordingProgress(ctx, m.ID, 1, 5, 0, ""); err != nil {
		t.Fatalf("UpdateRecordingProgress: %v", err)
	}
	got, err := repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Duration != 600000 {
		t.Fatalf("Duration = %d, want 600000 (progress must not write duration)", got.Duration)
	}
	if got.NextSeg != 5 {
		t.Fatalf("NextSeg = %d, want 5", got.NextSeg)
	}
	if err := repo.UpdateRecordingProgress(ctx, m.ID, 1, 6, 999999, ""); err != nil {
		t.Fatalf("UpdateRecordingProgress ahead: %v", err)
	}
	got, err = repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Duration != 600000 {
		t.Fatalf("Duration = %d, want 600000 after inflated progress value", got.Duration)
	}
}

func TestSealMediaWindow_URLKeepsDurationAndExistingURL(t *testing.T) {
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
		Name:         "url-seal",
		M3U8URL:      "https://example.com/a.m3u8",
		LiveURL:      "https://cdn.example/master.mp4",
		RecordUUID:   "url-uuid",
		SourceMode:   model.SourceModeLive,
		LiveStatus:   model.LiveStatusLive,
		IngestEpoch:  1,
		MediaWindows: "[]",
		Duration:     11401029,
		LiveASR:      "{}",
		ASRStatus:    model.ASRStatusPending,
		CreatedBy:    1,
	}
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	withURL := model.MediaWindowList{{
		Index: 19, StartMS: 10800000, EndMS: 11400000, DurMS: 600000, Ready: true,
		URL: "https://cdn.example/window_00019.mp4", ObjectKey: "live-record/url-uuid/windows/window_00019.mp4",
	}}.Marshal()
	if err := repo.SealMediaWindow(ctx, m.ID, 1, withURL, 20); err != nil {
		t.Fatalf("SealMediaWindow: %v", err)
	}
	got, err := repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Duration != 11401029 {
		t.Fatalf("Duration = %d, want unchanged 11401029", got.Duration)
	}
	wins := got.ParsedMediaWindows()
	if len(wins) != 1 || wins[0].URL == "" {
		t.Fatalf("window URL missing: %+v", wins)
	}
	// 后续封窗快照若漏带 URL，合并不能抹掉已落库的地址。
	emptyURL := model.MediaWindowList{{
		Index: 19, StartMS: 10800000, EndMS: 11400000, DurMS: 600000, Ready: true,
	}}.Marshal()
	if err := repo.SealMediaWindow(ctx, m.ID, 1, emptyURL, 20); err != nil {
		t.Fatalf("SealMediaWindow overlay: %v", err)
	}
	got, err = repo.GetByID(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	wins = got.ParsedMediaWindows()
	if len(wins) != 1 || wins[0].URL != "https://cdn.example/window_00019.mp4" {
		t.Fatalf("URL wiped: %+v", wins)
	}
	if got.Duration != 11401029 {
		t.Fatalf("Duration = %d, want 11401029", got.Duration)
	}
}

func newResolveWorker(t *testing.T) (*liveIngestWorker, string) {
	t.Helper()
	root := t.TempDir()
	w := &liveIngestWorker{
		logger: zap.NewNop(),
		web:    webroot.Config{RootDir: root},
	}
	return w, root
}

func TestResolveWindowFile_PrefersCurrentThenOlderEpoch(t *testing.T) {
	w, root := newResolveWorker(t)
	m := &model.LiveMaterial{ID: 3, IngestEpoch: 2, RecordUUID: "cb4956", NextWindowSeg: 20}
	win := model.MediaWindow{Index: 19, Ready: true, ObjectKey: "live-record/cb4956/windows/window_00019.mp4"}

	oldDir := filepath.Join(root, "staging", "live_ingest", "3", "e1", "windows")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "window_00019.mp4"), []byte("from-e1"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := w.resolveWindowFile(context.Background(), m, win)
	if err != nil {
		t.Fatalf("resolve older epoch: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from-e1" {
		t.Fatalf("body = %q, want from-e1", body)
	}
	current := filepath.Join(root, "staging", "live_ingest", "3", "e2", "windows", "window_00019.mp4")
	if got != current {
		t.Fatalf("path = %s, want copied into current epoch %s", got, current)
	}

	if err := os.WriteFile(current, []byte("from-e2"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = w.resolveWindowFile(context.Background(), m, win)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	body, err = os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from-e2" {
		t.Fatalf("current epoch not preferred, body = %q", body)
	}
}

func TestResolveWindowFile_URLDownloadAndUnrecoverable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("from-url"))
	}))
	defer srv.Close()

	w, _ := newResolveWorker(t)
	m := &model.LiveMaterial{ID: 3, IngestEpoch: 2, RecordUUID: "cb4956", NextWindowSeg: 20}
	win := model.MediaWindow{Index: 19, Ready: true, URL: srv.URL}
	got, err := w.resolveWindowFile(context.Background(), m, win)
	if err != nil {
		t.Fatalf("download url: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from-url" {
		t.Fatalf("body = %q", body)
	}

	missingWorker, _ := newResolveWorker(t)
	missing := model.MediaWindow{Index: 19, Ready: true, ObjectKey: "live-record/cb4956/windows/window_00019.mp4"}
	_, err = missingWorker.resolveWindowFile(context.Background(), m, missing)
	if !errors.Is(err, errWindowUnrecoverable) {
		t.Fatalf("err = %v, want unrecoverable when object key has no storage client", err)
	}
}

func TestAbandonUnrecoverableWindowJob(t *testing.T) {
	err := &windowUnrecoverableError{index: 19}
	if abandonUnrecoverableWindowJob(err, 1) {
		t.Fatal("first miss should still retry")
	}
	if !abandonUnrecoverableWindowJob(err, maxUnrecoverableWindowAttempts) {
		t.Fatal("should abandon after max attempts")
	}
	if abandonUnrecoverableWindowJob(fmt.Errorf("下载窗 19 失败: timeout"), maxUnrecoverableWindowAttempts) {
		t.Fatal("transient download error must keep retrying")
	}
}

func TestMasterOmitsEarlierWindow(t *testing.T) {
	m := &model.LiveMaterial{
		Duration: 11401029,
		MediaWindows: model.MediaWindowList{
			{Index: 18, EndMS: 11401029, DurMS: 600000, Ready: true},
			{Index: 19, EndMS: 12001084, DurMS: 600055, Ready: true},
		}.Marshal(),
	}
	if !masterOmitsEarlierWindow(m, 20, m.Duration) {
		t.Fatal("window 19 is ahead of committed duration")
	}
	m.Duration = 12001084
	if masterOmitsEarlierWindow(m, 20, m.Duration) {
		t.Fatal("window 19 is already covered")
	}
}
