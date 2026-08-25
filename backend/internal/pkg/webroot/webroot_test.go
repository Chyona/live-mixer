package webroot

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_StagingDirs(t *testing.T) {
	cfg := Config{RootDir: `D:\html`}
	staging := cfg.StagingDir("7")
	if !strings.Contains(staging, "staging") || !strings.Contains(staging, "7") {
		t.Errorf("StagingDir = %s", staging)
	}
	record := cfg.CapCutMateRecordDir("7")
	if !strings.Contains(record, "capcut_mate") {
		t.Errorf("CapCutMateRecordDir = %s", record)
	}
}

func TestConfig_ASRStagingDir(t *testing.T) {
	cfg := Config{RootDir: `D:\html`}
	got := cfg.ASRStagingDir(12, 3)
	want := filepath.Join(`D:\html`, "staging", ASRStagingSubDir, "12-v3")
	if got != want {
		t.Errorf("ASRStagingDir = %q, want %q", got, want)
	}
	empty := Config{}
	if empty.ASRStagingDir(1, 1) != "" {
		t.Error("empty RootDir should return empty ASRStagingDir")
	}
}
