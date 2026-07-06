// Package utils 提供字符串、时间、加密等通用工具函数。
package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// HashPassword 对密码进行 SHA256 哈希（示例实现，生产环境建议使用 bcrypt）。
func HashPassword(password string) (string, error) {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:]), nil
}

// FormatTime 格式化时间为标准字符串。
func FormatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// DefaultPage 规范化分页参数，返回 page 与 pageSize。
func DefaultPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// IsEmpty 判断字符串是否为空（含空白字符）。
func IsEmpty(s string) bool {
	return len(s) == 0
}
