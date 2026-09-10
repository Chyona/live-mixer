package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowTimelineMS_PrefersSegDurations(t *testing.T) {
	dir := t.TempDir()
	durs := map[int64]int64{0: 6055, 1: 6061, 2: 6055}
	saveSegDurations(dir, durs)
	files := []string{
		filepath.Join(dir, "seg_00000.ts"),
		filepath.Join(dir, "seg_00001.ts"),
		filepath.Join(dir, "seg_00002.ts"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := windowTimelineMS(dir, files, 18000, 6000)
	want := int64(6055 + 6061 + 6055)
	if got != want {
		t.Fatalf("windowTimelineMS = %d, want %d", got, want)
	}
}

func TestWindowTimelineMS_FallsBackToConcatProbe(t *testing.T) {
	dir := t.TempDir()
	files := []string{filepath.Join(dir, "seg_00000.ts")}
	got := windowTimelineMS(dir, files, 12345, 6000)
	if got != 12345 {
		t.Fatalf("windowTimelineMS = %d, want 12345", got)
	}
}
