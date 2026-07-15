package webroot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_LocalPathToURL(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "staging", "42", "clip_000.mp4")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{RootDir: root, RootURL: "http://192.168.3.219:81"}
	got, err := cfg.LocalPathToURL(local)
	if err != nil {
		t.Fatalf("LocalPathToURL() error = %v", err)
	}
	wantSuffix := "/staging/42/clip_000.mp4"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("url = %s, want suffix %s", got, wantSuffix)
	}
	if !strings.HasPrefix(got, "http://192.168.3.219:81/") {
		t.Errorf("url = %s, want prefix", got)
	}
}

func TestConfig_LocalPathToURL_OutsideRoot(t *testing.T) {
	cfg := Config{RootDir: t.TempDir(), RootURL: "http://example.com"}
	_, err := cfg.LocalPathToURL(filepath.Join(t.TempDir(), "a.mp4"))
	if err == nil {
		t.Fatal("expected error for path outside root")
	}
}

func TestConfig_StagingDirs(t *testing.T) {
	cfg := Config{RootDir: `D:\html`}
	staging := cfg.StagingDir(7)
	if !strings.Contains(staging, "staging") || !strings.Contains(staging, "7") {
		t.Errorf("StagingDir = %s", staging)
	}
	record := cfg.CapCutMateRecordDir(7)
	if !strings.Contains(record, "capcut_mate") {
		t.Errorf("CapCutMateRecordDir = %s", record)
	}
}
