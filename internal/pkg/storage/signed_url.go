package storage

import "time"

const (
	// DefaultSignedURLExpireDays 上传后返回的签名链接默认有效期（天）。
	DefaultSignedURLExpireDays = 30
	// tosSignedURLMaxExpireSeconds 火山引擎 TOS 预签名链接最长 7 天（SDK 限制）。
	tosSignedURLMaxExpireSeconds = 604800
)

// ResolveSignedURLExpireDays 解析签名链接有效期天数，未配置或非法值时使用 DefaultSignedURLExpireDays。
func ResolveSignedURLExpireDays(days int) int {
	if days <= 0 {
		return DefaultSignedURLExpireDays
	}
	return days
}

// signedURLExpireDuration 将天数转换为各后端可用的签名有效期。
// TOS 最长 7 天，超出时自动截断至上限。
func signedURLExpireDuration(days int, provider ProviderType) time.Duration {
	seconds := int64(ResolveSignedURLExpireDays(days)) * 24 * 3600
	if provider == ProviderTOS && seconds > tosSignedURLMaxExpireSeconds {
		seconds = tosSignedURLMaxExpireSeconds
	}
	return time.Duration(seconds) * time.Second
}
