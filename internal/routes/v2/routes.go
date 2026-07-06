// Package v2 注册 API v2 路由分组。
package v2

import (
	v2handler "live-mixer/internal/handler/v2"
	"live-mixer/internal/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册 v2 版本全部路由。
func RegisterRoutes(rg *gin.RouterGroup, accountHandler *v2handler.AccountHandler, jwtSecret string) {
	// v2 全部接口均需 JWT 鉴权
	authorized := rg.Group("", middleware.JWTAuth(jwtSecret))

	accounts := authorized.Group("/accounts")
	{
		accounts.GET("/:id/profile", accountHandler.GetAccountProfile)
	}
}
