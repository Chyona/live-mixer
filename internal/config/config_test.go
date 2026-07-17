package config

import (
	"os"
	"testing"
)

func TestLoad_StorageFromEmbeddedConfig(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// 内嵌默认配置中 COS/OSS 为空占位，TOS 已配置项目桶
	if cfg.Storage.COS.Region != "" {
		t.Errorf("COS.Region = %q, want empty default", cfg.Storage.COS.Region)
	}
	if cfg.Storage.OSS.Endpoint != "" {
		t.Errorf("OSS.Endpoint = %q, want empty default", cfg.Storage.OSS.Endpoint)
	}
	if cfg.Storage.TOS.BucketName != "arkclaw-wxbpd" {
		t.Errorf("TOS.BucketName = %q, want arkclaw-wxbpd", cfg.Storage.TOS.BucketName)
	}
	if cfg.Storage.TOS.Region != "cn-shanghai" {
		t.Errorf("TOS.Region = %q, want cn-shanghai", cfg.Storage.TOS.Region)
	}
	if cfg.Storage.TOS.Endpoint != "tos-cn-shanghai.volces.com" {
		t.Errorf("TOS.Endpoint = %q, want tos-cn-shanghai.volces.com", cfg.Storage.TOS.Endpoint)
	}
	if cfg.Storage.BasePath != "video_editing" {
		t.Errorf("Storage.BasePath = %q, want video_editing", cfg.Storage.BasePath)
	}

	// ASR 默认 API Key 与轮询配置
	if cfg.ASR.APIKey != "606b0b96-0706-4fa5-ba0d-d9d1e879a4f7" {
		t.Errorf("ASR.APIKey = %q, want default api key", cfg.ASR.APIKey)
	}
	if cfg.ASR.ResourceID != "volc.seedasr.auc" {
		t.Errorf("ASR.ResourceID = %q, want volc.seedasr.auc", cfg.ASR.ResourceID)
	}
	if cfg.ASR.PollIntervalSec != 10 {
		t.Errorf("ASR.PollIntervalSec = %d, want 10", cfg.ASR.PollIntervalSec)
	}

	if cfg.Worker.AISliceConcurrency != 6 {
		t.Errorf("Worker.AISliceConcurrency = %d, want 6", cfg.Worker.AISliceConcurrency)
	}
	if cfg.Worker.ASRConcurrency != 6 {
		t.Errorf("Worker.ASRConcurrency = %d, want 6", cfg.Worker.ASRConcurrency)
	}
	if cfg.Worker.DraftConcurrency != 3 {
		t.Errorf("Worker.DraftConcurrency = %d, want 3", cfg.Worker.DraftConcurrency)
	}
	if cfg.Worker.AISliceDraftConcurrency != 3 {
		t.Errorf("Worker.AISliceDraftConcurrency = %d, want 3", cfg.Worker.AISliceDraftConcurrency)
	}
}

func TestLoad_StorageEnvOverride(t *testing.T) {
	t.Setenv("APP_STORAGE_OSS_ACCESS_KEY_ID", "oss-id")
	t.Setenv("APP_STORAGE_OSS_ACCESS_KEY_SECRET", "oss-secret")
	t.Setenv("APP_STORAGE_OSS_BUCKET_NAME", "my-bucket")
	t.Setenv("APP_STORAGE_OSS_ENDPOINT", "oss-cn-shanghai.aliyuncs.com")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Storage.OSS.AccessKeyID != "oss-id" {
		t.Errorf("OSS.AccessKeyID = %q, want oss-id", cfg.Storage.OSS.AccessKeyID)
	}
	if cfg.Storage.OSS.Endpoint != "oss-cn-shanghai.aliyuncs.com" {
		t.Errorf("OSS.Endpoint = %q, want oss-cn-shanghai.aliyuncs.com", cfg.Storage.OSS.Endpoint)
	}
}

func TestLoad_StorageCOSEnvOverride(t *testing.T) {
	for _, key := range []string{
		"APP_STORAGE_COS_SECRET_ID",
		"APP_STORAGE_COS_SECRET_KEY",
		"APP_STORAGE_COS_BUCKET_NAME",
		"APP_STORAGE_COS_REGION",
	} {
		os.Unsetenv(key)
	}

	t.Setenv("APP_STORAGE_COS_SECRET_ID", "cos-id")
	t.Setenv("APP_STORAGE_COS_SECRET_KEY", "cos-key")
	t.Setenv("APP_STORAGE_COS_BUCKET_NAME", "cos-bucket")
	t.Setenv("APP_STORAGE_COS_REGION", "ap-beijing")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Storage.COS.SecretID != "cos-id" {
		t.Errorf("COS.SecretID = %q, want cos-id", cfg.Storage.COS.SecretID)
	}
	if cfg.Storage.COS.Region != "ap-beijing" {
		t.Errorf("COS.Region = %q, want ap-beijing", cfg.Storage.COS.Region)
	}
}

func TestLoad_StorageTOSEnvOverride(t *testing.T) {
	for _, key := range []string{
		"APP_STORAGE_TOS_ACCESS_KEY_ID",
		"APP_STORAGE_TOS_ACCESS_KEY_SECRET",
		"APP_STORAGE_TOS_BUCKET_NAME",
		"APP_STORAGE_TOS_REGION",
		"APP_STORAGE_TOS_ENDPOINT",
	} {
		os.Unsetenv(key)
	}

	t.Setenv("APP_STORAGE_TOS_ACCESS_KEY_ID", "tos-id")
	t.Setenv("APP_STORAGE_TOS_ACCESS_KEY_SECRET", "tos-secret")
	t.Setenv("APP_STORAGE_TOS_BUCKET_NAME", "tos-bucket")
	t.Setenv("APP_STORAGE_TOS_REGION", "cn-shanghai")
	t.Setenv("APP_STORAGE_TOS_ENDPOINT", "tos-cn-shanghai.volces.com")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Storage.TOS.AccessKeyID != "tos-id" {
		t.Errorf("TOS.AccessKeyID = %q, want tos-id", cfg.Storage.TOS.AccessKeyID)
	}
	if cfg.Storage.TOS.Region != "cn-shanghai" {
		t.Errorf("TOS.Region = %q, want cn-shanghai", cfg.Storage.TOS.Region)
	}
	if cfg.Storage.TOS.Endpoint != "tos-cn-shanghai.volces.com" {
		t.Errorf("TOS.Endpoint = %q, want tos-cn-shanghai.volces.com", cfg.Storage.TOS.Endpoint)
	}
}

func TestLoad_CapCutMateAndWebDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CapCutMate.BaseURL != "http://192.168.3.219:81" {
		t.Errorf("CapCutMate.BaseURL = %q", cfg.CapCutMate.BaseURL)
	}
	if cfg.Web.RootDir != "docker/html" {
		t.Errorf("Web.RootDir = %q", cfg.Web.RootDir)
	}
	if cfg.Web.RootURL != "http://192.168.3.219:81" {
		t.Errorf("Web.RootURL = %q", cfg.Web.RootURL)
	}
}

func TestLoad_CapCutMateEnvOverride(t *testing.T) {
	t.Setenv("CAPCUT_MATE_URL", "http://10.0.0.1:81")
	t.Setenv("WEB_ROOT_DIR", `D:\html`)
	t.Setenv("WEB_ROOT_URL", "http://10.0.0.1:81")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CapCutMate.BaseURL != "http://10.0.0.1:81" {
		t.Errorf("CapCutMate.BaseURL = %q", cfg.CapCutMate.BaseURL)
	}
	if cfg.Web.RootDir != `D:\html` {
		t.Errorf("Web.RootDir = %q", cfg.Web.RootDir)
	}
	if cfg.Web.RootURL != "http://10.0.0.1:81" {
		t.Errorf("Web.RootURL = %q", cfg.Web.RootURL)
	}
}

func TestLoad_WorkerAISliceConcurrencyEnvOverride(t *testing.T) {
	t.Setenv("APP_WORKER_AI_SLICE_CONCURRENCY", "3")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.AISliceConcurrency != 3 {
		t.Errorf("Worker.AISliceConcurrency = %d, want 3", cfg.Worker.AISliceConcurrency)
	}
}

func TestLoad_WorkerASRConcurrencyEnvOverride(t *testing.T) {
	t.Setenv("APP_WORKER_ASR_CONCURRENCY", "4")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.ASRConcurrency != 4 {
		t.Errorf("Worker.ASRConcurrency = %d, want 4", cfg.Worker.ASRConcurrency)
	}
}

func TestLoad_WorkerAISliceConcurrencyInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("APP_WORKER_AI_SLICE_CONCURRENCY", "0")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.AISliceConcurrency != DefaultAISliceConcurrency {
		t.Errorf("Worker.AISliceConcurrency = %d, want %d", cfg.Worker.AISliceConcurrency, DefaultAISliceConcurrency)
	}
}

func TestLoad_WorkerASRConcurrencyInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("APP_WORKER_ASR_CONCURRENCY", "0")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.ASRConcurrency != DefaultASRConcurrency {
		t.Errorf("Worker.ASRConcurrency = %d, want %d", cfg.Worker.ASRConcurrency, DefaultASRConcurrency)
	}
}

func TestWorkerConfig_AISliceConcurrencyOrDefault(t *testing.T) {
	if got := (WorkerConfig{AISliceConcurrency: 8}).AISliceConcurrencyOrDefault(); got != 8 {
		t.Errorf("got %d, want 8", got)
	}
	if got := (WorkerConfig{}).AISliceConcurrencyOrDefault(); got != DefaultAISliceConcurrency {
		t.Errorf("got %d, want %d", got, DefaultAISliceConcurrency)
	}
	if got := (WorkerConfig{AISliceConcurrency: -1}).AISliceConcurrencyOrDefault(); got != DefaultAISliceConcurrency {
		t.Errorf("got %d, want %d", got, DefaultAISliceConcurrency)
	}
}

func TestWorkerConfig_ASRConcurrencyOrDefault(t *testing.T) {
	if got := (WorkerConfig{ASRConcurrency: 5}).ASRConcurrencyOrDefault(); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	if got := (WorkerConfig{}).ASRConcurrencyOrDefault(); got != DefaultASRConcurrency {
		t.Errorf("got %d, want %d", got, DefaultASRConcurrency)
	}
	if got := (WorkerConfig{ASRConcurrency: -2}).ASRConcurrencyOrDefault(); got != DefaultASRConcurrency {
		t.Errorf("got %d, want %d", got, DefaultASRConcurrency)
	}
}

func TestLoad_WorkerDraftConcurrencyEnvOverride(t *testing.T) {
	t.Setenv("APP_WORKER_DRAFT_CONCURRENCY", "5")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.DraftConcurrency != 5 {
		t.Errorf("Worker.DraftConcurrency = %d, want 5", cfg.Worker.DraftConcurrency)
	}
}

func TestLoad_WorkerDraftConcurrencyInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("APP_WORKER_DRAFT_CONCURRENCY", "0")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.DraftConcurrency != DefaultDraftConcurrency {
		t.Errorf("Worker.DraftConcurrency = %d, want %d", cfg.Worker.DraftConcurrency, DefaultDraftConcurrency)
	}
}

func TestWorkerConfig_DraftConcurrencyOrDefault(t *testing.T) {
	if got := (WorkerConfig{DraftConcurrency: 4}).DraftConcurrencyOrDefault(); got != 4 {
		t.Errorf("got %d, want 4", got)
	}
	if got := (WorkerConfig{}).DraftConcurrencyOrDefault(); got != DefaultDraftConcurrency {
		t.Errorf("got %d, want %d", got, DefaultDraftConcurrency)
	}
	if got := (WorkerConfig{DraftConcurrency: -1}).DraftConcurrencyOrDefault(); got != DefaultDraftConcurrency {
		t.Errorf("got %d, want %d", got, DefaultDraftConcurrency)
	}
}

func TestLoad_WorkerAISliceDraftConcurrencyEnvOverride(t *testing.T) {
	t.Setenv("APP_WORKER_AI_SLICE_DRAFT_CONCURRENCY", "2")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.AISliceDraftConcurrency != 2 {
		t.Errorf("Worker.AISliceDraftConcurrency = %d, want 2", cfg.Worker.AISliceDraftConcurrency)
	}
}

func TestLoad_WorkerAISliceDraftConcurrencyInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("APP_WORKER_AI_SLICE_DRAFT_CONCURRENCY", "0")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.AISliceDraftConcurrency != DefaultAISliceDraftConcurrency {
		t.Errorf("Worker.AISliceDraftConcurrency = %d, want %d", cfg.Worker.AISliceDraftConcurrency, DefaultAISliceDraftConcurrency)
	}
}

func TestWorkerConfig_AISliceDraftConcurrencyOrDefault(t *testing.T) {
	if got := (WorkerConfig{AISliceDraftConcurrency: 5}).AISliceDraftConcurrencyOrDefault(); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	if got := (WorkerConfig{}).AISliceDraftConcurrencyOrDefault(); got != DefaultAISliceDraftConcurrency {
		t.Errorf("got %d, want %d", got, DefaultAISliceDraftConcurrency)
	}
	if got := (WorkerConfig{AISliceDraftConcurrency: -1}).AISliceDraftConcurrencyOrDefault(); got != DefaultAISliceDraftConcurrency {
		t.Errorf("got %d, want %d", got, DefaultAISliceDraftConcurrency)
	}
}
