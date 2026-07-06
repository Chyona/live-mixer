// Package v2 提供 API v2 版本的 HTTP 处理器（预留扩展）。
package v2

import (
	"strconv"

	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"github.com/gin-gonic/gin"
)

// AccountHandler v2 账号处理器。
type AccountHandler struct {
	accountService service.AccountService
}

// NewAccountHandler 创建 v2 账号处理器实例。
func NewAccountHandler(accountService service.AccountService) *AccountHandler {
	return &AccountHandler{accountService: accountService}
}

// GetAccountProfile 获取账号简要信息（v2 示例接口）
// @Summary      获取账号简要信息
// @Description  v2 版本账号概要接口
// @Tags         账号-v2
// @Produce      json
// @Param        id   path      int  true  "账号 ID"
// @Success      200  {object}  response.Body
// @Router       /v2/accounts/{id}/profile [get]
func (h *AccountHandler) GetAccountProfile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的账号 ID")
		return
	}

	account, err := h.accountService.GetAccount(c.Request.Context(), uint(id))
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}

	// v2 仅返回概要字段（含头像与角色）
	response.Success(c, gin.H{
		"id":       account.ID,
		"username": account.Username,
		"nickname": account.Nickname,
		"avatar":   account.Avatar,
		"roles":    account.Roles,
	})
}
