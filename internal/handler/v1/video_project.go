package v1

import (
	"errors"

	"live-mixer/internal/middleware"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
)

// ListVideoProjectsRequest 剪辑项目列表查询参数。
type ListVideoProjectsRequest struct {
	Keywords  string `form:"keywords"`   // 原始字符串，如 "发布会,2026"
	StartDate string `form:"start_date"` // 开始日期 YYYY-MM-DD
	EndDate   string `form:"end_date"`   // 结束日期 YYYY-MM-DD
	Page      int    `form:"page" binding:"omitempty,min=1"`
	PageSize  int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// VideoProjectHandler 剪辑项目 HTTP 处理器。
type VideoProjectHandler struct {
	videoProjectService service.VideoProjectService
}

// NewVideoProjectHandler 创建剪辑项目处理器实例。
func NewVideoProjectHandler(videoProjectService service.VideoProjectService) *VideoProjectHandler {
	return &VideoProjectHandler{videoProjectService: videoProjectService}
}

// CreateVideoProjectRequest 创建剪辑项目请求体。
type CreateVideoProjectRequest struct {
	Name   string `json:"name" binding:"required,max=64"`
	Remark string `json:"remark" binding:"max=256"`
	LiveID uint   `json:"live_id" binding:"required"`
	Clips0 string `json:"clips0"`
	Clips1 string `json:"clips1"`
}

// UpdateVideoProjectRequest 更新剪辑项目请求体（字段可选，传则更新）。
type UpdateVideoProjectRequest struct {
	Name     *string `json:"name" binding:"omitempty,max=64"`
	Remark   *string `json:"remark" binding:"omitempty,max=256"`
	Clips0   *string `json:"clips0"`
	Clips1   *string `json:"clips1"`
	DraftURL *string `json:"draft_url" binding:"omitempty,max=1024"`
	VideoURL *string `json:"video_url" binding:"omitempty,max=1024"`
}

// ListVideoProjects 剪辑项目列表
// @Summary      剪辑项目列表
// @Description  分页查询剪辑项目，支持关键词与日期筛选
// @Tags         剪辑项目
// @Produce      json
// @Param        keywords     query  string  false  "关键词，英文逗号分隔，匹配 name/remark"
// @Param        start_date   query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date     query  string  false  "结束日期 YYYY-MM-DD"
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页数量，默认 10"
// @Success      200          {object}  response.Body
// @Failure      400          {object}  response.Body
// @Failure      401          {object}  response.Body
// @Router       /v1/video-projects [get]
func (h *VideoProjectHandler) ListVideoProjects(c *gin.Context) {
	var req ListVideoProjectsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, pageSize := utils.DefaultPage(req.Page, req.PageSize)

	projects, total, err := h.videoProjectService.List(c.Request.Context(), page, pageSize, service.VideoProjectListOptions{
		Keywords:  req.Keywords,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     projects,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// CreateVideoProject 创建剪辑项目
// @Summary      创建剪辑项目
// @Description  添加一条剪辑项目，创建人取自 JWT 当前用户
// @Tags         剪辑项目
// @Accept       json
// @Produce      json
// @Param        body  body      CreateVideoProjectRequest  true  "项目信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Router       /v1/video-projects [post]
func (h *VideoProjectHandler) CreateVideoProject(c *gin.Context) {
	var req CreateVideoProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	user, ok := middleware.GetAuthUser(c)
	if !ok {
		response.Unauthorized(c, "未登录")
		return
	}

	project, err := h.videoProjectService.Create(
		c.Request.Context(), user.ID, req.Name, req.Remark, req.LiveID, req.Clips0, req.Clips1,
	)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, project)
}

// GetVideoProject 获取剪辑项目详情
// @Summary      获取剪辑项目详情
// @Description  根据 ID 查询剪辑项目
// @Tags         剪辑项目
// @Produce      json
// @Param        id   path  int  true  "项目 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/video-projects/{id} [get]
func (h *VideoProjectHandler) GetVideoProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的项目 ID")
		return
	}

	project, err := h.videoProjectService.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrVideoProjectNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, project)
}

// UpdateVideoProject 更新剪辑项目
// @Summary      更新剪辑项目
// @Description  更新 name、remark、clips0、clips1、draft_url、video_url，未传字段保持不变
// @Tags         剪辑项目
// @Accept       json
// @Produce      json
// @Param        id    path      int                        true  "项目 ID"
// @Param        body  body      UpdateVideoProjectRequest  true  "更新内容"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      404   {object}  response.Body
// @Router       /v1/video-projects/{id} [put]
func (h *VideoProjectHandler) UpdateVideoProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的项目 ID")
		return
	}

	var req UpdateVideoProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	project, err := h.videoProjectService.Update(c.Request.Context(), id, service.VideoProjectUpdateInput{
		Name:     req.Name,
		Remark:   req.Remark,
		Clips0:   req.Clips0,
		Clips1:   req.Clips1,
		DraftURL: req.DraftURL,
		VideoURL: req.VideoURL,
	})
	if err != nil {
		if errors.Is(err, service.ErrVideoProjectNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, project)
}

// DeleteVideoProject 删除剪辑项目
// @Summary      删除剪辑项目
// @Description  物理删除剪辑项目
// @Tags         剪辑项目
// @Produce      json
// @Param        id   path  int  true  "项目 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/video-projects/{id} [delete]
func (h *VideoProjectHandler) DeleteVideoProject(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的项目 ID")
		return
	}

	if err := h.videoProjectService.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrVideoProjectNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.SuccessWithMessage(c, "删除成功", nil)
}
