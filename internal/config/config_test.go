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

	// 内嵌默认配置中 storage 字段为空占位
	if cfg.Storage.COS.Region != "" {
		t.Errorf("COS.Region = %q, want empty default", cfg.Storage.COS.Region)
	}
	if cfg.Storage.OSS.Endpoint != "" {
		t.Errorf("OSS.Endpoint = %q, want empty default", cfg.Storage.OSS.Endpoint)
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
