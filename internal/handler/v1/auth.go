// Package v1 提供 API v1 版本的 HTTP 处理器。
package v1

import (
	"live-mixer/internal/service"
	"live-mixer/pkg/response"

	"github.com/gin-gonic/gin"
)

// AuthHandler 认证相关 HTTP 处理器。
type AuthHandler struct {
	authService service.AuthService
}

// NewAuthHandler 创建认证处理器实例。
func NewAuthHandler(authService service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// LoginRequest 登录请求体。
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 用户名密码登录
// @Summary      用户名密码登录
// @Description  校验账号密码并返回 JWT Token
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body      LoginRequest  true  "登录信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Router       /v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.authService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		// 登录失败统一返回 401，避免泄露账号是否存在
		response.Unauthorized(c, err.Error())
		return
	}
	response.Success(c, result)
}
