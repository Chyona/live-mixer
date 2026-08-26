package storage

import "time"

const (
	// DefaultSignedURLExpireDays 上传后返回的签名链接默认有效期（天）。
	DefaultSignedURLExpireDays = 30
	// tosSignedURLMaxExpireSeconds 火山引擎 TOS 预签名链接最长 7 天（SDK 限制）。
	tosSignedURLMaxExpireSeconds = 604800
)

// ResolveSignedURLExpireDays 解析签名链接有效期天数。
// 0 表示不签名、无有效期；负数视为非法，回退 DefaultSignedURLExpireDays。
func ResolveSignedURLExpireDays(days int) int {
	if days < 0 {
		return DefaultSignedURLExpireDays
	}
	return days
}

// useUnsignedObjectURL 为 true 时返回对象直链（不带签名、无有效期）。
func useUnsignedObjectURL(days int) bool {
	return ResolveSignedURLExpireDays(days) == 0
}

// signedURLExpireDuration 将天数转换为各后端可用的签名有效期。
// 天数为 0 时返回 0；TOS 最长 7 天，超出时自动截断至上限。
func signedURLExpireDuration(days int, provider ProviderType) time.Duration {
	resolved := ResolveSignedURLExpireDays(days)
	if resolved == 0 {
		return 0
	}
	seconds := int64(resolved) * 24 * 3600
	if provider == ProviderTOS && seconds > tosSignedURLMaxExpireSeconds {
		seconds = tosSignedURLMaxExpireSeconds
	}
	return time.Duration(seconds) * time.Second
}
