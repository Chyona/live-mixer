package storage

import (
	"testing"

	appconfig "live-mixer/internal/config"
)

func TestConfigFromApp(t *testing.T) {
	appCfg := appconfig.StorageConfig{
		COS: appconfig.COSStorageConfig{
			SecretID: "cos-id", SecretKey: "cos-key",
			BucketName: "cos-bucket", Region: "ap-guangzhou",
		},
		OSS: appconfig.OSSStorageConfig{
			AccessKeyID: "oss-id", AccessKeySecret: "oss-secret",
			BucketName: "oss-bucket", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		},
	}

	cfg := ConfigFromApp(appCfg)

	if cfg.COS.SecretID != "cos-id" {
		t.Errorf("COS.SecretID = %q, want cos-id", cfg.COS.SecretID)
	}
	if cfg.OSS.Endpoint != "oss-cn-hangzhou.aliyuncs.com" {
		t.Errorf("OSS.Endpoint = %q, want oss-cn-hangzhou.aliyuncs.com", cfg.OSS.Endpoint)
	}
}

func TestNewClientFromAppConfig(t *testing.T) {
	appCfg := appconfig.StorageConfig{
		OSS: appconfig.OSSStorageConfig{
			AccessKeyID: "id", AccessKeySecret: "secret",
			BucketName: "bucket", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		},
	}

	client, err := NewClientFromAppConfig(appCfg)
	if err != nil {
		t.Fatalf("NewClientFromAppConfig() error = %v", err)
	}
	if client.ProviderType() != ProviderOSS {
		t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderOSS)
	}
}

func TestNewClientFromAppConfig_LoadIntegration(t *testing.T) {
	t.Setenv("APP_STORAGE_OSS_ACCESS_KEY_ID", "id")
	t.Setenv("APP_STORAGE_OSS_ACCESS_KEY_SECRET", "secret")
	t.Setenv("APP_STORAGE_OSS_BUCKET_NAME", "bucket")
	t.Setenv("APP_STORAGE_OSS_ENDPOINT", "oss-cn-hangzhou.aliyuncs.com")

	appCfg, err := appconfig.Load("")
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	client, err := NewClientFromAppConfig(appCfg.Storage)
	if err != nil {
		t.Fatalf("NewClientFromAppConfig() error = %v", err)
	}
	if client.ProviderType() != ProviderOSS {
		t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderOSS)
	}
}

func TestNewClientFromAppConfig_COSPriority(t *testing.T) {
	appCfg := appconfig.StorageConfig{
		COS: appconfig.COSStorageConfig{
			SecretID: "id", SecretKey: "key", BucketName: "bucket", Region: "ap-guangzhou",
		},
		OSS: appconfig.OSSStorageConfig{
			AccessKeyID: "oss-id", AccessKeySecret: "oss-secret",
			BucketName: "oss-bucket", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
		},
	}

	client, err := NewClientFromAppConfig(appCfg)
	if err != nil {
		t.Fatalf("NewClientFromAppConfig() error = %v", err)
	}
	if client.ProviderType() != ProviderCOS {
		t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderCOS)
	}
}
