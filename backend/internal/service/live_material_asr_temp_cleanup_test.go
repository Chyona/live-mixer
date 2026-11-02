package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupASRTempFiles_RemovesExpiredASRFilesOnly(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	fresh := time.Now()

	write := func(name string, mtime time.Time) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("写入 %s 失败: %v", name, err)
		}
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("设置 %s mtime 失败: %v", name, err)
		}
		return path
	}

	oldSrc := write("asr_old.src", old)
	oldMP3 := write("asr_old.mp3", old)
	oldOther := write("other.txt", old)
	freshSrc := write("asr_fresh.src", fresh)

	oldDir := filepath.Join(dir, "asr_old-dir")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldDir, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := CleanupASRTempFiles(dir, time.Hour)
	if err != nil {
		t.Fatalf("CleanupASRTempFiles error = %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	for _, path := range []string{oldSrc, oldMP3, oldOther} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("过期文件应被删除 %s: %v", path, statErr)
		}
	}
	for _, path := range []string{freshSrc, oldDir} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("文件/目录不应被删除 %s: %v", path, statErr)
		}
	}
}

func TestCleanupASRTempFiles_MissingDirIsOK(t *testing.T) {
	removed, err := CleanupASRTempFiles(filepath.Join(t.TempDir(), "missing"), time.Hour)
	if err != nil {
		t.Fatalf("CleanupASRTempFiles error = %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestCleanupASRTempFiles_InvalidArgs(t *testing.T) {
	if _, err := CleanupASRTempFiles("", time.Hour); err == nil {
		t.Fatal("空 dir 应返回错误")
	}
	if _, err := CleanupASRTempFiles(t.TempDir(), 0); err == nil {
		t.Fatal("非正 maxAge 应返回错误")
	}
}
