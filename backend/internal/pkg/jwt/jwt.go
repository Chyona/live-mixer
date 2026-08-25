// Package jwt 提供 JWT 签发与解析，Claims 内嵌常用用户信息以减少数据库查询。
package jwt

import (
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// UserClaims JWT 载荷，包含业务常用的用户信息。
type UserClaims struct {
	UserID   uint     `json:"uid"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	Avatar   string   `json:"avatar"`
	Roles    []string `json:"roles"`
	jwtlib.RegisteredClaims
}

// GenerateToken 签发 JWT，expiresIn 单位为秒。
func GenerateToken(secret string, expiresIn int, claims UserClaims) (string, error) {
	if secret == "" {
		return "", errors.New("JWT 密钥不能为空")
	}
	if expiresIn <= 0 {
		return "", errors.New("JWT 过期时间必须大于 0")
	}

	now := time.Now()
	claims.RegisteredClaims = jwtlib.RegisteredClaims{
		IssuedAt:  jwtlib.NewNumericDate(now),
		ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Duration(expiresIn) * time.Second)),
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("签发 JWT 失败: %w", err)
	}
	return signed, nil
}

// ParseToken 解析并校验 JWT，返回用户 Claims。
func ParseToken(secret, tokenString string) (*UserClaims, error) {
	if secret == "" {
		return nil, errors.New("JWT 密钥不能为空")
	}

	token, err := jwtlib.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwtlib.Token) (interface{}, error) {
		if token.Method != jwtlib.SigningMethodHS256 {
			return nil, fmt.Errorf("不支持的签名算法: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("JWT 解析失败: %w", err)
	}

	claims, ok := token.Claims.(*UserClaims)
	if !ok || !token.Valid {
		return nil, errors.New("JWT 无效")
	}
	return claims, nil
}
