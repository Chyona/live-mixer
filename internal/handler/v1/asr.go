// Package v1 提供 API v1 版本的 HTTP 处理器。
package v1

import (
	"encoding/json"

	"live-mixer/internal/service"
	"live-mixer/pkg/response"

	"github.com/gin-gonic/gin"
)

// ASRHandler 语音识别 HTTP 处理器。
type ASRHandler struct {
	asrService service.ASRService
}

// NewASRHandler 创建 ASR 处理器实例。
func NewASRHandler(asrService service.ASRService) *ASRHandler {
	return &ASRHandler{asrService: asrService}
}

// ASRRequest 语音识别请求体。
type ASRRequest struct {
	AudioURL string `json:"audio_url" binding:"required,url"`
}

// Transcribe 同步语音识别
// @Summary      同步语音识别
// @Description  提交公网音频 URL，同步返回豆包 ASR 完整识别结果
// @Tags         语音识别
// @Accept       json
// @Produce      json
// @Param        body  body      ASRRequest  true  "音频 URL"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      500   {object}  response.Body
// @Router       /v1/asr [post]
func (h *ASRHandler) Transcribe(c *gin.Context) {
	var req ASRRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result, err := h.asrService.Transcribe(c.Request.Context(), req.AudioURL)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	var data interface{}
	if err := json.Unmarshal(result, &data); err != nil {
		response.InternalError(c, "解析 ASR 结果失败")
		return
	}
	response.Success(c, data)
}
