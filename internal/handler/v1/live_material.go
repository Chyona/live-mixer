package v1

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"live-mixer/internal/middleware"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

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

// RetryASRRequest 重新触发 ASR 请求体。
type RetryASRRequest struct {
	Force bool `json:"force"` // 为 true 时允许覆盖已完成的 ASR
}

// LiveMaterialDetailResponse 直播素材详情响应，live_asr 为分句数组格式。
type LiveMaterialDetailResponse struct {
	ID           uint            `json:"id"`
	Name         string          `json:"name"`
	Remark       string          `json:"remark"`
	LiveURL      string          `json:"live_url"`
	LiveASR      []asr.Utterance `json:"live_asr"`
	Duration     int64           `json:"duration"`
	ASRStatus    string          `json:"asr_status"`
	ASRProgress  int16           `json:"asr_progress"`
	ASRErrorMsg  string          `json:"asr_error_msg,omitempty"`
	ASRStartedAt *time.Time      `json:"asr_started_at,omitempty"`
	ASRUpdatedAt *time.Time      `json:"asr_updated_at,omitempty"`
	CreatedBy    uint            `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Ext          string          `json:"ext"`
}

func toLiveMaterialDetailResponse(material *model.LiveMaterial) LiveMaterialDetailResponse {
	return LiveMaterialDetailResponse{
		ID:           material.ID,
		Name:         material.Name,
		Remark:       material.Remark,
		LiveURL:      material.LiveURL,
		LiveASR:      asr.FormatUtterancesForAPI(material.LiveASR),
		Duration:     material.Duration,
		ASRStatus:    material.ASRStatus,
		ASRProgress:  material.ASRProgress,
		ASRErrorMsg:  material.ASRErrorMsg,
		ASRStartedAt: material.ASRStartedAt,
		ASRUpdatedAt: material.ASRUpdatedAt,
		CreatedBy:    material.CreatedBy,
		CreatedAt:    material.CreatedAt,
		UpdatedAt:    material.UpdatedAt,
		Ext:          material.Ext,
	}
}

// ListLiveMaterialsRequest 直播素材列表查询参数。
type ListLiveMaterialsRequest struct {
	StartDate     string `form:"start_date"`
	EndDate       string `form:"end_date"`
	TitleKeyword  string `form:"title_keyword"`  // 原始字符串，如 "游戏,周末"
	GlobalKeyword string `form:"global_keyword"` // 原始字符串，如 "发布会,2026"
	Page          int    `form:"page" binding:"omitempty,min=1"`
	PageSize      int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// ListLiveMaterials 直播素材列表
// @Summary      直播素材列表
// @Description  分页查询直播素材，支持日期与关键词筛选，不含 live_asr 字段，默认每页 10 条
// @Tags         直播素材
// @Produce      json
// @Param        start_date      query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date        query  string  false  "结束日期 YYYY-MM-DD"
// @Param        title_keyword   query  string  false  "标题关键词，英文逗号分隔，匹配 name/remark"
// @Param        global_keyword  query  string  false  "全局关键词，英文逗号分隔，匹配 live_url/asr_error_msg/name/remark"
// @Param        page            query  int     false  "页码"
// @Param        page_size       query  int     false  "每页数量，默认 10"
// @Success      200             {object}  response.Body
// @Failure      400             {object}  response.Body
// @Failure      401             {object}  response.Body
// @Router       /v1/live-materials [get]
func (h *LiveMaterialHandler) ListLiveMaterials(c *gin.Context) {
	var req ListLiveMaterialsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, pageSize := utils.DefaultPage(req.Page, req.PageSize)

	materials, total, err := h.liveMaterialService.List(c.Request.Context(), page, pageSize, service.LiveMaterialListOptions{
		StartDate:     req.StartDate,
		EndDate:       req.EndDate,
		TitleKeyword:  req.TitleKeyword,
		GlobalKeyword: req.GlobalKeyword,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
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

// GetLiveMaterial 获取直播素材详情
// @Summary      获取直播素材详情
// @Description  根据 ID 查询直播素材完整信息；live_asr 为分句数组，含 speaker、时间戳与字级 words
// @Tags         直播素材
// @Produce      json
// @Param        id   path  int  true  "素材 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/live-materials/{id} [get]
func (h *LiveMaterialHandler) GetLiveMaterial(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}

	material, err := h.liveMaterialService.Get(c.Request.Context(), uint(id))
	if err != nil {
		if errors.Is(err, service.ErrLiveMaterialNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, toLiveMaterialDetailResponse(material))
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
		if errors.Is(err, service.ErrLiveMaterialNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, material)
}

// DeleteLiveMaterial 删除直播素材
// @Summary      删除直播素材
// @Description  物理删除直播素材，并级联删除 video_project 中关联的剪辑项目
// @Tags         直播素材
// @Produce      json
// @Param        id   path  int  true  "素材 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/live-materials/{id} [delete]
func (h *LiveMaterialHandler) DeleteLiveMaterial(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}

	if err := h.liveMaterialService.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrLiveMaterialNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.SuccessWithMessage(c, "删除成功", nil)
}

// RetryASR 重新触发直播素材 ASR
// @Summary      重新 ASR
// @Description  将素材 ASR 重置为 pending 并后台重新识别；processing 中不可提交；completed 需 force=true
// @Tags         直播素材
// @Accept       json
// @Produce      json
// @Param        id    path  int              true  "素材 ID"
// @Param        body  body  RetryASRRequest  false "是否强制覆盖已完成结果"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      404   {object}  response.Body
// @Router       /v1/live-materials/{id}/asr/retry [post]
func (h *LiveMaterialHandler) RetryASR(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}

	var req RetryASRRequest
	// 允许空 body
	_ = c.ShouldBindJSON(&req)

	material, err := h.liveMaterialService.RetryASR(c.Request.Context(), id, req.Force)
	if err != nil {
		if errors.Is(err, service.ErrLiveMaterialNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{
		"id":            material.ID,
		"asr_status":    material.ASRStatus,
		"asr_progress":  material.ASRProgress,
		"asr_error_msg": material.ASRErrorMsg,
	})
}

// DownloadASRSubtitle 下载直播素材 ASR 字幕（JSON 文件）
// @Summary      下载 ASR 字幕
// @Description  同步返回 live_asr 原始 JSON 文件；仅 asr_status=completed 且内容非空时可下载
// @Tags         直播素材
// @Produce      application/json
// @Param        id   path  int  true  "素材 ID"
// @Success      200  {file}    file  "ASR 字幕 JSON 文件"
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/live-materials/{id}/asr/subtitle [get]
func (h *LiveMaterialHandler) DownloadASRSubtitle(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}

	content, fileName, err := h.liveMaterialService.DownloadASRSubtitle(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrLiveMaterialNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, service.ErrASRSubtitleNotReady) || errors.Is(err, service.ErrASRSubtitleEmpty) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	c.Data(http.StatusOK, "application/json; charset=utf-8", content)
}
