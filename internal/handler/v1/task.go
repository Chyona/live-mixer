package v1

import (
	"context"
	"errors"
	"strings"
	"time"

	"live-mixer/internal/middleware"
	"live-mixer/internal/model"
	"live-mixer/internal/repository"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TaskHandler 异步任务 HTTP 处理器。
type TaskHandler struct {
	taskService service.TaskService
	createdBy   createdByResolver
}

// NewTaskHandler 创建任务处理器实例。
// accountRepo 用于将 created_by 账号 ID 解析为 nickname/username 展示名。
func NewTaskHandler(taskService service.TaskService, accountRepo repository.AccountRepository) *TaskHandler {
	return &TaskHandler{
		taskService: taskService,
		createdBy:   newCreatedByResolver(accountRepo),
	}
}

// CreateAISliceTaskRequest AI 切片任务请求体（仅需 video_project_id）。
type CreateAISliceTaskRequest struct {
	VideoProjectID uint `json:"video_project_id" binding:"required"`
}

// CreateDraftTaskRequest 剪映草稿任务请求体（仅需 video_project_id）。
type CreateDraftTaskRequest struct {
	VideoProjectID uint `json:"video_project_id" binding:"required"`
	CanvasWidth    int  `json:"canvas_width"`
	CanvasHeight   int  `json:"canvas_height"`
}

// CreateAISliceDraftTaskRequest 一键成片任务请求体（先 AI 切片再生成草稿）。
type CreateAISliceDraftTaskRequest struct {
	VideoProjectID uint `json:"video_project_id" binding:"required"`
	CanvasWidth    int  `json:"canvas_width"`
	CanvasHeight   int  `json:"canvas_height"`
}

// ListTasksRequest 任务列表查询参数。
type ListTasksRequest struct {
	Type      string   `form:"type"`
	Status    string   `form:"status"`
	StartDate string   `form:"start_date"` // 开始日期 YYYY-MM-DD，按 created_at 筛选
	EndDate   string   `form:"end_date"`   // 结束日期 YYYY-MM-DD，按 created_at 筛选
	Keywords  string   `form:"keywords"`   // 可选；"," 与、"|" 或；模糊匹配 task.video_project_name
	Page      int      `form:"page" binding:"omitempty,min=1"`
	PageSize  int      `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// ListRunningTasksByProjectRequest 项目运行中任务查询参数。
type ListRunningTasksByProjectRequest struct {
	Type       string `form:"type"`        // 可选：ai_slice / draft / ai_slice_draft
	ActiveOnly bool   `form:"active_only"` // true 时仅返回 processing
}

// RunningTasksData 项目运行中任务列表响应（无分页）。
type RunningTasksData struct {
	List  []TaskResponse `json:"list"`
	Total int64          `json:"total"`
}

// TaskCreateResponse 创建任务立即返回的摘要。
type TaskCreateResponse struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Progress  int16     `json:"progress"`
	CreatedAt time.Time `json:"created_at"`
}

func toTaskCreateResponse(task *model.Task) TaskCreateResponse {
	return TaskCreateResponse{
		ID:        task.ID,
		Type:      task.Type,
		Status:    task.Status,
		Progress:  task.Progress,
		CreatedAt: task.CreatedAt,
	}
}

// TaskResponse 任务详情/列表 API 响应。
// created_by 为创建人展示名（nickname 优先，否则 username），不是账号 ID。
// width/height/live_url/live_name 为创建任务时按 video_project 自动快照的冗余字段。
type TaskResponse struct {
	ID               string     `json:"id"`
	Type             string     `json:"type"`
	Status           string     `json:"status"`
	Progress         int16      `json:"progress"`
	Version          int64      `json:"version"`
	SysPrompt        string     `json:"sys_prompt,omitempty"`
	UsrPrompt        string     `json:"usr_prompt,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	VideoProjectID   *uint      `json:"video_project_id,omitempty"`
	VideoProjectName string     `json:"video_project_name"`
	LiveURL          string     `json:"live_url"`
	LiveName         string     `json:"live_name"`
	Width            int        `json:"width"`
	Height           int        `json:"height"`
	DraftURL         string     `json:"draft_url"`
	VideoURL         string     `json:"video_url"`
	ClipsTarURL      string     `json:"clips_tar_url"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Ext              string     `json:"ext"`
}

func (h *TaskHandler) toTaskResponse(ctx context.Context, task *model.Task) TaskResponse {
	return TaskResponse{
		ID:               task.ID,
		Type:             task.Type,
		Status:           task.Status,
		Progress:         task.Progress,
		Version:          task.Version,
		SysPrompt:        task.SysPrompt,
		UsrPrompt:        task.UsrPrompt,
		ErrorMessage:     task.ErrorMessage,
		VideoProjectID:   task.VideoProjectID,
		VideoProjectName: task.VideoProjectName,
		LiveURL:          task.LiveURL,
		LiveName:         task.LiveName,
		Width:            task.Width,
		Height:           task.Height,
		DraftURL:         task.DraftURL,
		VideoURL:         task.VideoURL,
		ClipsTarURL:      task.ClipsTarURL,
		CreatedBy:        h.createdBy.nameOf(ctx, task.CreatedBy),
		CreatedAt:        task.CreatedAt,
		UpdatedAt:        task.UpdatedAt,
		StartedAt:        task.StartedAt,
		CompletedAt:      task.CompletedAt,
		Ext:              task.Ext,
	}
}

func (h *TaskHandler) toTaskResponseList(ctx context.Context, items []model.TaskListItem) []TaskResponse {
	ids := make([]uint, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].CreatedBy)
	}
	names := h.createdBy.namesOf(ctx, uniqueAccountIDs(ids))
	out := make([]TaskResponse, 0, len(items))
	for i := range items {
		item := items[i]
		out = append(out, TaskResponse{
			ID:               item.ID,
			Type:             item.Type,
			Status:           item.Status,
			Progress:         item.Progress,
			Version:          item.Version,
			SysPrompt:        item.SysPrompt,
			UsrPrompt:        item.UsrPrompt,
			ErrorMessage:     item.ErrorMessage,
			VideoProjectID:   item.VideoProjectID,
			VideoProjectName: item.VideoProjectName,
			LiveURL:          item.LiveURL,
			LiveName:         item.LiveName,
			Width:            item.Width,
			Height:           item.Height,
			DraftURL:         item.DraftURL,
			VideoURL:         item.VideoURL,
			ClipsTarURL:      item.ClipsTarURL,
			CreatedBy:        names[item.CreatedBy],
			CreatedAt:        item.CreatedAt,
			UpdatedAt:        item.UpdatedAt,
			StartedAt:        item.StartedAt,
			CompletedAt:      item.CompletedAt,
			Ext:              item.Ext,
		})
	}
	return out
}

// CreateAISliceTask 创建 AI 切片任务
// @Summary      创建 AI 切片任务
// @Description  异步：按 video_project.prompt_id 加载系统提示词，用 clips0 时间段从 live_asr 筛选句段组装用户提示词，LLM 返回句段索引后回写 video_project.clips1；立即返回 task，请轮询 GET /v1/tasks/:id 查看 status/progress
// @Tags         异步任务
// @Accept       json
// @Produce      json
// @Param        body  body      CreateAISliceTaskRequest  true  "任务参数"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/tasks/ai-slice [post]
func (h *TaskHandler) CreateAISliceTask(c *gin.Context) {
	var req CreateAISliceTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	user, ok := middleware.GetAuthUser(c)
	if !ok {
		response.Unauthorized(c, "未登录")
		return
	}

	task, err := h.taskService.CreateAISlice(c.Request.Context(), user.ID, service.CreateAISliceInput{
		VideoProjectID: req.VideoProjectID,
	})
	if err != nil {
		writeTaskCreateError(c, err)
		return
	}
	response.Success(c, toTaskCreateResponse(task))
}

// CreateDraftTask 创建剪映草稿任务
// @Summary      创建剪映草稿任务
// @Description  异步：根据 video_project.id 读取 live_material.live_url 与 video_project.clips1，ffmpeg 精确裁剪后调用 capcut-mate create_draft（画布取请求 canvas_*，否则用 video_project.width/height，创建时写入 task.width/height/live_url）生成剪映草稿并回写 task.draft_url，成功后继续 gen_video 回写 task.video_url（视频失败仍保留 draft_url，任务标记 completed）；立即返回 task，请轮询 GET /v1/tasks/:id 查看 status/progress/draft_url/video_url
// @Tags         异步任务
// @Accept       json
// @Produce      json
// @Param        body  body      CreateDraftTaskRequest  true  "任务参数"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/tasks/draft [post]
func (h *TaskHandler) CreateDraftTask(c *gin.Context) {
	var req CreateDraftTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	user, ok := middleware.GetAuthUser(c)
	if !ok {
		response.Unauthorized(c, "未登录")
		return
	}

	task, err := h.taskService.CreateDraft(c.Request.Context(), user.ID, service.CreateDraftInput{
		VideoProjectID: req.VideoProjectID,
		CanvasWidth:    req.CanvasWidth,
		CanvasHeight:   req.CanvasHeight,
	})
	if err != nil {
		writeTaskCreateError(c, err)
		return
	}
	response.Success(c, toTaskCreateResponse(task))
}

// CreateAISliceDraftTask 创建一键成片任务
// @Summary      创建一键成片任务
// @Description  异步：等价于先执行 AI 切片（按 clips0 筛选 ASR、LLM 选索引写 clips1）再执行剪映草稿（create_draft 画布取请求 canvas_*，否则用 video_project.width/height，创建时写入 task.width/height/live_url）并回写 task.draft_url，成功后继续 gen_video 回写 task.video_url（视频失败仍保留 draft_url，任务标记 completed）；立即返回 task，请轮询 GET /v1/tasks/:id 查看 status/progress/draft_url/video_url
// @Tags         异步任务
// @Accept       json
// @Produce      json
// @Param        body  body      CreateAISliceDraftTaskRequest  true  "任务参数"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/tasks/ai-slice-draft [post]
func (h *TaskHandler) CreateAISliceDraftTask(c *gin.Context) {
	var req CreateAISliceDraftTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	user, ok := middleware.GetAuthUser(c)
	if !ok {
		response.Unauthorized(c, "未登录")
		return
	}

	task, err := h.taskService.CreateAISliceDraft(c.Request.Context(), user.ID, service.CreateAISliceDraftInput{
		VideoProjectID: req.VideoProjectID,
		CanvasWidth:    req.CanvasWidth,
		CanvasHeight:   req.CanvasHeight,
	})
	if err != nil {
		writeTaskCreateError(c, err)
		return
	}
	response.Success(c, toTaskCreateResponse(task))
}

// ListTasks 任务列表
// @Summary      任务列表
// @Description  分页查询异步任务，支持按 type、status、创建日期与关键词筛选；keywords 支持 ","=与、"|"=或，模糊匹配 task.video_project_name；列表项含 video_project_name、live_url、live_name、clips_tar_url、width/height（创建时按 video_project 自动快照）
// @Tags         异步任务
// @Produce      json
// @Param        type        query  string  false  "任务类型：ai_slice / draft / ai_slice_draft"
// @Param        status      query  string  false  "任务状态：pending / processing / completed / failed"
// @Param        start_date  query  string  false  "开始日期 YYYY-MM-DD，按 created_at 筛选"
// @Param        end_date    query  string  false  "结束日期 YYYY-MM-DD，按 created_at 筛选"
// @Param        keywords    query  string  false  "关键词：\",\"=与，\"|\"=或，模糊匹配 video_project_name；如 发布会,精剪|一键"
// @Param        page        query  int     false  "页码"
// @Param        page_size   query  int     false  "每页数量，默认 10"
// @Success      200         {object}  response.Body
// @Failure      400         {object}  response.Body
// @Failure      401         {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/tasks [get]
func (h *TaskHandler) ListTasks(c *gin.Context) {
	var req ListTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	page, pageSize := utils.DefaultPage(req.Page, req.PageSize)

	tasks, total, err := h.taskService.List(c.Request.Context(), page, pageSize, service.TaskListOptions{
		Type:      req.Type,
		Status:    req.Status,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Keywords:  req.Keywords,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     h.toTaskResponseList(c.Request.Context(), tasks),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// ListRunningTasksByProject 查询项目运行中的任务
// @Summary      查询项目运行中的任务
// @Description  返回指定剪辑项目下未结束的任务（默认 pending + processing）；active_only=true 时仅返回 processing；可选按 type 筛选；返回 task 全字段（created_by 为展示名）
// @Tags         异步任务
// @Produce      json
// @Param        id           path   int     true   "剪辑项目 ID"
// @Param        type         query  string  false  "任务类型：ai_slice / draft / ai_slice_draft"
// @Param        active_only  query  bool    false  "仅返回 processing 状态"
// @Success      200          {object}  response.Body
// @Failure      400          {object}  response.Body
// @Failure      401          {object}  response.Body
// @Failure      404          {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/video-projects/{id}/running-tasks [get]
func (h *TaskHandler) ListRunningTasksByProject(c *gin.Context) {
	projectID, err := parseUintParam(c, "id")
	if err != nil {
		response.BadRequest(c, "无效的项目 ID")
		return
	}
	var req ListRunningTasksByProjectRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	tasks, total, err := h.taskService.ListRunningByVideoProject(
		c.Request.Context(),
		projectID,
		req.Type,
		req.ActiveOnly,
	)
	if err != nil {
		if errors.Is(err, service.ErrVideoProjectNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		if errors.Is(err, service.ErrTaskInvalidType) {
			response.BadRequest(c, err.Error())
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, RunningTasksData{
		List:  h.toTaskResponseList(c.Request.Context(), tasks),
		Total: total,
	})
}

// GetTask 获取任务详情
// @Summary      获取任务详情
// @Description  根据 ID 查询任务；直接读取数据库中的 status、progress、draft_url、video_url、clips_tar_url、width/height/live_url/live_name 等字段，用于轮询异步任务进度；created_by 为创建人展示名
// @Tags         异步任务
// @Produce      json
// @Param        id   path  string  true  "任务 ID（UUID）"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Security     BearerAuth
// @Router       /v1/tasks/{id} [get]
func (h *TaskHandler) GetTask(c *gin.Context) {
	id, err := parseTaskIDParam(c)
	if err != nil {
		response.BadRequest(c, "无效的任务 ID")
		return
	}

	task, err := h.taskService.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrTaskNotFound) {
			response.NotFound(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, h.toTaskResponse(c.Request.Context(), task))
}

// parseTaskIDParam 解析路径中的任务 UUID。
func parseTaskIDParam(c *gin.Context) (string, error) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		return "", errors.New("empty task id")
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", err
	}
	return id, nil
}

func writeTaskCreateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrLiveMaterialNotFound),
		errors.Is(err, service.ErrVideoProjectNotFound),
		errors.Is(err, service.ErrLLMSystemPromptNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrTaskASRNotReady):
		response.BadRequest(c, err.Error())
	default:
		response.BadRequest(c, err.Error())
	}
}
