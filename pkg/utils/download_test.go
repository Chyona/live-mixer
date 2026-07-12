package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadFile_ToDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network download in short mode")
	}

	saveDir := t.TempDir()
	file, err := DownloadFile("https://gogoshine.com/min.mp4", saveDir)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	defer os.Remove(file)

	if !strings.HasPrefix(file, saveDir) {
		t.Errorf("saved path = %q, want under %q", file, saveDir)
	}
	if filepath.Ext(file) != ".mp4" {
		t.Errorf("ext = %q, want .mp4", filepath.Ext(file))
	}

	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("downloaded file size is 0")
	}
}

func TestDownloadFile_ToFilePath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network download in short mode")
	}

	savePath := filepath.Join(t.TempDir(), "output.mp4")
	file, err := DownloadFile("https://gogoshine.com/min.mp4", savePath)
	if err != nil {
		t.Fatalf("DownloadFile() error = %v", err)
	}
	defer os.Remove(file)

	if file != savePath {
		t.Errorf("saved path = %q, want %q", file, savePath)
	}
}

func TestIsSaveDir(t *testing.T) {
	dir := t.TempDir()
	if !isSaveDir(dir) {
		t.Errorf("isSaveDir(%q) = false, want true", dir)
	}
	if isSaveDir(filepath.Join(dir, "file.mp4")) {
		t.Error("file path should not be treated as directory")
	}
	if !isSaveDir(dir + string(os.PathSeparator)) {
		t.Errorf("trailing separator path should be treated as directory")
	}
}
