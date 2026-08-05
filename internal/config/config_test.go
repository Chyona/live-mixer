package config

import (
	"os"
	"testing"
	"time"
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
	if !cfg.CapCutMate.GenVideoEnabled() {
		t.Errorf("CapCutMate.GenVideoEnabled() = false, want true")
	}
	if cfg.Web.RootDir != "docker/html" {
		t.Errorf("Web.RootDir = %q", cfg.Web.RootDir)
	}
	if cfg.Web.StagingMaxDirs != DefaultStagingMaxDirs {
		t.Errorf("Web.StagingMaxDirs = %d, want %d", cfg.Web.StagingMaxDirs, DefaultStagingMaxDirs)
	}
	if cfg.Web.ASRStagingMaxDirs != DefaultASRStagingMaxDirs {
		t.Errorf("Web.ASRStagingMaxDirs = %d, want %d", cfg.Web.ASRStagingMaxDirs, DefaultASRStagingMaxDirs)
	}
	if cfg.Web.StagingCleanupIntervalMin != DefaultStagingCleanupIntervalMin {
		t.Errorf("Web.StagingCleanupIntervalMin = %d, want %d", cfg.Web.StagingCleanupIntervalMin, DefaultStagingCleanupIntervalMin)
	}
	if got := cfg.Web.StagingCleanupInterval(); got != time.Hour {
		t.Errorf("StagingCleanupInterval() = %v, want 1h", got)
	}
}

func TestLoad_CapCutMateEnvOverride(t *testing.T) {
	t.Setenv("CAPCUT_MATE_URL", "http://10.0.0.1:81")
	t.Setenv("CAPCUT_MATE_API_KEY", "capcut-key-from-env")
	t.Setenv("APP_CAPCUT_MATE_GEN_VIDEO_BASE_URL", "https://capcut.example")
	t.Setenv("APP_CAPCUT_MATE_ENABLE_GEN_VIDEO", "false")
	t.Setenv("WEB_ROOT_DIR", `D:\html`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CapCutMate.BaseURL != "http://10.0.0.1:81" {
		t.Errorf("CapCutMate.BaseURL = %q", cfg.CapCutMate.BaseURL)
	}
	if cfg.CapCutMate.APIKey != "capcut-key-from-env" {
		t.Errorf("CapCutMate.APIKey = %q", cfg.CapCutMate.APIKey)
	}
	if cfg.CapCutMate.GenVideoBaseURL != "https://capcut.example" {
		t.Errorf("CapCutMate.GenVideoBaseURL = %q", cfg.CapCutMate.GenVideoBaseURL)
	}
	if cfg.CapCutMate.GenVideoEnabled() {
		t.Errorf("CapCutMate.GenVideoEnabled() = true, want false")
	}
	if cfg.Web.RootDir != `D:\html` {
		t.Errorf("Web.RootDir = %q", cfg.Web.RootDir)
	}
}

func TestCapCutMateConfig_GenVideoEnabledDefault(t *testing.T) {
	if !(CapCutMateConfig{}).GenVideoEnabled() {
		t.Error("zero CapCutMateConfig should enable gen_video by default")
	}
	off := false
	if (CapCutMateConfig{EnableGenVideo: &off}).GenVideoEnabled() {
		t.Error("EnableGenVideo=false should disable")
	}
}

func TestCapCutMateClientConfig_IncludesAPIKey(t *testing.T) {
	cfg := CapCutMateConfig{
		BaseURL:                 "http://capcut",
		APIKey:                  "k1",
		GenVideoBaseURL:         "https://v.example",
		GenVideoPollIntervalSec: 3,
		GenVideoMaxPolls:        10,
	}.CapCutMateClientConfig()
	if cfg.BaseURL != "http://capcut" || cfg.APIKey != "k1" {
		t.Errorf("cfg = %#v", cfg)
	}
	if cfg.GenVideoBaseURL != "https://v.example" {
		t.Errorf("GenVideoBaseURL = %q", cfg.GenVideoBaseURL)
	}
	if cfg.PollInterval != 3*time.Second || cfg.MaxPolls != 10 {
		t.Errorf("poll = %v max=%d", cfg.PollInterval, cfg.MaxPolls)
	}
}

func TestLoad_WebStagingCleanupEnvOverride(t *testing.T) {
	t.Setenv("APP_WEB_STAGING_MAX_DIRS", "120")
	t.Setenv("APP_WEB_ASR_STAGING_MAX_DIRS", "8")
	t.Setenv("APP_WEB_STAGING_CLEANUP_INTERVAL_MIN", "30")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Web.StagingMaxDirs != 120 {
		t.Errorf("StagingMaxDirs = %d, want 120", cfg.Web.StagingMaxDirs)
	}
	if cfg.Web.ASRStagingMaxDirs != 8 {
		t.Errorf("ASRStagingMaxDirs = %d, want 8", cfg.Web.ASRStagingMaxDirs)
	}
	if cfg.Web.StagingCleanupIntervalMin != 30 {
		t.Errorf("StagingCleanupIntervalMin = %d, want 30", cfg.Web.StagingCleanupIntervalMin)
	}
}

func TestLoad_WebStagingCleanupInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("APP_WEB_STAGING_MAX_DIRS", "0")
	t.Setenv("APP_WEB_ASR_STAGING_MAX_DIRS", "0")
	t.Setenv("APP_WEB_STAGING_CLEANUP_INTERVAL_MIN", "-1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Web.StagingMaxDirs != DefaultStagingMaxDirs {
		t.Errorf("StagingMaxDirs = %d, want %d", cfg.Web.StagingMaxDirs, DefaultStagingMaxDirs)
	}
	if cfg.Web.ASRStagingMaxDirs != DefaultASRStagingMaxDirs {
		t.Errorf("ASRStagingMaxDirs = %d, want %d", cfg.Web.ASRStagingMaxDirs, DefaultASRStagingMaxDirs)
	}
	if cfg.Web.StagingCleanupIntervalMin != DefaultStagingCleanupIntervalMin {
		t.Errorf("StagingCleanupIntervalMin = %d, want %d", cfg.Web.StagingCleanupIntervalMin, DefaultStagingCleanupIntervalMin)
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

// TestLoad_WorkerStaleTimeoutDefaults 验证未配置时孤儿回收超时回落默认分钟数。
func TestLoad_WorkerStaleTimeoutDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.ASRStaleTimeoutMin != DefaultASRStaleTimeoutMin {
		t.Errorf("ASRStaleTimeoutMin = %d, want %d", cfg.Worker.ASRStaleTimeoutMin, DefaultASRStaleTimeoutMin)
	}
	if cfg.Worker.AISliceStaleTimeoutMin != DefaultAISliceStaleTimeoutMin {
		t.Errorf("AISliceStaleTimeoutMin = %d, want %d", cfg.Worker.AISliceStaleTimeoutMin, DefaultAISliceStaleTimeoutMin)
	}
	if cfg.Worker.DraftStaleTimeoutMin != DefaultDraftStaleTimeoutMin {
		t.Errorf("DraftStaleTimeoutMin = %d, want %d", cfg.Worker.DraftStaleTimeoutMin, DefaultDraftStaleTimeoutMin)
	}
	if cfg.Worker.AISliceDraftStaleTimeoutMin != DefaultAISliceDraftStaleTimeoutMin {
		t.Errorf("AISliceDraftStaleTimeoutMin = %d, want %d", cfg.Worker.AISliceDraftStaleTimeoutMin, DefaultAISliceDraftStaleTimeoutMin)
	}
}

// TestLoad_WorkerStaleTimeoutEnvOverride 验证环境变量可覆盖孤儿回收超时。
func TestLoad_WorkerStaleTimeoutEnvOverride(t *testing.T) {
	t.Setenv("APP_WORKER_ASR_STALE_TIMEOUT_MIN", "45")
	t.Setenv("APP_WORKER_AI_SLICE_STALE_TIMEOUT_MIN", "15")
	t.Setenv("APP_WORKER_DRAFT_STALE_TIMEOUT_MIN", "75")
	t.Setenv("APP_WORKER_AI_SLICE_DRAFT_STALE_TIMEOUT_MIN", "100")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.ASRStaleTimeoutMin != 45 {
		t.Errorf("ASRStaleTimeoutMin = %d, want 45", cfg.Worker.ASRStaleTimeoutMin)
	}
	if cfg.Worker.AISliceStaleTimeoutMin != 15 {
		t.Errorf("AISliceStaleTimeoutMin = %d, want 15", cfg.Worker.AISliceStaleTimeoutMin)
	}
	if cfg.Worker.DraftStaleTimeoutMin != 75 {
		t.Errorf("DraftStaleTimeoutMin = %d, want 75", cfg.Worker.DraftStaleTimeoutMin)
	}
	if cfg.Worker.AISliceDraftStaleTimeoutMin != 100 {
		t.Errorf("AISliceDraftStaleTimeoutMin = %d, want 100", cfg.Worker.AISliceDraftStaleTimeoutMin)
	}
}

// TestLoad_WorkerStaleTimeoutInvalidEnvFallsBackToDefault 验证非法超时环境变量回落默认值。
func TestLoad_WorkerStaleTimeoutInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("APP_WORKER_ASR_STALE_TIMEOUT_MIN", "0")
	t.Setenv("APP_WORKER_AI_SLICE_STALE_TIMEOUT_MIN", "-1")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Worker.ASRStaleTimeoutMin != DefaultASRStaleTimeoutMin {
		t.Errorf("ASRStaleTimeoutMin = %d, want %d", cfg.Worker.ASRStaleTimeoutMin, DefaultASRStaleTimeoutMin)
	}
	if cfg.Worker.AISliceStaleTimeoutMin != DefaultAISliceStaleTimeoutMin {
		t.Errorf("AISliceStaleTimeoutMin = %d, want %d", cfg.Worker.AISliceStaleTimeoutMin, DefaultAISliceStaleTimeoutMin)
	}
}

// TestWorkerConfig_StaleTimeoutHelpers 验证分钟数转 Duration 的辅助方法。
func TestWorkerConfig_StaleTimeoutHelpers(t *testing.T) {
	cfg := WorkerConfig{
		ASRStaleTimeoutMin:          45,
		AISliceStaleTimeoutMin:      15,
		DraftStaleTimeoutMin:        75,
		AISliceDraftStaleTimeoutMin: 100,
	}
	if got := cfg.ASRStaleTimeout(); got != 45*time.Minute {
		t.Errorf("ASRStaleTimeout = %v, want 45m", got)
	}
	if got := cfg.AISliceStaleTimeout(); got != 15*time.Minute {
		t.Errorf("AISliceStaleTimeout = %v, want 15m", got)
	}
	if got := cfg.DraftStaleTimeout(); got != 75*time.Minute {
		t.Errorf("DraftStaleTimeout = %v, want 75m", got)
	}
	if got := cfg.AISliceDraftStaleTimeout(); got != 100*time.Minute {
		t.Errorf("AISliceDraftStaleTimeout = %v, want 100m", got)
	}

	empty := WorkerConfig{}
	if got := empty.ASRStaleTimeout(); got != time.Duration(DefaultASRStaleTimeoutMin)*time.Minute {
		t.Errorf("empty ASRStaleTimeout = %v", got)
	}
	if got := empty.AISliceStaleTimeout(); got != time.Duration(DefaultAISliceStaleTimeoutMin)*time.Minute {
		t.Errorf("empty AISliceStaleTimeout = %v", got)
	}
	if got := empty.DraftStaleTimeout(); got != time.Duration(DefaultDraftStaleTimeoutMin)*time.Minute {
		t.Errorf("empty DraftStaleTimeout = %v", got)
	}
	if got := empty.AISliceDraftStaleTimeout(); got != time.Duration(DefaultAISliceDraftStaleTimeoutMin)*time.Minute {
		t.Errorf("empty AISliceDraftStaleTimeout = %v", got)
	}
}
