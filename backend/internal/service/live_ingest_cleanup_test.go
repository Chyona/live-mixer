package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/webroot"
)

type mapLiveIngestLookup map[uint]*model.LiveMaterial

func (m mapLiveIngestLookup) ListByIDs(_ context.Context, ids []uint) (map[uint]*model.LiveMaterial, error) {
	out := make(map[uint]*model.LiveMaterial, len(ids))
	for _, id := range ids {
		if mat, ok := m[id]; ok {
			out[id] = mat
		}
	}
	return out, nil
}

func mkdirLiveIngestID(t *testing.T, root string, id uint, mt time.Time, epochs ...int64) string {
	t.Helper()
	dir := filepath.Join(root, "staging", webroot.LiveIngestSubDir, fmt.Sprintf("%d", id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, epoch := range epochs {
		ep := filepath.Join(dir, fmt.Sprintf("e%d", epoch))
		if err := os.MkdirAll(ep, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, mt, mt); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCleanupLiveIngest_EvictsOldestEnded(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-10 * time.Hour)
	lookup := mapLiveIngestLookup{
		1: {ID: 1, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusCompleted, IngestEpoch: 1},
		2: {ID: 2, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusCompleted, IngestEpoch: 1},
		3: {ID: 3, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusCompleted, IngestEpoch: 1},
	}
	mkdirLiveIngestID(t, root, 1, base, 1)
	mkdirLiveIngestID(t, root, 2, base.Add(time.Hour), 1)
	mkdirLiveIngestID(t, root, 3, base.Add(2*time.Hour), 1)

	result, err := CleanupLiveIngest(context.Background(), root, 2, lookup, nil)
	if err != nil {
		t.Fatalf("CleanupLiveIngest: %v", err)
	}
	if result.RemovedIDs != 1 {
		t.Fatalf("RemovedIDs=%d, want 1", result.RemovedIDs)
	}
	if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, "1")); !os.IsNotExist(err) {
		t.Fatalf("oldest ended id=1 should be removed: %v", err)
	}
	for _, id := range []string{"2", "3"} {
		if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, id)); err != nil {
			t.Fatalf("id=%s should remain: %v", id, err)
		}
	}
}

func TestCleanupLiveIngest_ProtectedExceedsQuota(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-5 * time.Hour)
	lookup := mapLiveIngestLookup{
		1: {ID: 1, LiveStatus: model.LiveStatusLive, ASRStatus: model.ASRStatusPending, IngestEpoch: 3},
		2: {ID: 2, LiveStatus: model.LiveStatusLive, ASRStatus: model.ASRStatusPending, IngestEpoch: 2},
		3: {ID: 3, LiveStatus: model.LiveStatusLive, ASRStatus: model.ASRStatusPending, IngestEpoch: 1},
		4: {ID: 4, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusCompleted, IngestEpoch: 1},
	}
	mkdirLiveIngestID(t, root, 1, base, 1, 2, 3)
	mkdirLiveIngestID(t, root, 2, base.Add(time.Hour), 1, 2)
	mkdirLiveIngestID(t, root, 3, base.Add(2*time.Hour), 1)
	mkdirLiveIngestID(t, root, 4, base.Add(-time.Hour), 1) // oldest ended

	result, err := CleanupLiveIngest(context.Background(), root, 2, lookup, nil)
	if err != nil {
		t.Fatalf("CleanupLiveIngest: %v", err)
	}
	if result.Protected != 3 {
		t.Fatalf("Protected=%d, want 3", result.Protected)
	}
	if result.RemovedIDs != 1 {
		t.Fatalf("RemovedIDs=%d, want 1 (only ended)", result.RemovedIDs)
	}
	if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, "4")); !os.IsNotExist(err) {
		t.Fatalf("ended id=4 should be evicted: %v", err)
	}
	for _, id := range []string{"1", "2", "3"} {
		if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, id)); err != nil {
			t.Fatalf("live id=%s must remain: %v", id, err)
		}
	}
}

func TestCleanupLiveIngest_KeepsUnuploadedWindowEpoch(t *testing.T) {
	root := t.TempDir()
	idDir := mkdirLiveIngestID(t, root, 7, time.Now(), 1, 2, 3, 4)
	// e1 有未上传窗；e2 空旧代数；e3/e4 为保留档（current 与 previous）
	winPath := filepath.Join(idDir, "e1", liveingest.WindowsDirName(), liveingest.WindowMP4FileName(0))
	if err := os.MkdirAll(filepath.Dir(winPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(winPath, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	windows := model.MediaWindowList{{
		Index: 0, Ready: true, URL: "", ObjectKey: "k",
	}}
	lookup := mapLiveIngestLookup{
		7: {
			ID:           7,
			LiveStatus:   model.LiveStatusLive,
			ASRStatus:    model.ASRStatusPending,
			IngestEpoch:  4,
			MediaWindows: windows.Marshal(),
		},
	}

	result, err := CleanupLiveIngest(context.Background(), root, 20, lookup, nil)
	if err != nil {
		t.Fatalf("CleanupLiveIngest: %v", err)
	}
	if result.RemovedEpoch != 1 {
		t.Fatalf("RemovedEpoch=%d, want 1 (only empty e2)", result.RemovedEpoch)
	}
	if _, err := os.Stat(filepath.Join(idDir, "e1")); err != nil {
		t.Fatalf("e1 with unuploaded window must remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(idDir, "e2")); !os.IsNotExist(err) {
		t.Fatalf("empty old e2 should be removed: %v", err)
	}
	for _, name := range []string{"e3", "e4"} {
		if _, err := os.Stat(filepath.Join(idDir, name)); err != nil {
			t.Fatalf("%s must remain: %v", name, err)
		}
	}
}

func TestCleanupLiveIngest_OrphanIDEvicted(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-3 * time.Hour)
	mkdirLiveIngestID(t, root, 9, base, 1)
	mkdirLiveIngestID(t, root, 10, base.Add(time.Hour), 1)
	lookup := mapLiveIngestLookup{
		10: {ID: 10, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusCompleted, IngestEpoch: 1},
		// 9 missing from DB → orphan, eligible
	}

	result, err := CleanupLiveIngest(context.Background(), root, 1, lookup, nil)
	if err != nil {
		t.Fatalf("CleanupLiveIngest: %v", err)
	}
	if result.RemovedIDs != 1 {
		t.Fatalf("RemovedIDs=%d, want 1", result.RemovedIDs)
	}
	if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, "9")); !os.IsNotExist(err) {
		t.Fatalf("orphan id=9 should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, "10")); err != nil {
		t.Fatalf("id=10 should remain: %v", err)
	}
}

func TestCleanupLiveIngest_ASRProcessingProtected(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-4 * time.Hour)
	lookup := mapLiveIngestLookup{
		1: {ID: 1, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusProcessing, IngestEpoch: 1},
		2: {ID: 2, LiveStatus: model.LiveStatusEnded, ASRStatus: model.ASRStatusCompleted, IngestEpoch: 1},
	}
	mkdirLiveIngestID(t, root, 1, base, 1)               // older but protected via ASR
	mkdirLiveIngestID(t, root, 2, base.Add(time.Hour), 1) // newer ended

	result, err := CleanupLiveIngest(context.Background(), root, 1, lookup, nil)
	if err != nil {
		t.Fatalf("CleanupLiveIngest: %v", err)
	}
	if result.Protected != 1 {
		t.Fatalf("Protected=%d, want 1", result.Protected)
	}
	if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, "1")); err != nil {
		t.Fatalf("ASR processing id=1 must remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "staging", webroot.LiveIngestSubDir, "2")); !os.IsNotExist(err) {
		t.Fatalf("ended id=2 should be evicted when keep=1")
	}
}

func TestParseLiveIngestMaterialID(t *testing.T) {
	if id, ok := parseLiveIngestMaterialID("12"); !ok || id != 12 {
		t.Fatalf("got %d %v", id, ok)
	}
	if _, ok := parseLiveIngestMaterialID("e1"); ok {
		t.Fatal("epoch name should not parse as material id")
	}
	if _, ok := parseLiveIngestMaterialID("0"); ok {
		t.Fatal("0 should be invalid")
	}
}
