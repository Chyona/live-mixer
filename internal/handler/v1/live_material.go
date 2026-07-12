package v1

import (
	"strconv"

	"live-mixer/internal/middleware"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"

	"github.com/gin-gonic/gin"
)

// LiveMaterialHandler 直播素材相关 HTTP 处理器。
type LiveMaterialHandler struct {
	liveMaterialService service.LiveMaterialService
}

// NewLiveMaterialHandler 创建直播素材处理器实例。
func NewLiveMaterialHandler(liveMaterialService service.LiveMaterialService) *LiveMaterialHandler {
	return &LiveMaterialHandler{liveMaterialService: liveMaterialService}
}

// CreateLiveMaterialRequest 创建直播素材请求体。
type CreateLiveMaterialRequest struct {
	Name    string `json:"name" binding:"required,max=64"`
	LiveURL string `json:"live_url" binding:"required,url,max=1024"`
	Remark  string `json:"remark" binding:"max=256"`
	Ext     string `json:"ext" binding:"max=1024"`
}

// UpdateLiveMaterialRequest 更新直播素材请求体（仅允许编辑 name、remark）。
type UpdateLiveMaterialRequest struct {
	Name   string `json:"name" binding:"required,max=64"`
	Remark string `json:"remark" binding:"max=256"`
}

// CreateLiveMaterial 创建直播素材
// @Summary      创建直播素材
// @Description  添加一条直播素材，name、live_url 为必填
// @Tags         直播素材
// @Accept       json
// @Produce      json
// @Param        body  body      CreateLiveMaterialRequest  true  "素材信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Router       /v1/live-materials [post]
func (h *LiveMaterialHandler) CreateLiveMaterial(c *gin.Context) {
	var req CreateLiveMaterialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 创建人取自 JWT 当前用户，不由客户端传入。
	user, ok := middleware.GetAuthUser(c)
	if !ok {
		response.Unauthorized(c, "未登录")
		return
	}

	material, err := h.liveMaterialService.Create(
		c.Request.Context(), user.ID, req.Name, req.LiveURL, req.Remark, req.Ext,
	)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, material)
}

// UpdateLiveMaterial 更新直播素材
// @Summary      更新直播素材
// @Description  仅可编辑 name、remark，其它字段不可修改
// @Tags         直播素材
// @Accept       json
// @Produce      json
// @Param        id    path      int                        true  "素材 ID"
// @Param        body  body      UpdateLiveMaterialRequest  true  "更新内容"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      404   {object}  response.Body
// @Router       /v1/live-materials/{id} [put]
func (h *LiveMaterialHandler) UpdateLiveMaterial(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}

	var req UpdateLiveMaterialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	material, err := h.liveMaterialService.Update(c.Request.Context(), uint(id), req.Name, req.Remark)
	if err != nil {
		if err.Error() == "直播素材不存在" {
			response.NotFound(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, material)
}
