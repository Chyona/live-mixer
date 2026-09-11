package liveingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFFConcatList(t *testing.T) {
	dir := t.TempDir()
	seg0 := filepath.Join(dir, "seg_00000.ts")
	seg1 := filepath.Join(dir, "seg_00001.ts")
	if err := os.WriteFile(seg0, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seg1, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "list.ffconcat")
	if err := WriteFFConcatList([]string{seg0, seg1}, out); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "ffconcat version 1.0\n") {
		t.Fatalf("header: %q", s)
	}
	if !strings.Contains(s, "seg_00000.ts") || !strings.Contains(s, "seg_00001.ts") {
		t.Fatalf("missing files: %s", s)
	}
}

func TestLiveIngestSegmentDir(t *testing.T) {
	got := LiveIngestSegmentDir(`/html`, 18, 1)
	want := filepath.Join(`/html`, "staging", "live_ingest", "18", "e1")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if LiveIngestSegmentDir("", 1, 1) != "" {
		t.Fatal("empty root")
	}
}
