package storage

import appconfig "live-mixer/internal/config"

// ConfigFromApp 将应用全局配置中的 storage 段转换为 storage 包配置。
func ConfigFromApp(cfg appconfig.StorageConfig) Config {
	return Config{
		BasePath:            cfg.BasePath,
		SignedURLExpireDays: cfg.SignedURLExpireDays,
		COS: COSConfig{
			SecretID:   cfg.COS.SecretID,
			SecretKey:  cfg.COS.SecretKey,
			BucketName: cfg.COS.BucketName,
			Region:     cfg.COS.Region,
		},
		OSS: OSSConfig{
			AccessKeyID:     cfg.OSS.AccessKeyID,
			AccessKeySecret: cfg.OSS.AccessKeySecret,
			BucketName:      cfg.OSS.BucketName,
			Endpoint:        cfg.OSS.Endpoint,
		},
		TOS: TOSConfig{
			AccessKeyID:     cfg.TOS.AccessKeyID,
			AccessKeySecret: cfg.TOS.AccessKeySecret,
			BucketName:      cfg.TOS.BucketName,
			Region:          cfg.TOS.Region,
			Endpoint:        cfg.TOS.Endpoint,
		},
	}
}

// NewClientFromAppConfig 从应用配置（config.yaml / APP_* 环境变量）创建对象存储客户端。
// 配置加载请使用 config.Load，以保证与项目统一的配置优先级策略。
func NewClientFromAppConfig(cfg appconfig.StorageConfig, opts ...UploadOptions) (*Client, error) {
	return NewClient(ConfigFromApp(cfg), opts...)
}
