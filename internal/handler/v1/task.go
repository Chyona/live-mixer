package v1

import (
	"errors"
	"time"

	"live-mixer/internal/middleware"
	"live-mixer/internal/model"
	"live-mixer/internal/service"
	"live-mixer/pkg/response"
	"live-mixer/pkg/utils"

	"github.com/gin-gonic/gin"
)

// TaskHandler 异步任务 HTTP 处理器。
type TaskHandler struct {
	taskService service.TaskService
}

// NewTaskHandler 创建任务处理器实例。
func NewTaskHandler(taskService service.TaskService) *TaskHandler {
	return &TaskHandler{taskService: taskService}
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

// CreateAISliceDraftTaskRequest 一键成片任务请求体。
type CreateAISliceDraftTaskRequest struct {
	LiveID           uint   `json:"live_id" binding:"required"`
	Name             string `json:"name" binding:"max=64"`
	Remark           string `json:"remark" binding:"max=256"`
	SysPromptID      uint   `json:"sys_prompt_id"`
	UsrPrompt        string `json:"usr_prompt"`
	TargetDurationMs int64  `json:"target_duration_ms"`
	CanvasWidth      int    `json:"canvas_width"`
	CanvasHeight     int    `json:"canvas_height"`
}

// ListTasksRequest 任务列表查询参数。
type ListTasksRequest struct {
	Type      string `form:"type"`
	Status    string `form:"status"`
	StartDate string `form:"start_date"` // 开始日期 YYYY-MM-DD，按 created_at 筛选
	EndDate   string `form:"end_date"`   // 结束日期 YYYY-MM-DD，按 created_at 筛选
	Page      int    `form:"page" binding:"omitempty,min=1"`
	PageSize  int    `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// TaskCreateResponse 创建任务立即返回的摘要。
type TaskCreateResponse struct {
	ID        uint      `json:"id"`
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

// CreateAISliceTask 创建 AI 切片任务
// @Summary      创建 AI 切片任务
// @Description  异步：根据 video_project.id 读取关联直播 ASR，由 LLM 选取高光片段并回写 video_project.clips0/clips1；立即返回 task，请轮询 GET /v1/tasks/:id 查看 status/progress
// @Tags         异步任务
// @Accept       json
// @Produce      json
// @Param        body  body      CreateAISliceTaskRequest  true  "任务参数"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
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
// @Description  异步：根据 video_project.id 读取 live_material.live_url 与 video_project.clips1，ffmpeg 精确裁剪后调用 capcut-mate 生成剪映草稿；立即返回 task，请轮询 GET /v1/tasks/:id 查看 status/progress
// @Tags         异步任务
// @Accept       json
// @Produce      json
// @Param        body  body      CreateDraftTaskRequest  true  "任务参数"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
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
// @Description  异步：先 AI 切片再 ffmpeg+capcut-mate 生成剪映草稿；立即返回 task，请轮询 GET /v1/tasks/:id
// @Tags         异步任务
// @Accept       json
// @Produce      json
// @Param        body  body      CreateAISliceDraftTaskRequest  true  "任务参数"
// @Success      200   {object}  response.Body
// @Failure      400   {object}  response.Body
// @Failure      401   {object}  response.Body
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
		LiveID:           req.LiveID,
		Name:             req.Name,
		Remark:           req.Remark,
		SysPromptID:      req.SysPromptID,
		UsrPrompt:        req.UsrPrompt,
		TargetDurationMs: req.TargetDurationMs,
		CanvasWidth:      req.CanvasWidth,
		CanvasHeight:     req.CanvasHeight,
	})
	if err != nil {
		writeTaskCreateError(c, err)
		return
	}
	response.Success(c, toTaskCreateResponse(task))
}

// ListTasks 任务列表
// @Summary      任务列表
// @Description  分页查询异步任务，支持按 type、status 与创建日期（start_date/end_date）筛选
// @Tags         异步任务
// @Produce      json
// @Param        type        query  string  false  "任务类型：ai_slice / draft / ai_slice_draft"
// @Param        status      query  string  false  "任务状态：pending / processing / completed / failed"
// @Param        start_date  query  string  false  "开始日期 YYYY-MM-DD，按 created_at 筛选"
// @Param        end_date    query  string  false  "结束日期 YYYY-MM-DD，按 created_at 筛选"
// @Param        page        query  int     false  "页码"
// @Param        page_size   query  int     false  "每页数量，默认 10"
// @Success      200         {object}  response.Body
// @Failure      400         {object}  response.Body
// @Failure      401         {object}  response.Body
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
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, response.PageData{
		List:     tasks,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// GetTask 获取任务详情
// @Summary      获取任务详情
// @Description  根据 ID 查询任务；直接读取数据库中的 status、progress 等字段，用于轮询异步任务进度
// @Tags         异步任务
// @Produce      json
// @Param        id   path  int  true  "任务 ID"
// @Success      200  {object}  response.Body
// @Failure      400  {object}  response.Body
// @Failure      404  {object}  response.Body
// @Router       /v1/tasks/{id} [get]
func (h *TaskHandler) GetTask(c *gin.Context) {
	id, err := parseUintParam(c, "id")
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
	response.Success(c, task)
}

func writeTaskCreateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrLiveMaterialNotFound),
		errors.Is(err, service.ErrVideoProjectNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, service.ErrTaskASRNotReady):
		response.BadRequest(c, err.Error())
	default:
		response.BadRequest(c, err.Error())
	}
}
