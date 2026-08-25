package utils

import (
	"crypto/rand"
	"fmt"
)

// 随机密码字符集：数字 + 大小写字母（0-9a-zA-Z）。
const randomPasswordCharset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// DefaultRandomPasswordLength 默认随机密码长度。
const DefaultRandomPasswordLength = 16

// ComparePassword 校验明文密码是否与哈希值匹配。
func ComparePassword(hashed, password string) bool {
	sum, err := HashPassword(password)
	if err != nil {
		return false
	}
	return sum == hashed
}

// GenerateRandomPassword 生成指定长度的随机密码，字符仅包含 0-9a-zA-Z。
// length 必须大于 0，否则返回错误。
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("密码长度必须大于 0，got %d", length)
	}
	charsetLen := len(randomPasswordCharset)
	// 拒绝采样上限：保证模 charsetLen 时分布均匀
	maxUnbiased := byte(256 - (256 % charsetLen))
	buf := make([]byte, length)
	for i := 0; i < length; i++ {
		var b [1]byte
		for {
			if _, err := rand.Read(b[:]); err != nil {
				return "", fmt.Errorf("生成随机密码失败: %w", err)
			}
			if b[0] < maxUnbiased {
				buf[i] = randomPasswordCharset[int(b[0])%charsetLen]
				break
			}
		}
	}
	return string(buf), nil
}

// ParseRoles 将逗号分隔的角色字符串解析为数组，空字符串返回 nil。
func ParseRoles(roles string) []string {
	if roles == "" {
		return nil
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i <= len(roles); i++ {
		if i == len(roles) || roles[i] == ',' {
			part := roles[start:i]
			start = i + 1
			if part == "" {
				continue
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}
