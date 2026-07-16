package v1

import (
	"encoding/json"
	"errors"

	"live-mixer/internal/service"
	"live-mixer/pkg/response"

	"github.com/gin-gonic/gin"
)

// ChatHandler 大模型同步对话 HTTP 处理器。
type ChatHandler struct {
	chatService service.ChatService
}

// NewChatHandler 创建对话处理器实例。
func NewChatHandler(chatService service.ChatService) *ChatHandler {
	return &ChatHandler{chatService: chatService}
}

// ChatRequest 同步对话请求体。
type ChatRequest struct {
	// SysPrompt 系统提示词，可选，默认为空。
	SysPrompt string `json:"sys_prompt"`
	// UsrPrompt 用户提示词，必选。
	UsrPrompt string `json:"usr_prompt" binding:"required"`
}

// Chat 同步调用大模型并返回完整响应
// @Summary      同步大模型对话
// @Description  提交系统/用户提示词，同步返回上游 LLM 的完整 Chat Completions 结果
// @Tags         大模型对话
// @Accept       json
// @Produce      json
// @Param        body  body      ChatRequest  true  "对话请求"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      500   {object}  response.Body
// @Router       /v1/chat [post]
func (h *ChatHandler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.chatService.Chat(c.Request.Context(), req.SysPrompt, req.UsrPrompt)
	if err != nil {
		if errors.Is(err, service.ErrInvalidChatRequest) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	var data interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		response.InternalError(c, "解析 LLM 结果失败")
		return
	}
	response.Success(c, data)
}
