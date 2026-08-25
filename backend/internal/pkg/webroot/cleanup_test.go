package webroot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStaging_KeepsNewestDirs(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}

	keepFile := filepath.Join(staging, "not-a-dir.txt")
	if err := os.WriteFile(keepFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-10 * time.Hour)
	dirs := make([]string, 5)
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("task-%d", i)
		path := filepath.Join(staging, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Hour) // task-4 最新
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
		dirs[i] = path
	}

	removed, err := CleanupStaging(root, 3)
	if err != nil {
		t.Fatalf("CleanupStaging error = %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	// 保留 task-2/3/4，删除 task-0/1
	for i, path := range dirs {
		_, statErr := os.Stat(path)
		if i < 2 {
			if !os.IsNotExist(statErr) {
				t.Fatalf("old dir %s should be removed: %v", path, statErr)
			}
			continue
		}
		if statErr != nil {
			t.Fatalf("new dir %s missing: %v", path, statErr)
		}
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Fatalf("file under staging should remain: %v", err)
	}
}

func TestCleanupStaging_NoOpWhenWithinLimit(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	for _, name := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(staging, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := CleanupStaging(root, 80)
	if err != nil {
		t.Fatalf("CleanupStaging error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestCleanupStaging_MissingRootIsOK(t *testing.T) {
	removed, err := CleanupStaging(filepath.Join(t.TempDir(), "missing"), 80)
	if err != nil {
		t.Fatalf("CleanupStaging error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestCleanupStaging_RejectsInvalidArgs(t *testing.T) {
	if _, err := CleanupStaging("", 80); err == nil {
		t.Fatal("empty rootDir should error")
	}
	if _, err := CleanupStaging(t.TempDir(), 0); err == nil {
		t.Fatal("non-positive keep should error")
	}
}

func TestCleanupStaging_SkipsASRSubDir(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	asrChild := filepath.Join(staging, ASRStagingSubDir, "1-v1")
	if err := os.MkdirAll(asrChild, 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-5 * time.Hour)
	for i := 0; i < 3; i++ {
		path := filepath.Join(staging, fmt.Sprintf("task-%d", i))
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := CleanupStaging(root, 2)
	if err != nil {
		t.Fatalf("CleanupStaging error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(asrChild); err != nil {
		t.Fatalf("asr child should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, ASRStagingSubDir)); err != nil {
		t.Fatalf("asr root should remain: %v", err)
	}
}

func TestCleanupASRStaging_KeepsNewestDirs(t *testing.T) {
	root := t.TempDir()
	asrRoot := filepath.Join(root, "staging", ASRStagingSubDir)
	draftDir := filepath.Join(root, "staging", "draft-task")
	if err := os.MkdirAll(draftDir, 0o755); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-10 * time.Hour)
	dirs := make([]string, 5)
	for i := 0; i < 5; i++ {
		path := filepath.Join(asrRoot, fmt.Sprintf("%d-v1", i))
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
		dirs[i] = path
	}

	removed, err := CleanupASRStaging(root, 2)
	if err != nil {
		t.Fatalf("CleanupASRStaging error = %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	for i, path := range dirs {
		_, statErr := os.Stat(path)
		if i < 3 {
			if !os.IsNotExist(statErr) {
				t.Fatalf("old asr dir %s should be removed", path)
			}
			continue
		}
		if statErr != nil {
			t.Fatalf("new asr dir %s missing: %v", path, statErr)
		}
	}
	if _, err := os.Stat(draftDir); err != nil {
		t.Fatalf("draft dir should be untouched: %v", err)
	}
}

func TestCleanupASRStaging_MissingRootIsOK(t *testing.T) {
	removed, err := CleanupASRStaging(filepath.Join(t.TempDir(), "missing"), 20)
	if err != nil {
		t.Fatalf("CleanupASRStaging error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestCleanupASRStaging_RejectsInvalidArgs(t *testing.T) {
	if _, err := CleanupASRStaging("", 20); err == nil {
		t.Fatal("empty rootDir should error")
	}
	if _, err := CleanupASRStaging(t.TempDir(), 0); err == nil {
		t.Fatal("non-positive keep should error")
	}
}
