package liveingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuoteFFConcatPath_WindowsStyle(t *testing.T) {
	got := quoteFFConcatPath(`E:\workspace\GitHub\live-mixer\seg_00000.ts`)
	want := `'E:/workspace/GitHub/live-mixer/seg_00000.ts'`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// 不得出现 Go strconv.Quote 那种 \"E:\\\\...\" 形态。
	if strings.Contains(got, `\\`) || strings.HasPrefix(got, `"`) {
		t.Fatalf("must not use Go double-quote escaping: %q", got)
	}
}

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
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[1], "file '") || !strings.HasSuffix(lines[1], "'") {
		t.Fatalf("want single-quoted file line, got %q", lines)
	}
	if strings.Contains(lines[1], `\\`) {
		t.Fatalf("Windows path must use forward slashes, got %q", lines[1])
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
