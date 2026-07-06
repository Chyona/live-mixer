package middleware

import (
	"strings"

	jwtpkg "live-mixer/pkg/jwt"
	"live-mixer/pkg/response"

	"github.com/gin-gonic/gin"
)

const authHeaderKey = "Authorization"
const authUserContextKey = "auth_user"

// AuthUser 已认证用户信息，从 JWT Claims 解析得到。
type AuthUser struct {
	ID       uint
	Username string
	Nickname string
	Avatar   string
	Roles    []string
}

// JWTAuth JWT 鉴权中间件，校验 Bearer Token 并将用户信息写入上下文。
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader(authHeaderKey)
		if header == "" {
			response.Unauthorized(c, "缺少 Authorization 请求头")
			c.Abort()
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.Unauthorized(c, "Authorization 格式错误，应为 Bearer <token>")
			c.Abort()
			return
		}

		token := parts[1]
		if token == "" {
			response.Unauthorized(c, "Token 不能为空")
			c.Abort()
			return
		}

		// 解析 JWT，提取内嵌的用户信息
		claims, err := jwtpkg.ParseToken(secret, token)
		if err != nil {
			response.Unauthorized(c, "Token 无效或已过期")
			c.Abort()
			return
		}

		c.Set(authUserContextKey, AuthUser{
			ID:       claims.UserID,
			Username: claims.Username,
			Nickname: claims.Nickname,
			Avatar:   claims.Avatar,
			Roles:    claims.Roles,
		})
		c.Next()
	}
}

// GetAuthUser 从上下文获取已认证用户信息。
func GetAuthUser(c *gin.Context) (AuthUser, bool) {
	if v, ok := c.Get(authUserContextKey); ok {
		if user, ok := v.(AuthUser); ok {
			return user, true
		}
	}
	return AuthUser{}, false
}
