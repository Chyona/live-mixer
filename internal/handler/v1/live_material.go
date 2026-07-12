package v1

import (
	"strconv"

	"live-mixer/internal/middleware"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
)

// liveMaterialDefaultPageSize 直播素材列表默认每页条数。
const liveMaterialDefaultPageSize = 20

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

// ListLiveMaterials 直播素材列表
// @Summary      直播素材列表
// @Description  分页查询直播素材，返回全部字段（含 live_asr），默认每页 20 条
// @Tags         直播素材
// @Produce      json
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页数量，默认 20"
// @Success      200        {object}  response.Body
// @Failure      401        {object}  response.Body
// @Router       /v1/live-materials [get]
func (h *LiveMaterialHandler) ListLiveMaterials(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(liveMaterialDefaultPageSize)))
	page, pageSize = utils.DefaultPage(page, pageSize)

	materials, total, err := h.liveMaterialService.List(c.Request.Context(), page, pageSize)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     materials,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
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
