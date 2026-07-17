// Package v1 提供 API v1 版本的 HTTP 处理器。
package v1

import (
	"strconv"

	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"
	"github.com/gin-gonic/gin"
)

// AccountHandler 账号相关 HTTP 处理器。
type AccountHandler struct {
	accountService service.AccountService
}

// NewAccountHandler 创建账号处理器实例。
func NewAccountHandler(accountService service.AccountService) *AccountHandler {
	return &AccountHandler{accountService: accountService}
}

// CreateAccountRequest 创建账号请求体。
type CreateAccountRequest struct {
	Username string `json:"username" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"` // 用户头像 URL
	Roles    string `json:"roles"`  // 用户角色，多个角色用逗号分隔
}

// UpdateAccountRequest 更新账号请求体。
type UpdateAccountRequest struct {
	Nickname string  `json:"nickname"`
	Avatar   *string `json:"avatar"` // 可选，用户头像 URL
	Roles    *string `json:"roles"`  // 可选，用户角色，多个角色用逗号分隔
	IsActive *int8   `json:"is_active"`
}

// CreateAccount 创建账号
// @Summary      创建账号
// @Description  注册新账号
// @Tags         账号
// @Accept       json
// @Produce      json
// @Param        body  body      CreateAccountRequest  true  "账号信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/accounts [post]
func (h *AccountHandler) CreateAccount(c *gin.Context) {
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	account, err := h.accountService.CreateAccount(c.Request.Context(), req.Username, req.Email, req.Password, req.Nickname, req.Avatar, req.Roles)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, account)
}

// GetAccount 获取账号详情
// @Summary      获取账号详情
// @Description  根据 ID 查询账号
// @Tags         账号
// @Produce      json
// @Param        id   path      int  true  "账号 ID"
// @Success      200  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/accounts/{id} [get]
func (h *AccountHandler) GetAccount(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的账号 ID")
		return
	}

	account, err := h.accountService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, account)
}

// ListAccounts 账号列表
// @Summary      账号列表
// @Description  分页查询账号
// @Tags         账号
// @Produce      json
// @Param        page       query     int  false  "页码"
// @Param        page_size  query     int  false  "每页数量"
// @Success      200        {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/accounts [get]
func (h *AccountHandler) ListAccounts(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	page, pageSize = utils.DefaultPage(page, pageSize)

	accounts, total, err := h.accountService.ListAccounts(c.Request.Context(), page, pageSize)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     accounts,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// UpdateAccount 更新账号
// @Summary      更新账号
// @Description  更新账号昵称、头像、角色或状态
// @Tags         账号
// @Accept       json
// @Produce      json
// @Param        id    path      int                   true  "账号 ID"
// @Param        body  body      UpdateAccountRequest  true  "更新内容"
// @Success      200   {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/accounts/{id} [put]
func (h *AccountHandler) UpdateAccount(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的账号 ID")
		return
	}

	var req UpdateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	account, err := h.accountService.UpdateAccount(c.Request.Context(), id, req.Nickname, req.Avatar, req.Roles, req.IsActive)
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}
	response.Success(c, account)
}

// DeleteAccount 删除账号
// @Summary      删除账号
// @Description  软删除账号
// @Tags         账号
// @Produce      json
// @Param        id   path      int  true  "账号 ID"
// @Success      200  {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/accounts/{id} [delete]
func (h *AccountHandler) DeleteAccount(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的账号 ID")
		return
	}

	if err := h.accountService.DeleteAccount(c.Request.Context(), id); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.SuccessWithMessage(c, "删除成功", nil)
}

func parseUintParam(c *gin.Context, name string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	return uint(v), err
}
