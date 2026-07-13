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
