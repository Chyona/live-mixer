package v1

import (
	"context"
	"errors"
	"time"

	"live-mixer/internal/middleware"
	"live-mixer/internal/model"
	"live-mixer/internal/repository"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
)

// ListVideoProjectsRequest 剪辑项目列表查询参数。
type ListVideoProjectsRequest struct {
	Keywords  string `form:"keywords"`   // 如 "发布会,2026|精剪"；","=与，"|"=或
	StartDate string `form:"start_date"` // 开始日期 YYYY-MM-DD
	EndDate   string `form:"end_date"`   // 结束日期 YYYY-MM-DD
	Page      int    `form:"page" binding:"omitempty,min=1"`
	PageSize  int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// VideoProjectHandler 剪辑项目 HTTP 处理器。
type VideoProjectHandler struct {
	videoProjectService service.VideoProjectService
	createdBy           createdByResolver
}

// NewVideoProjectHandler 创建剪辑项目处理器实例。
// accountRepo 用于将 created_by 账号 ID 解析为 nickname/username 展示名。
func NewVideoProjectHandler(videoProjectService service.VideoProjectService, accountRepo repository.AccountRepository) *VideoProjectHandler {
	return &VideoProjectHandler{
		videoProjectService: videoProjectService,
		createdBy:           newCreatedByResolver(accountRepo),
	}
}

// CreateVideoProjectRequest 创建剪辑项目请求体。
// Clips0 / Clips1 为可选 JSON 数组；未传时存为空数组。
type CreateVideoProjectRequest struct {
	Name           string               `json:"name" binding:"required,max=64"`
	Remark         string               `json:"remark" binding:"max=256"`
	LiveID         uint                 `json:"live_id" binding:"required"`
	PromptID       uint                 `json:"prompt_id"` // 提示词 ID，未传或为 0 时默认 1
	Clips0         []model.ClipRange    `json:"clips0"`
	Clips1         []model.ClipWithText `json:"clips1"`
	// Width / Height 可选：仅支持 1920×1080 或 1080×1920；都不传时按素材分辨率自动选档。
	Width          int                  `json:"width" binding:"omitempty,min=0"`
	Height         int                  `json:"height" binding:"omitempty,min=0"`
	ProjectSource  string               `json:"project_source" binding:"max=32"` // 项目来源，未传默认为空
	EnableCaptions *bool                `json:"enable_captions"`                 // 是否添加字幕；未传默认 true，入库为 0/1
}

// UpdateVideoProjectRequest 更新剪辑项目请求体。
// 指针字段为 nil 表示未传，不更新；非 nil（含空数组/空字符串）表示要更新为该值。
type UpdateVideoProjectRequest struct {
	Name           *string               `json:"name" binding:"omitempty,max=64"`
	Remark         *string               `json:"remark" binding:"omitempty,max=256"`
	PromptID       *uint                 `json:"prompt_id"`
	Clips0         *[]model.ClipRange    `json:"clips0"`
	Clips1         *[]model.ClipWithText `json:"clips1"`
	Width          *int                  `json:"width" binding:"omitempty,min=0"`
	Height         *int                  `json:"height" binding:"omitempty,min=0"`
	ProjectSource  *string               `json:"project_source" binding:"omitempty,max=32"`
	EnableCaptions *bool                 `json:"enable_captions"` // 是否添加字幕；入库为 0/1
}

// enableCaptionsToInt 将前端 bool 转为库内 0/1；nil 表示未传。
func enableCaptionsToInt(v *bool) *int {
	if v == nil {
		return nil
	}
	n := model.EnableCaptionsOff
	if *v {
		n = model.EnableCaptionsOn
	}
	return &n
}

// VideoProjectResponse 剪辑项目 API 响应。
// created_by 为创建人展示名（nickname 优先，否则 username），不是账号 ID。
type VideoProjectResponse struct {
	ID             uint                 `json:"id"`
	Name           string               `json:"name"`
	Remark         string               `json:"remark"`
	LiveID         uint                 `json:"live_id"`
	PromptID       uint                 `json:"prompt_id"`
	Clips0         []model.ClipRange    `json:"clips0"`
	Clips1         []model.ClipWithText `json:"clips1"`
	Width          int                  `json:"width"`
	Height         int                  `json:"height"`
	ProjectSource  string               `json:"project_source"`
	EnableCaptions int                  `json:"enable_captions"`
	CreatedBy      string               `json:"created_by"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
	Ext            string               `json:"ext"`
}

func (h *VideoProjectHandler) toVideoProjectResponse(ctx context.Context, project *model.VideoProject) VideoProjectResponse {
	clips0 := project.Clips0
	if clips0 == nil {
		clips0 = []model.ClipRange{}
	}
	clips1 := project.Clips1
	if clips1 == nil {
		clips1 = []model.ClipWithText{}
	}
	return VideoProjectResponse{
		ID:             project.ID,
		Name:           project.Name,
		Remark:         project.Remark,
		LiveID:         project.LiveID,
		PromptID:       project.PromptID,
		Clips0:         clips0,
		Clips1:         clips1,
		Width:          project.Width,
		Height:         project.Height,
		ProjectSource:  project.ProjectSource,
		EnableCaptions: project.EnableCaptions,
		CreatedBy:      h.createdBy.nameOf(ctx, project.CreatedBy),
		CreatedAt:      project.CreatedAt,
		UpdatedAt:      project.UpdatedAt,
		Ext:            project.Ext,
	}
}

func (h *VideoProjectHandler) toVideoProjectResponseList(ctx context.Context, items []model.VideoProjectListItem) []VideoProjectListResponse {
	ids := make([]uint, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].CreatedBy)
	}
	names := h.createdBy.namesOf(ctx, uniqueAccountIDs(ids))
	out := make([]VideoProjectListResponse, 0, len(items))
	for i := range items {
		out = append(out, h.toVideoProjectListResponse(&items[i], names[items[i].CreatedBy]))
	}
	return out
}

// VideoProjectListResponse 剪辑项目列表项响应；含 live_id 与关联素材名称 live_name。
type VideoProjectListResponse struct {
	ID             uint                 `json:"id"`
	Name           string               `json:"name"`
	Remark         string               `json:"remark"`
	LiveID         uint                 `json:"live_id"`
	LiveName       string               `json:"live_name"`
	PromptID       uint                 `json:"prompt_id"`
	Clips0         []model.ClipRange    `json:"clips0"`
	Clips1         []model.ClipWithText `json:"clips1"`
	Width          int                  `json:"width"`
	Height         int                  `json:"height"`
	ProjectSource  string               `json:"project_source"`
	EnableCaptions int                  `json:"enable_captions"`
	CreatedBy      string               `json:"created_by"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
	Ext            string               `json:"ext"`
	TaskCount      int64                `json:"task_count"`
}

func (h *VideoProjectHandler) toVideoProjectListResponse(item *model.VideoProjectListItem, createdByName string) VideoProjectListResponse {
	clips0 := item.Clips0
	if clips0 == nil {
		clips0 = []model.ClipRange{}
	}
	clips1 := item.Clips1
	if clips1 == nil {
		clips1 = []model.ClipWithText{}
	}
	return VideoProjectListResponse{
		ID:             item.ID,
		Name:           item.Name,
		Remark:         item.Remark,
		LiveID:         item.LiveID,
		LiveName:       item.LiveName,
		PromptID:       item.PromptID,
		Clips0:         clips0,
		Clips1:         clips1,
		Width:          item.Width,
		Height:         item.Height,
		ProjectSource:  item.ProjectSource,
		EnableCaptions: item.EnableCaptions,
		CreatedBy:      createdByName,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
		Ext:            item.Ext,
		TaskCount:      item.TaskCount,
	}
}

// ListVideoProjects 剪辑项目列表
// @Summary      剪辑项目列表
// @Description  分页查询剪辑项目，支持关键词与日期筛选；列表项同时返回 live_id、live_name（关联 live_material.name）与 task_count（关联 task 总数）
// @Tags         剪辑项目
// @Produce      json
// @Param        keywords     query  string  false  "关键词：\",\"=与，\"|\"=或，匹配 name/remark/live_name；如 发布会,2026|精剪"
// @Param        start_date   query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date     query  string  false  "结束日期 YYYY-MM-DD"
// @Param        page         query  int     false  "页码"
// @Param        page_size    query  int     false  "每页数量，默认 10"
// @Success      200          {object}  response.Body
// @Failure      400          {object}  response.Body
// @Failure      401          {object}  response.Body
// @Security     BearerAuth
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
		List:     h.toVideoProjectResponseList(c.Request.Context(), projects),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// ListVideoProjectsByLiveMaterialRequest 素材关联剪辑项目列表查询参数。
type ListVideoProjectsByLiveMaterialRequest struct {
	Page     int `form:"page" binding:"omitempty,min=1"`
	PageSize int `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// ListVideoProjectsByLiveMaterial 查询素材关联的剪辑项目列表
// @Summary      素材关联的剪辑项目列表
// @Description  分页查询指定直播素材关联的剪辑项目；列表项同时返回 live_id、live_name 与 task_count；素材不存在时 404
// @Tags         直播素材
// @Produce      json
// @Param        id         path   int  true   "素材 ID"
// @Param        page       query  int  false  "页码"
// @Param        page_size  query  int  false  "每页数量，默认 10"
// @Success      200        {object}  response.Body
// @Failure      400        {object}  response.Body
// @Failure      401        {object}  response.Body
// @Failure      404        {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/live-materials/{id}/video-projects [get]
func (h *VideoProjectHandler) ListVideoProjectsByLiveMaterial(c *gin.Context) {
	liveID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}
	var req ListVideoProjectsByLiveMaterialRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, pageSize := utils.DefaultPage(req.Page, req.PageSize)

	projects, total, err := h.videoProjectService.ListByLiveMaterial(c.Request.Context(), liveID, page, pageSize)
	if err != nil {
		if errors.Is(err, service.ErrLiveMaterialNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     h.toVideoProjectResponseList(c.Request.Context(), projects),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// CreateVideoProject 创建剪辑项目
// @Summary      创建剪辑项目
// @Description  添加一条剪辑项目，创建人取自 JWT 当前用户；clips0/clips1 为可选 JSON 数组；enable_captions 可选（bool，默认 true）；width/height 可选，仅支持 1920×1080 或 1080×1920，不传时按关联素材分辨率自动选更接近的一档；成功后返回完整项目
// @Tags         剪辑项目
// @Accept       json
// @Produce      json
// @Param        body  body      CreateVideoProjectRequest  true  "项目信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Security     BearerAuth
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

	project, err := h.videoProjectService.Create(c.Request.Context(), user.ID, service.CreateVideoProjectInput{
		Name:           req.Name,
		Remark:         req.Remark,
		LiveID:         req.LiveID,
		PromptID:       req.PromptID,
		Clips0:         req.Clips0,
		Clips1:         req.Clips1,
		Width:          req.Width,
		Height:         req.Height,
		ProjectSource:  req.ProjectSource,
		EnableCaptions: enableCaptionsToInt(req.EnableCaptions),
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.toVideoProjectResponse(c.Request.Context(), project))
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
// @Security     BearerAuth
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
	response.Success(c, h.toVideoProjectResponse(c.Request.Context(), project))
}

// UpdateVideoProject 更新剪辑项目
// @Summary      更新剪辑项目
// @Description  仅更新请求中显式传入且合法的字段（name/remark/prompt_id/clips0/clips1/width/height/project_source/enable_captions）；未传字段保持不变；若传 width/height 须成对且仅为 1920×1080 或 1080×1920；enable_captions 为 bool，入库为 0/1
// @Tags         剪辑项目
// @Accept       json
// @Produce      json
// @Param        id    path      int                        true  "项目 ID"
// @Param        body  body      UpdateVideoProjectRequest  true  "更新内容"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      404   {object}  response.Body
// @Security     BearerAuth
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
		Name:           req.Name,
		Remark:         req.Remark,
		PromptID:       req.PromptID,
		Clips0:         req.Clips0,
		Clips1:         req.Clips1,
		Width:          req.Width,
		Height:         req.Height,
		ProjectSource:  req.ProjectSource,
		EnableCaptions: enableCaptionsToInt(req.EnableCaptions),
	})
	if err != nil {
		if errors.Is(err, service.ErrVideoProjectNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.toVideoProjectResponse(c.Request.Context(), project))
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
// @Security     BearerAuth
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
