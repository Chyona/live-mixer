package v1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"live-mixer/internal/middleware"
	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/repository"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
)

// LiveMaterialHandler 直播素材相关 HTTP 处理器。
type LiveMaterialHandler struct {
	liveMaterialService service.LiveMaterialService
	createdBy           createdByResolver
}

// NewLiveMaterialHandler 创建直播素材处理器实例。
// accountRepo 用于将 created_by 账号 ID 解析为 nickname/username 展示名。
func NewLiveMaterialHandler(liveMaterialService service.LiveMaterialService, accountRepo repository.AccountRepository) *LiveMaterialHandler {
	return &LiveMaterialHandler{
		liveMaterialService: liveMaterialService,
		createdBy:           newCreatedByResolver(accountRepo),
	}
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

// LiveMaterialDetailResponse 直播素材详情响应，live_asr 为分句数组格式。
// created_by 为创建人展示名（nickname 优先，否则 username），不是账号 ID。
type LiveMaterialDetailResponse struct {
	ID           uint            `json:"id"`
	Name         string          `json:"name"`
	Remark       string          `json:"remark"`
	LiveURL      string          `json:"live_url"`
	URLType      string          `json:"url_type"`
	LiveStatus   string          `json:"live_status"`
	LiveASR      []asr.Utterance `json:"live_asr"`
	ASRSummaries  []model.ASRSummarySegment `json:"asr_summaries"`
	ASRParagraphs []model.ASRParagraph      `json:"asr_paragraphs"`
	Duration     int64           `json:"duration"`
	Width        int             `json:"width"`
	Height       int             `json:"height"`
	ASRStatus    string          `json:"asr_status"`
	ASRProgress  int16           `json:"asr_progress"`
	ASRErrorMsg  string          `json:"asr_error_msg,omitempty"`
	ASRStartedAt   *time.Time      `json:"asr_started_at,omitempty"`
	ASRUpdatedAt   *time.Time      `json:"asr_updated_at,omitempty"`
	ASRCompletedAt *time.Time      `json:"asr_completed_at,omitempty"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Ext            string          `json:"ext"`
}

func (h *LiveMaterialHandler) toLiveMaterialDetailResponse(ctx context.Context, material *model.LiveMaterial) LiveMaterialDetailResponse {
	summaries := material.ASRSummaries
	if summaries == nil {
		summaries = []model.ASRSummarySegment{}
	}
	paragraphs := material.ASRParagraphs
	if paragraphs == nil {
		paragraphs = []model.ASRParagraph{}
	}
	return LiveMaterialDetailResponse{
		ID:             material.ID,
		Name:           material.Name,
		Remark:         material.Remark,
		LiveURL:        material.LiveURL,
		URLType:        material.URLType,
		LiveStatus:     material.LiveStatus,
		LiveASR:        asr.FormatUtterancesForAPI(material.LiveASR),
		ASRSummaries:   summaries,
		ASRParagraphs:  paragraphs,
		Duration:       material.Duration,
		Width:          material.Width,
		Height:         material.Height,
		ASRStatus:      material.ASRStatus,
		ASRProgress:    material.ASRProgress,
		ASRErrorMsg:    material.ASRErrorMsg,
		ASRStartedAt:   material.ASRStartedAt,
		ASRUpdatedAt:   material.ASRUpdatedAt,
		ASRCompletedAt: material.ASRCompletedAt,
		CreatedBy:      h.createdBy.nameOf(ctx, material.CreatedBy),
		CreatedAt:      material.CreatedAt,
		UpdatedAt:      material.UpdatedAt,
		Ext:            material.Ext,
	}
}

// LiveMaterialListResponse 直播素材列表项响应（不含 live_asr）。
type LiveMaterialListResponse struct {
	ID           uint       `json:"id"`
	Name         string     `json:"name"`
	Remark       string     `json:"remark"`
	LiveURL      string     `json:"live_url"`
	URLType      string     `json:"url_type"`
	LiveStatus   string     `json:"live_status"`
	Duration     int64      `json:"duration"`
	Width        int        `json:"width"`
	Height       int        `json:"height"`
	ASRStatus    string     `json:"asr_status"`
	ASRProgress  int16      `json:"asr_progress"`
	ASRErrorMsg  string     `json:"asr_error_msg,omitempty"`
	ASRStartedAt   *time.Time `json:"asr_started_at,omitempty"`
	ASRUpdatedAt   *time.Time `json:"asr_updated_at,omitempty"`
	ASRCompletedAt *time.Time `json:"asr_completed_at,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Ext            string     `json:"ext"`
	ProjectCount   int64      `json:"project_count"`
	// MatchedParagraphs 仅 asr_keywords 搜索时返回命中的 asr_paragraphs 段落。
	MatchedParagraphs []model.ASRParagraph `json:"matched_paragraphs,omitempty"`
}

func (h *LiveMaterialHandler) toLiveMaterialListResponse(ctx context.Context, items []model.LiveMaterialListItem) []LiveMaterialListResponse {
	ids := make([]uint, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].CreatedBy)
	}
	names := h.createdBy.namesOf(ctx, uniqueAccountIDs(ids))
	out := make([]LiveMaterialListResponse, 0, len(items))
	for i := range items {
		item := items[i]
		matched := item.MatchedParagraphs
		if matched == nil {
			matched = []model.ASRParagraph{}
		}
		out = append(out, LiveMaterialListResponse{
			ID:                item.ID,
			Name:              item.Name,
			Remark:            item.Remark,
			LiveURL:           item.LiveURL,
			URLType:           item.URLType,
			LiveStatus:        item.LiveStatus,
			Duration:          item.Duration,
			Width:             item.Width,
			Height:            item.Height,
			ASRStatus:         item.ASRStatus,
			ASRProgress:       item.ASRProgress,
			ASRErrorMsg:       item.ASRErrorMsg,
			ASRStartedAt:      item.ASRStartedAt,
			ASRUpdatedAt:      item.ASRUpdatedAt,
			ASRCompletedAt:    item.ASRCompletedAt,
			CreatedBy:         names[item.CreatedBy],
			CreatedAt:         item.CreatedAt,
			UpdatedAt:         item.UpdatedAt,
			Ext:               item.Ext,
			ProjectCount:      item.ProjectCount,
			MatchedParagraphs: matched,
		})
	}
	return out
}

// ListLiveMaterialsRequest 直播素材列表查询参数。
type ListLiveMaterialsRequest struct {
	StartDate   string `form:"start_date"`
	EndDate     string `form:"end_date"`
	Keywords    string `form:"keywords"`     // 如 "游戏,周末|发布会"；","=与，"|"=或；匹配 name/remark
	ASRKeywords string `form:"asr_keywords"` // 如 "发布会,2026|新品"；","=与，"|"=或；匹配 asr_paragraphs
	Page        int    `form:"page" binding:"omitempty,min=1"`
	PageSize    int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// ListLiveMaterials 直播素材列表
// @Summary      直播素材列表
// @Description  分页查询直播素材，支持日期与关键词筛选，不含 live_asr / asr_summaries；keywords/asr_keywords 支持 ","=与、"|"=或；asr_keywords 匹配 asr_paragraphs 并返回 matched_paragraphs；列表项含 project_count，默认每页 10 条
// @Tags         直播素材
// @Produce      json
// @Param        start_date    query  string  false  "开始日期 YYYY-MM-DD"
// @Param        end_date      query  string  false  "结束日期 YYYY-MM-DD"
// @Param        keywords      query  string  false  "标题关键词：\",\"=与，\"|\"=或，匹配 name/remark；如 游戏,周末|发布会"
// @Param        asr_keywords  query  string  false  "ASR 段落关键词：\",\"=与，\"|\"=或，匹配 asr_paragraphs；命中段落见 matched_paragraphs"
// @Param        page            query  int     false  "页码"
// @Param        page_size       query  int     false  "每页数量，默认 10"
// @Success      200             {object}  response.Body
// @Failure      400             {object}  response.Body
// @Failure      401             {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/live-materials [get]
func (h *LiveMaterialHandler) ListLiveMaterials(c *gin.Context) {
	var req ListLiveMaterialsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, pageSize := utils.DefaultPage(req.Page, req.PageSize)

	materials, total, err := h.liveMaterialService.List(c.Request.Context(), page, pageSize, service.LiveMaterialListOptions{
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		Keywords:    req.Keywords,
		ASRKeywords: req.ASRKeywords,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     h.toLiveMaterialListResponse(c.Request.Context(), materials),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// CreateLiveMaterial 创建直播素材
// @Summary      创建直播素材
// @Description  添加一条直播素材，name、live_url 为必填；url_type 由后台按 live_url 自动识别写入
// @Tags         直播素材
// @Accept       json
// @Produce      json
// @Param        body  body      CreateLiveMaterialRequest  true  "素材信息"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      409   {object}  response.Body  "素材已存在，code=40901，data 为已有记录"
// @Failure      401   {object}  response.Body
// @Security     BearerAuth
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
		var existsErr *service.LiveMaterialExistsError
		if errors.As(err, &existsErr) && existsErr.Material != nil {
			response.Conflict(
				c,
				service.CodeLiveMaterialExists,
				existsErr.Error(),
				h.toLiveMaterialDetailResponse(c.Request.Context(), existsErr.Material),
			)
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.toLiveMaterialDetailResponse(c.Request.Context(), material))
}

// GetLiveMaterial 获取直播素材详情
// @Summary      获取直播素材详情
// @Description  根据 ID 查询直播素材完整信息；live_asr 为分句数组；含 asr_summaries（AI 主题分段）与 asr_paragraphs（全文段落划分）
// @Tags         直播素材
// @Produce      json
// @Param        id   path  int  true  "素材 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Security     BearerAuth
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
	response.Success(c, h.toLiveMaterialDetailResponse(c.Request.Context(), material))
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
// @Security     BearerAuth
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
	response.Success(c, h.toLiveMaterialDetailResponse(c.Request.Context(), material))
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
// @Security     BearerAuth
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
// @Description  仅 asr_status=failed 时可重试：重置为 pending，由后台 Worker 扫库自动执行
// @Tags         直播素材
// @Produce      json
// @Param        id   path  int  true  "素材 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/live-materials/{id}/asr/retry [post]
func (h *LiveMaterialHandler) RetryASR(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的素材 ID")
		return
	}

	material, err := h.liveMaterialService.RetryASR(c.Request.Context(), id)
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

// DownloadASRSubtitle 下载直播素材 ASR 字幕（TXT 文件）
// @Summary      下载 ASR 字幕
// @Description  返回 TXT：关键词取自 asr_summaries.title；文字记录分段与 asr_paragraphs 完全一致（说话人 / start_time / text），显示为「说话人${speaker}」；仅 asr_status=completed 且 asr_paragraphs 非空时可下载
// @Tags         直播素材
// @Produce      text/plain
// @Param        id   path  int  true  "素材 ID"
// @Success      200  {file}    file  "ASR 字幕 TXT 文件"
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Security     BearerAuth
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
	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}
