package webroot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStaging_RemovesOnlyExpiredDirs(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}

	oldDir := filepath.Join(staging, "old-task")
	newDir := filepath.Join(staging, "new-task")
	keepFile := filepath.Join(staging, "not-a-dir.txt")
	for _, dir := range []string{oldDir, newDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(keepFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	removed, err := CleanupStaging(root, 24*time.Hour, now)
	if err != nil {
		t.Fatalf("CleanupStaging error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old dir still exists: %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("new dir missing: %v", err)
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("file under staging should remain: %v", err)
	}
}

func TestCleanupStaging_MissingRootIsOK(t *testing.T) {
	removed, err := CleanupStaging(filepath.Join(t.TempDir(), "missing"), 24*time.Hour, time.Now())
	if err != nil {
		t.Fatalf("CleanupStaging error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestCleanupStaging_RejectsInvalidArgs(t *testing.T) {
	if _, err := CleanupStaging("", time.Hour, time.Now()); err == nil {
		t.Fatal("empty rootDir should error")
	}
	if _, err := CleanupStaging(t.TempDir(), 0, time.Now()); err == nil {
		t.Fatal("non-positive maxAge should error")
	}
}
