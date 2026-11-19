package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"live-mixer/internal/pkg/media"
)

type mapDurProber struct {
	ms map[string]int64
}

func (m mapDurProber) ProbeMediaTimeline(ctx context.Context, inputPath string) (media.MediaTimeline, error) {
	if d, ok := m.ms[inputPath]; ok {
		return media.MediaTimeline{FormatDurationSec: float64(d) / 1000.0}, nil
	}
	return media.MediaTimeline{}, fmt.Errorf("missing")
}

func (m mapDurProber) ProbeVideoSize(ctx context.Context, inputPath string) (int, int, error) {
	return 0, 0, nil
}

func TestResolveWindowStartFast_CursorZero(t *testing.T) {
	dir := t.TempDir()
	start, off, err := resolveWindowStartFast(context.Background(), mapDurProber{}, dir, 0, 1)
	if err != nil || start != 0 || off != 0 {
		t.Fatalf("got start=%d off=%d err=%v", start, off, err)
	}
}

func TestResolveWindowStartFast_StopsAtCursor(t *testing.T) {
	dir := t.TempDir()
	durs := map[string]int64{}
	for i := 0; i < 20; i++ {
		p := filepath.Join(dir, fmt.Sprintf("seg_%05d.ts", i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		durs[p] = 5900
	}
	prober := mapDurProber{ms: durs}
	// 10 片后游标 = 59000
	start, off, err := resolveWindowStartFast(context.Background(), prober, dir, 59000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if start != 9 { // hit at seg 10 → overlap 1 → 9
		t.Fatalf("start=%d want 9", start)
	}
	if off != 9*5900 {
		t.Fatalf("offset=%d want %d", off, 9*5900)
	}
}

func TestLatestLocalSegIndex(t *testing.T) {
	dir := t.TempDir()
	if got := latestLocalSegIndex(dir); got != -1 {
		t.Fatalf("empty dir got %d want -1", got)
	}
	for _, i := range []int{0, 3, 12} {
		p := filepath.Join(dir, fmt.Sprintf("seg_%05d.ts", i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := latestLocalSegIndex(dir); got != 12 {
		t.Fatalf("got %d want 12", got)
	}
}
