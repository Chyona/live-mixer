package storage

import (
	"os"
	"strconv"
)

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

// TOSConfig 火山引擎对象存储（TOS）连接配置。
type TOSConfig struct {
	AccessKeyID     string // 火山引擎 AccessKeyId
	AccessKeySecret string // 火山引擎 AccessKeySecret
	BucketName      string // 存储桶名称
	Region          string // 地域，例如 cn-beijing
	Endpoint        string // 可选，自定义访问域名；未设置时按地域自动生成
}

// Config 多对象存储配置，同时包含 COS、OSS 与 TOS。
// 当多个后端均配置完整时，优先级为 COS > OSS > TOS。
type Config struct {
	COS                 COSConfig
	OSS                 OSSConfig
	TOS                 TOSConfig
	BasePath            string // 对象键保存路径前缀，空值时使用 DefaultBasePath
	SignedURLExpireDays int    // 上传后返回的签名链接有效期（天），0 表示 DefaultSignedURLExpireDays
}

// LoadConfigFromEnv 从独立环境变量加载对象存储配置（不经过 config.yaml）。
//
// 应用内请优先使用 config.Load 配合 NewClientFromAppConfig，以遵循统一配置策略：
// 环境变量 APP_STORAGE_* > 外部配置文件（-config）> 内嵌 config.yaml。
//
// 本函数保留用于仅需 COS_* / OSS_* 环境变量的独立场景：
//
// 腾讯云 COS（优先）：
//   - COS_SECRET_ID
//   - COS_SECRET_KEY
//   - COS_BUCKET_NAME
//   - COS_REGION
//
// 阿里云 OSS（COS 未配置完整时作为次选）：
//   - OSS_ACCESS_KEY_ID
//   - OSS_ACCESS_KEY_SECRET
//   - OSS_BUCKET_NAME
//   - OSS_ENDPOINT
//
// 火山引擎 TOS（COS、OSS 均未配置完整时作为兜底）：
//   - TOS_ACCESS_KEY_ID
//   - TOS_ACCESS_KEY_SECRET
//   - TOS_BUCKET_NAME
//   - TOS_REGION
//   - TOS_ENDPOINT（可选）
//
// 通用：
//   - STORAGE_BASE_PATH（可选，默认 video_editing）
//   - STORAGE_SIGNED_URL_EXPIRE_DAYS（可选，默认 30）
func LoadConfigFromEnv() Config {
	signedURLExpireDays := 0
	if val := os.Getenv("STORAGE_SIGNED_URL_EXPIRE_DAYS"); val != "" {
		if days, err := strconv.Atoi(val); err == nil {
			signedURLExpireDays = days
		}
	}
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
		TOS: TOSConfig{
			AccessKeyID:     os.Getenv("TOS_ACCESS_KEY_ID"),
			AccessKeySecret: os.Getenv("TOS_ACCESS_KEY_SECRET"),
			BucketName:      os.Getenv("TOS_BUCKET_NAME"),
			Region:          os.Getenv("TOS_REGION"),
			Endpoint:        os.Getenv("TOS_ENDPOINT"),
		},
		BasePath:            os.Getenv("STORAGE_BASE_PATH"),
		SignedURLExpireDays: signedURLExpireDays,
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

// isTOSConfigured 判断 TOS 配置是否完整可用。
func isTOSConfigured(cfg TOSConfig) bool {
	return cfg.AccessKeyID != "" &&
		cfg.AccessKeySecret != "" &&
		cfg.BucketName != "" &&
		cfg.Region != ""
}
