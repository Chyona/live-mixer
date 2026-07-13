package v1

import (
	"errors"

	"live-mixer/internal/middleware"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
)

// ListLLMSystemPromptsRequest 系统提示词列表查询参数。
type ListLLMSystemPromptsRequest struct {
	Keywords  string `form:"keywords"`   // 原始字符串，如 "直播,话术"
	StartDate string `form:"start_date"` // 开始日期 YYYY-MM-DD
	EndDate   string `form:"end_date"`   // 结束日期 YYYY-MM-DD
	Page      int    `form:"page" binding:"omitempty,min=1"`
	PageSize  int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// LLMSystemPromptHandler 大模型系统提示词 HTTP 处理器。
type LLMSystemPromptHandler struct {
	llmSystemPromptService service.LLMSystemPromptService
}

// NewLLMSystemPromptHandler 创建系统提示词处理器实例。
func NewLLMSystemPromptHandler(llmSystemPromptService service.LLMSystemPromptService) *LLMSystemPromptHandler {
	return &LLMSystemPromptHandler{llmSystemPromptService: llmSystemPromptService}
}

// CreateLLMSystemPromptRequest 创建系统提示词请求体。
type CreateLLMSystemPromptRequest struct {
	Name    string `json:"name" binding:"required,max=128"`
	Content string `json:"content" binding:"required"`
	Remark  string `json:"remark" binding:"max=256"`
	Ext     string `json:"ext" binding:"max=1024"`
}

// UpdateLLMSystemPromptRequest 更新系统提示词请求体。
type UpdateLLMSystemPromptRequest struct {
	Name    string `json:"name" binding:"required,max=128"`
	Content string `json:"content" binding:"required"`
	Remark  string `json:"remark" binding:"max=256"`
	Ext     string `json:"ext" binding:"max=1024"`
}

// ListLLMSystemPrompts 系统提示词列表
// @Summary      系统提示词列表
// @Description  分页查询系统提示词，支持关键词与日期筛选，列表返回 content_preview
// @Tags         大模型系统提示词
// @Produce      json
// @Param        keywords     query  string  false  "关键词，英文逗号分隔，匹配 name/content/remark"
// @Param        start_date   query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date     query  string  false  "结束日期 YYYY-MM-DD"
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页数量，默认 10"
// @Success      200          {object}  response.Body
// @Failure      400          {object}  response.Body
// @Failure      401          {object}  response.Body
// @Router       /v1/llm-system-prompts [get]
func (h *LLMSystemPromptHandler) ListLLMSystemPrompts(c *gin.Context) {
	var req ListLLMSystemPromptsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, pageSize := utils.DefaultPage(req.Page, req.PageSize)

	items, total, err := h.llmSystemPromptService.List(c.Request.Context(), page, pageSize, service.LLMSystemPromptListOptions{
		Keywords:  req.Keywords,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// CreateLLMSystemPrompt 创建系统提示词
// @Summary      创建系统提示词
// @Description  添加一条系统提示词，创建人取自 JWT 当前用户
// @Tags         大模型系统提示词
// @Accept       json
// @Produce      json
// @Param        body  body      CreateLLMSystemPromptRequest  true  "提示词信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Router       /v1/llm-system-prompts [post]
func (h *LLMSystemPromptHandler) CreateLLMSystemPrompt(c *gin.Context) {
	var req CreateLLMSystemPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	user, ok := middleware.GetAuthUser(c)
	if !ok {
		response.Unauthorized(c, "未登录")
		return
	}

	prompt, err := h.llmSystemPromptService.Create(
		c.Request.Context(), user.ID, req.Name, req.Content, req.Remark, req.Ext,
	)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, prompt)
}

// GetLLMSystemPrompt 获取系统提示词详情
// @Summary      获取系统提示词详情
// @Description  根据 ID 查询系统提示词完整信息（含 content）
// @Tags         大模型系统提示词
// @Produce      json
// @Param        id   path  int  true  "提示词 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/llm-system-prompts/{id} [get]
func (h *LLMSystemPromptHandler) GetLLMSystemPrompt(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的提示词 ID")
		return
	}

	prompt, err := h.llmSystemPromptService.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrLLMSystemPromptNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, prompt)
}

// UpdateLLMSystemPrompt 更新系统提示词
// @Summary      更新系统提示词
// @Description  更新 name、content、remark、ext；is_editable=0 的预置提示词不可修改
// @Tags         大模型系统提示词
// @Accept       json
// @Produce      json
// @Param        id    path      int                           true  "提示词 ID"
// @Param        body  body      UpdateLLMSystemPromptRequest  true  "更新内容"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      403   {object}  response.Body
// @Failure      404   {object}  response.Body
// @Router       /v1/llm-system-prompts/{id} [put]
func (h *LLMSystemPromptHandler) UpdateLLMSystemPrompt(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的提示词 ID")
		return
	}

	var req UpdateLLMSystemPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	prompt, err := h.llmSystemPromptService.Update(
		c.Request.Context(), id, req.Name, req.Content, req.Remark, req.Ext,
	)
	if err != nil {
		if errors.Is(err, service.ErrLLMSystemPromptNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, service.ErrLLMSystemPromptNotEditable) {
			response.Forbidden(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, prompt)
}

// DeleteLLMSystemPrompt 删除系统提示词
// @Summary      删除系统提示词
// @Description  物理删除系统提示词；is_editable=0 的预置提示词不可删除
// @Tags         大模型系统提示词
// @Produce      json
// @Param        id   path  int  true  "提示词 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      403  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/llm-system-prompts/{id} [delete]
func (h *LLMSystemPromptHandler) DeleteLLMSystemPrompt(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的提示词 ID")
		return
	}

	if err := h.llmSystemPromptService.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrLLMSystemPromptNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, service.ErrLLMSystemPromptNotDeletable) {
			response.Forbidden(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.SuccessWithMessage(c, "删除成功", nil)
}
