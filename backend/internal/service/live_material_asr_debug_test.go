package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/webroot"

	"go.uber.org/zap"
)

func TestASRDebugRecorder_EmptyDirNoop(t *testing.T) {
	rec := newASRDebugRecorder("", zap.NewNop())
	rec.Write("001_prepare.json", map[string]any{"x": 1})
	// 不应 panic；无目录可断言。
}

func TestASRDebugRecorder_WritesJSON(t *testing.T) {
	dir := t.TempDir()
	rec := newASRDebugRecorder(dir, zap.NewNop())
	rec.Write("001_prepare.json", map[string]any{
		"source_url": "https://example.com/a.mp4",
		"audio_url":  "https://cdn.example.com/a.mp3",
	})
	path := filepath.Join(dir, "001_prepare.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["source_url"] != "https://example.com/a.mp4" {
		t.Errorf("source_url = %v", payload["source_url"])
	}
}

func TestASRDebugRecorder_WriteFailureDoesNotPanic(t *testing.T) {
	// 将 dir 设为普通文件路径，MkdirAll / WriteFile 会失败，但不应 panic。
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newASRDebugRecorder(filePath, zap.NewNop())
	rec.Write("001_prepare.json", map[string]any{"ok": true})
}

func TestLiveMaterialASRWorker_Process_WritesStagingDebug(t *testing.T) {
	root := t.TempDir()
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			7: {
				ID:         7,
				LiveURL:    "https://example.com/live.mp4",
				ASRStatus:  model.ASRStatusProcessing,
				ASRVersion: 2,
			},
		},
	}
	asrSvc := &workerMockASR{
		transcribeFn: func(ctx context.Context, audioURL string, onProgress asr.ProgressCallback) (json.RawMessage, error) {
			return sampleLiveASRJSON(1200, "hello"), nil
		},
	}
	preparer := &mockASRAudioPreparer{
		prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
			return ASRAudioPrepareResult{
				AudioURL: "https://bucket.example.com/temp/asr.mp3",
				Width:    1280,
				Height:   720,
				Cleanup:  func() {},
			}, nil
		},
	}
	worker := NewLiveMaterialASRWorker(
		repo, asrSvc, preparer, defaultWorkerLLM(), nil, 0, 0,
		webroot.Config{RootDir: root},
	)
	if err := worker.Process(context.Background(), repo.materials[7]); err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	dir := filepath.Join(root, "staging", webroot.ASRStagingSubDir, "7-v2")
	for _, name := range []string{
		"001_prepare.json",
		"002_asr_raw.json",
		"003_llm_summaries.json",
		"004_llm_paragraphs.json",
		"005_postprocess_result.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing debug file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "999_fail.json")); !os.IsNotExist(err) {
		t.Errorf("999_fail.json should not exist on success, err=%v", err)
	}
}

func TestLiveMaterialASRWorker_Process_FailWrites999(t *testing.T) {
	root := t.TempDir()
	repo := &workerMockRepo{
		materials: map[uint]*model.LiveMaterial{
			8: {ID: 8, LiveURL: "https://example.com/x.mp4", ASRStatus: model.ASRStatusProcessing, ASRVersion: 1},
		},
	}
	worker := NewLiveMaterialASRWorker(
		repo,
		&workerMockASR{},
		&mockASRAudioPreparer{
			prepareFn: func(ctx context.Context, materialID uint, sourceURL string, onProgress func(progress int16)) (ASRAudioPrepareResult, error) {
				return ASRAudioPrepareResult{}, context.DeadlineExceeded
			},
		},
		nil, nil, 0, 0,
		webroot.Config{RootDir: root},
	)
	if err := worker.Process(context.Background(), repo.materials[8]); err == nil {
		t.Fatal("expected prepare failure")
	}
	path := filepath.Join(root, "staging", webroot.ASRStagingSubDir, "8-v1", "999_fail.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile 999_fail: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload["stage"] != "prepare" {
		t.Errorf("stage = %v, want prepare", payload["stage"])
	}
}
