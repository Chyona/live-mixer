package storage

import "os"

// COSConfig 腾讯云对象存储（COS）连接配置。
type COSConfig struct {
	SecretID   string // 腾讯云 SecretId
	SecretKey  string // 腾讯云 SecretKey
	BucketName string // 存储桶名称
	Region     string // 地域，例如 ap-guangzhou
}

// OSSConfig 阿里云对象存储（OSS）连接配置。
type OSSConfig struct {
	AccessKeyID     string // 阿里云 AccessKeyId
	AccessKeySecret string // 阿里云 AccessKeySecret
	BucketName      string // 存储桶名称
	Endpoint        string // 访问域名，例如 oss-cn-hangzhou.aliyuncs.com
}

// Config 多对象存储配置，同时包含 COS 与 OSS。
// 当两者均配置完整时，优先使用 COS。
type Config struct {
	COS COSConfig
	OSS OSSConfig
}

// LoadConfigFromEnv 从环境变量加载对象存储配置。
//
// 腾讯云 COS（优先）：
//   - COS_SECRET_ID
//   - COS_SECRET_KEY
//   - COS_BUCKET_NAME
//   - COS_REGION
//
// 阿里云 OSS（COS 未配置完整时作为兜底）：
//   - OSS_ACCESS_KEY_ID
//   - OSS_ACCESS_KEY_SECRET
//   - OSS_BUCKET_NAME
//   - OSS_ENDPOINT
func LoadConfigFromEnv() Config {
	return Config{
		COS: COSConfig{
			SecretID:   os.Getenv("COS_SECRET_ID"),
			SecretKey:  os.Getenv("COS_SECRET_KEY"),
			BucketName: os.Getenv("COS_BUCKET_NAME"),
			Region:     os.Getenv("COS_REGION"),
		},
		OSS: OSSConfig{
			AccessKeyID:     os.Getenv("OSS_ACCESS_KEY_ID"),
			AccessKeySecret: os.Getenv("OSS_ACCESS_KEY_SECRET"),
			BucketName:      os.Getenv("OSS_BUCKET_NAME"),
			Endpoint:        os.Getenv("OSS_ENDPOINT"),
		},
	}
}

// isCOSConfigured 判断 COS 配置是否完整可用。
func isCOSConfigured(cfg COSConfig) bool {
	return cfg.SecretID != "" &&
		cfg.SecretKey != "" &&
		cfg.BucketName != "" &&
		cfg.Region != ""
}

// isOSSConfigured 判断 OSS 配置是否完整可用。
func isOSSConfigured(cfg OSSConfig) bool {
	return cfg.AccessKeyID != "" &&
		cfg.AccessKeySecret != "" &&
		cfg.BucketName != "" &&
		cfg.Endpoint != ""
}
