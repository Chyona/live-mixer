package liveingest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMediaTimelineIndex_ResolveRange(t *testing.T) {
	idx := NewMediaTimelineIndex(6000)
	idx.SetDuration(0, 6055)
	idx.SetDuration(1, 6061)
	idx.SetDuration(2, 6000)
	if idx.TotalMS != 6055+6061+6000 {
		t.Fatalf("total=%d", idx.TotalMS)
	}
	seg, off, ok := idx.Resolve(7000)
	if !ok || seg != 1 || off != 7000-6055 {
		t.Fatalf("Resolve(7000)=%d,%d ok=%v", seg, off, ok)
	}
	spans, err := idx.Range(5000, 9000)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans=%d want 2: %+v", len(spans), spans)
	}
	if spans[0].SegIndex != 0 || spans[0].OffsetMS != 5000 || spans[0].DurMS != 1055 {
		t.Fatalf("span0=%+v", spans[0])
	}
	if spans[1].SegIndex != 1 || spans[1].OffsetMS != 0 || spans[1].DurMS != 2945 {
		t.Fatalf("span1=%+v", spans[1])
	}
}

func TestMediaTimelineIndex_WindowMS(t *testing.T) {
	idx := NewMediaTimelineIndex(6000)
	idx.MergeDurations(map[int64]int64{0: 1000, 1: 2000, 2: 3000})
	if got := idx.WindowMS(0, 3); got != 6000 {
		t.Fatalf("WindowMS=%d", got)
	}
	if got := idx.CumStartMS(2); got != 3000 {
		t.Fatalf("CumStartMS(2)=%d", got)
	}
}

func TestMediaTimelineIndex_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx := NewMediaTimelineIndex(6000)
	idx.SetDuration(0, 6055)
	idx.SetDuration(1, 6061)
	if err := SaveMediaTimelineIndex(dir, idx); err != nil {
		t.Fatal(err)
	}
	loaded := LoadMediaTimelineIndex(dir, 6000)
	if loaded.TotalMS != idx.TotalMS || len(loaded.Segs) != 2 {
		t.Fatalf("loaded=%+v", loaded)
	}
	if _, err := os.Stat(filepath.Join(dir, SegDurationsFileName)); err != nil {
		t.Fatalf("legacy sidecar missing: %v", err)
	}
}

func TestLoadMediaTimelineIndex_FromLegacySegDurations(t *testing.T) {
	dir := t.TempDir()
	if err := saveSegDurationsCompat(dir, map[int64]int64{0: 5000, 1: 5000}); err != nil {
		t.Fatal(err)
	}
	idx := LoadMediaTimelineIndex(dir, 6000)
	if idx.TotalMS != 10000 {
		t.Fatalf("total=%d want 10000", idx.TotalMS)
	}
}

func TestUpsertSegDuration(t *testing.T) {
	dir := t.TempDir()
	UpsertSegDuration(dir, 0, 1000, 6000)
	UpsertSegDuration(dir, 1, 2000, 6000)
	idx := LoadMediaTimelineIndex(dir, 6000)
	if idx.TotalMS != 3000 {
		t.Fatalf("total=%d", idx.TotalMS)
	}
}

func TestAttachFilePaths(t *testing.T) {
	spans := AttachFilePaths("/tmp/e1", []SegSpan{{SegIndex: 3, OffsetMS: 0, DurMS: 100}})
	if spans[0].FilePath != filepath.Join("/tmp/e1", "seg_00003.ts") {
		t.Fatalf("path=%s", spans[0].FilePath)
	}
}
