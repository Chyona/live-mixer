package webroot

import (
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
