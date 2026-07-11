package storage

import (
	"os"
	"testing"
)

func TestIsCOSConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  COSConfig
		want bool
	}{
		{
			name: "完整配置",
			cfg: COSConfig{
				SecretID: "id", SecretKey: "key", BucketName: "bucket", Region: "ap-guangzhou",
			},
			want: true,
		},
		{
			name: "缺少 SecretID",
			cfg:  COSConfig{SecretKey: "key", BucketName: "bucket", Region: "ap-guangzhou"},
			want: false,
		},
		{
			name: "全部为空",
			cfg:  COSConfig{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCOSConfigured(tt.cfg); got != tt.want {
				t.Errorf("isCOSConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsOSSConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  OSSConfig
		want bool
	}{
		{
			name: "完整配置",
			cfg: OSSConfig{
				AccessKeyID: "id", AccessKeySecret: "secret",
				BucketName: "bucket", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
			},
			want: true,
		},
		{
			name: "缺少 Endpoint",
			cfg:  OSSConfig{AccessKeyID: "id", AccessKeySecret: "secret", BucketName: "bucket"},
			want: false,
		},
		{
			name: "全部为空",
			cfg:  OSSConfig{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOSSConfigured(tt.cfg); got != tt.want {
				t.Errorf("isOSSConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTOSConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  TOSConfig
		want bool
	}{
		{
			name: "完整配置",
			cfg: TOSConfig{
				AccessKeyID: "id", AccessKeySecret: "secret",
				BucketName: "bucket", Region: "cn-beijing",
			},
			want: true,
		},
		{
			name: "缺少 Region",
			cfg:  TOSConfig{AccessKeyID: "id", AccessKeySecret: "secret", BucketName: "bucket"},
			want: false,
		},
		{
			name: "全部为空",
			cfg:  TOSConfig{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTOSConfigured(tt.cfg); got != tt.want {
				t.Errorf("isTOSConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("COS_SECRET_ID", "cos-id")
	t.Setenv("COS_SECRET_KEY", "cos-key")
	t.Setenv("COS_BUCKET_NAME", "cos-bucket")
	t.Setenv("COS_REGION", "ap-shanghai")
	t.Setenv("OSS_ACCESS_KEY_ID", "oss-id")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "oss-secret")
	t.Setenv("OSS_BUCKET_NAME", "oss-bucket")
	t.Setenv("OSS_ENDPOINT", "oss-cn-beijing.aliyuncs.com")
	t.Setenv("TOS_ACCESS_KEY_ID", "tos-id")
	t.Setenv("TOS_ACCESS_KEY_SECRET", "tos-secret")
	t.Setenv("TOS_BUCKET_NAME", "tos-bucket")
	t.Setenv("TOS_REGION", "cn-beijing")
	t.Setenv("TOS_ENDPOINT", "tos-cn-beijing.volces.com")

	cfg := LoadConfigFromEnv()

	if cfg.COS.SecretID != "cos-id" {
		t.Errorf("COS.SecretID = %q, want cos-id", cfg.COS.SecretID)
	}
	if cfg.OSS.Endpoint != "oss-cn-beijing.aliyuncs.com" {
		t.Errorf("OSS.Endpoint = %q, want oss-cn-beijing.aliyuncs.com", cfg.OSS.Endpoint)
	}
	if cfg.TOS.Region != "cn-beijing" {
		t.Errorf("TOS.Region = %q, want cn-beijing", cfg.TOS.Region)
	}
}

func TestNewClient_ProviderSelection(t *testing.T) {
	cosCfg := COSConfig{
		SecretID: "id", SecretKey: "key", BucketName: "bucket", Region: "ap-guangzhou",
	}
	ossCfg := OSSConfig{
		AccessKeyID: "id", AccessKeySecret: "secret",
		BucketName: "bucket", Endpoint: "oss-cn-hangzhou.aliyuncs.com",
	}
	tosCfg := TOSConfig{
		AccessKeyID: "id", AccessKeySecret: "secret",
		BucketName: "bucket", Region: "cn-beijing",
	}

	t.Run("仅 COS", func(t *testing.T) {
		client, err := NewClient(Config{COS: cosCfg})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client.ProviderType() != ProviderCOS {
			t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderCOS)
		}
	})

	t.Run("仅 OSS", func(t *testing.T) {
		client, err := NewClient(Config{OSS: ossCfg})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client.ProviderType() != ProviderOSS {
			t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderOSS)
		}
	})

	t.Run("COS 与 OSS 同时配置时优先 COS", func(t *testing.T) {
		client, err := NewClient(Config{COS: cosCfg, OSS: ossCfg})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client.ProviderType() != ProviderCOS {
			t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderCOS)
		}
	})

	t.Run("仅 TOS", func(t *testing.T) {
		client, err := NewClient(Config{TOS: tosCfg})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client.ProviderType() != ProviderTOS {
			t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderTOS)
		}
	})

	t.Run("COS、OSS、TOS 同时配置时优先 COS", func(t *testing.T) {
		client, err := NewClient(Config{COS: cosCfg, OSS: ossCfg, TOS: tosCfg})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client.ProviderType() != ProviderCOS {
			t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderCOS)
		}
	})

	t.Run("OSS 与 TOS 同时配置时优先 OSS", func(t *testing.T) {
		client, err := NewClient(Config{OSS: ossCfg, TOS: tosCfg})
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if client.ProviderType() != ProviderOSS {
			t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderOSS)
		}
	})

	t.Run("均未配置", func(t *testing.T) {
		_, err := NewClient(Config{})
		if err == nil {
			t.Fatal("expected error when no provider configured")
		}
	})
}

func TestNewClientFromEnv(t *testing.T) {
	// 清理可能干扰测试的环境变量
	for _, key := range []string{
		"COS_SECRET_ID", "COS_SECRET_KEY", "COS_BUCKET_NAME", "COS_REGION",
		"OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET", "OSS_BUCKET_NAME", "OSS_ENDPOINT",
		"TOS_ACCESS_KEY_ID", "TOS_ACCESS_KEY_SECRET", "TOS_BUCKET_NAME", "TOS_REGION", "TOS_ENDPOINT",
	} {
		os.Unsetenv(key)
	}

	t.Setenv("OSS_ACCESS_KEY_ID", "id")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "secret")
	t.Setenv("OSS_BUCKET_NAME", "bucket")
	t.Setenv("OSS_ENDPOINT", "oss-cn-hangzhou.aliyuncs.com")

	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("NewClientFromEnv() error = %v", err)
	}
	if client.ProviderType() != ProviderOSS {
		t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderOSS)
	}
}

func TestNewClientFromEnv_TOS(t *testing.T) {
	for _, key := range []string{
		"COS_SECRET_ID", "COS_SECRET_KEY", "COS_BUCKET_NAME", "COS_REGION",
		"OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET", "OSS_BUCKET_NAME", "OSS_ENDPOINT",
		"TOS_ACCESS_KEY_ID", "TOS_ACCESS_KEY_SECRET", "TOS_BUCKET_NAME", "TOS_REGION", "TOS_ENDPOINT",
	} {
		os.Unsetenv(key)
	}

	t.Setenv("TOS_ACCESS_KEY_ID", "id")
	t.Setenv("TOS_ACCESS_KEY_SECRET", "secret")
	t.Setenv("TOS_BUCKET_NAME", "bucket")
	t.Setenv("TOS_REGION", "cn-beijing")

	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("NewClientFromEnv() error = %v", err)
	}
	if client.ProviderType() != ProviderTOS {
		t.Errorf("ProviderType() = %q, want %q", client.ProviderType(), ProviderTOS)
	}
}
