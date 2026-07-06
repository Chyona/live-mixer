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
	Username string `json:"username" binding:"required" example:"admin"`
	Password string `json:"password" binding:"required" example:"123456"`
}

// LoginResponse 登录成功时 data 字段结构。
type LoginResponse struct {
	Token     string   `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	ExpiresIn int      `json:"expires_in" example:"7200"`
	ID        string   `json:"id" example:"1"`
	Username  string   `json:"username" example:"admin"`
	Nickname  string   `json:"nickname" example:"管理员"`
	Avatar    string   `json:"avatar" example:"https://cdn.example.com/avatar/1.jpg"`
	Roles     []string `json:"roles" example:"ADMIN"`
}

// Login 用户名密码登录
// @Summary      用户名密码登录
// @Description  使用用户名和密码登录，校验通过后返回 JWT Token 及用户信息。本接口无需鉴权。登录失败（用户名不存在、密码错误、账号被禁用）统一返回 401，避免泄露账号是否存在。
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        body  body      LoginRequest  true  "登录信息"
// @Success      200   {object}  response.Body{data=LoginResponse}  "登录成功"
// @Failure      400   {object}  response.Body  "请求参数错误（如缺少 username 或 password）"
// @Failure      401   {object}  response.Body  "用户名或密码错误，或账号已被禁用"
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
