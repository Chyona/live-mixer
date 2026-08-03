// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"live-mixer/internal/draft"
	"live-mixer/internal/draft/prepare"
	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// ErrTaskNotFound 任务不存在。
var ErrTaskNotFound = errors.New("任务不存在")

// ErrTaskASRNotReady 直播素材 ASR 尚未完成，无法创建依赖 ASR 的任务。
var ErrTaskASRNotReady = errors.New("直播素材 ASR 尚未完成")

// ErrTaskInvalidType 任务类型无效。
var ErrTaskInvalidType = errors.New("任务类型无效")

// TaskListOptions 任务列表查询选项。
type TaskListOptions struct {
	Type      string
	Status    string
	StartDate string   // YYYY-MM-DD，按 created_at 筛选
	EndDate   string   // YYYY-MM-DD，按 created_at 筛选
	Keywords  string // 关键词表达式："," 为与，"|" 为或；模糊匹配 task.video_project_name
}

// CreateAISliceInput AI 切片任务创建入参（仅需 video_project_id；直播 ASR 由 Worker 从关联 live_material 读取）。
type CreateAISliceInput struct {
	VideoProjectID uint
}

// CreateDraftInput 剪映草稿任务创建入参（仅需 video_project_id；切片与直播 URL 由 Worker 读取）。
type CreateDraftInput struct {
	VideoProjectID uint
	CanvasWidth    int
	CanvasHeight   int
}

// CreateAISliceDraftInput 一键成片任务创建入参（先 AI 切片再生成草稿；等价于 ai-slice + draft）。
type CreateAISliceDraftInput struct {
	VideoProjectID uint
	CanvasWidth    int
	CanvasHeight   int
}

// TaskExt 写入 task.ext 的结构化元数据。
// 画布尺寸与源视频链接/名称已冗余落在 task.width/height/live_url/live_name，不再写入 ext。
type TaskExt struct {
	LiveID           uint              `json:"live_id,omitempty"`
	VideoProjectID   uint              `json:"video_project_id,omitempty"`
	Name             string            `json:"name,omitempty"`
	Remark           string            `json:"remark,omitempty"`
	SysPromptID      uint              `json:"sys_prompt_id,omitempty"`
	TargetDurationMs int64             `json:"target_duration_ms,omitempty"`
	Clips0           []model.ClipRange `json:"clips0,omitempty"`
}

// runningTasksListLimit 单项目运行中任务查询上限，防止异常堆积时一次拉回过多行。
const runningTasksListLimit = 100

// TaskService 异步任务业务接口。
type TaskService interface {
	CreateAISlice(ctx context.Context, createdBy uint, input CreateAISliceInput) (*model.Task, error)
	CreateDraft(ctx context.Context, createdBy uint, input CreateDraftInput) (*model.Task, error)
	CreateAISliceDraft(ctx context.Context, createdBy uint, input CreateAISliceDraftInput) (*model.Task, error)
	Get(ctx context.Context, id string) (*model.Task, error)
	List(ctx context.Context, page, pageSize int, opts TaskListOptions) ([]model.TaskListItem, int64, error)
	// ListRunningByVideoProject 查询指定项目下未结束的任务（pending + processing；activeOnly 时仅 processing）。
	ListRunningByVideoProject(ctx context.Context, videoProjectID uint, taskType string, activeOnly bool) ([]model.TaskListItem, int64, error)
}

type taskService struct {
	taskRepo            repository.TaskRepository
	liveMaterialRepo    repository.LiveMaterialRepository
	videoProjectRepo    repository.VideoProjectRepository
	llmSystemPromptRepo repository.LLMSystemPromptRepository
	aiSliceWorker       AISliceWorker      // 可选；为 nil 时仅落库不调度
	draftWorker         DraftWorker        // 可选；为 nil 时仅落库不调度
	aiSliceDraftWorker  AISliceDraftWorker // 可选；为 nil 时仅落库不调度
}

// NewTaskService 创建任务业务服务实例。
// 各 Worker 可为 nil（例如纯单测场景）；生产环境应注入并 Start。
func NewTaskService(
	taskRepo repository.TaskRepository,
	liveMaterialRepo repository.LiveMaterialRepository,
	videoProjectRepo repository.VideoProjectRepository,
	llmSystemPromptRepo repository.LLMSystemPromptRepository,
	aiSliceWorker AISliceWorker,
	draftWorker DraftWorker,
	aiSliceDraftWorker AISliceDraftWorker,
) TaskService {
	return &taskService{
		taskRepo:            taskRepo,
		liveMaterialRepo:    liveMaterialRepo,
		videoProjectRepo:    videoProjectRepo,
		llmSystemPromptRepo: llmSystemPromptRepo,
		aiSliceWorker:       aiSliceWorker,
		draftWorker:         draftWorker,
		aiSliceDraftWorker:  aiSliceDraftWorker,
	}
}

func (s *taskService) CreateAISlice(ctx context.Context, createdBy uint, input CreateAISliceInput) (*model.Task, error) {
	// /ai-slice 仅接受 video_project_id：关联直播与 ASR 由 Worker 从库中读取。
	if input.VideoProjectID == 0 {
		return nil, errors.New("video_project_id 不能为空")
	}

	project, err := s.videoProjectRepo.GetByID(ctx, input.VideoProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}
	if project.LiveID == 0 {
		return nil, errors.New("video_project 未关联直播素材")
	}
	// clips0 为待分析时间窗口；为空则无法从 live_asr 筛选句段。
	if len(project.Clips0) == 0 {
		return nil, errors.New("video_project.clips0 不能为空，请先设置待分析时间段")
	}
	if err := prepare.ValidateClipRanges(project.Clips0); err != nil {
		return nil, err
	}
	// 校验 ASR 完成的同时取出素材，用于写入 task.live_url / live_name 快照。
	material, err := s.requireASRCompletedMaterial(ctx, project.LiveID)
	if err != nil {
		return nil, err
	}

	// 系统提示词必须来自 llm_system_prompt（按 video_project.prompt_id 查询）；查不到则任务直接失败。
	promptID := project.PromptID
	if promptID == 0 {
		promptID = model.DefaultVideoProjectPromptID
	}
	sysPrompt, err := s.resolveSysPrompt(ctx, promptID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sysPrompt) == "" {
		return nil, errors.New("系统提示词内容为空")
	}

	ext, err := marshalTaskExt(TaskExt{
		LiveID:         project.LiveID,
		VideoProjectID: project.ID,
		SysPromptID:    promptID,
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:             model.TaskTypeAISlice,
		Status:           model.TaskStatusPending,
		Progress:         0,
		Version:          0,
		SysPrompt:        sysPrompt,
		// usr_prompt 由 Worker 根据 clips0 + live_asr 组装内置模板后回写。
		VideoProjectID:   model.NewUintPtr(project.ID),
		VideoProjectName: project.Name,
		// 按 video_project 自动快照画布尺寸与源视频链接/名称（无外键）。
		Width:     project.Width,
		Height:    project.Height,
		LiveURL:   material.LiveURL,
		LiveName:  material.Name,
		CreatedBy: createdBy,
		Ext:       ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// 唤醒 AI 切片 Worker；多实例下由 DB 乐观锁抢占领取，进度/状态写入数据库供轮询。
	if s.aiSliceWorker != nil {
		s.aiSliceWorker.Enqueue()
	}
	return task, nil
}

func (s *taskService) CreateDraft(ctx context.Context, createdBy uint, input CreateDraftInput) (*model.Task, error) {
	// /draft 仅接受 video_project_id：直播 URL 与切片由 Worker 从关联表读取。
	if input.VideoProjectID == 0 {
		return nil, errors.New("video_project_id 不能为空")
	}

	project, err := s.videoProjectRepo.GetByID(ctx, input.VideoProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}

	// 创建前校验切片可用，避免无效任务进入队列。
	clips, err := prepare.ResolveClipRanges(project)
	if err != nil {
		return nil, err
	}
	if err := prepare.ValidateClipRanges(clips); err != nil {
		return nil, err
	}

	material, err := s.liveMaterialRepo.GetByID(ctx, project.LiveID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}

	// 解析最终画布尺寸并写入 task 冗余字段，供列表展示与 Worker 直接使用。
	width, height := draft.ResolveCanvasSize(input.CanvasWidth, input.CanvasHeight, project)

	// clips 不写入 ext，由 Worker 从 video_project 读取，避免超出 1024 字节限制。
	ext, err := marshalTaskExt(TaskExt{
		LiveID:         project.LiveID,
		VideoProjectID: project.ID,
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:             model.TaskTypeDraft,
		Status:           model.TaskStatusPending,
		Progress:         0,
		Version:          0,
		VideoProjectID:   model.NewUintPtr(project.ID),
		VideoProjectName: project.Name,
		Width:            width,
		Height:           height,
		LiveURL:          material.LiveURL,
		LiveName:         material.Name,
		CreatedBy:        createdBy,
		Ext:              ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// 唤醒草稿 Worker；多实例下由 DB 乐观锁抢占领取，进度/状态写入数据库供轮询。
	if s.draftWorker != nil {
		s.draftWorker.Enqueue()
	}
	return task, nil
}

func (s *taskService) CreateAISliceDraft(ctx context.Context, createdBy uint, input CreateAISliceDraftInput) (*model.Task, error) {
	// 一键成片 = AI 切片 + 草稿：创建校验与 ai-slice 对齐，画布参数与 draft 对齐。
	if input.VideoProjectID == 0 {
		return nil, errors.New("video_project_id 不能为空")
	}

	project, err := s.videoProjectRepo.GetByID(ctx, input.VideoProjectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}
	if project.LiveID == 0 {
		return nil, errors.New("video_project 未关联直播素材")
	}
	if len(project.Clips0) == 0 {
		return nil, errors.New("video_project.clips0 不能为空，请先设置待分析时间段")
	}
	if err := prepare.ValidateClipRanges(project.Clips0); err != nil {
		return nil, err
	}
	material, err := s.requireASRCompletedMaterial(ctx, project.LiveID)
	if err != nil {
		return nil, err
	}

	promptID := project.PromptID
	if promptID == 0 {
		promptID = model.DefaultVideoProjectPromptID
	}
	sysPrompt, err := s.resolveSysPrompt(ctx, promptID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sysPrompt) == "" {
		return nil, errors.New("系统提示词内容为空")
	}

	// 一键成片同样落库解析后的画布尺寸与源视频链接/名称快照。
	width, height := draft.ResolveCanvasSize(input.CanvasWidth, input.CanvasHeight, project)

	ext, err := marshalTaskExt(TaskExt{
		LiveID:         project.LiveID,
		VideoProjectID: project.ID,
		SysPromptID:    promptID,
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:             model.TaskTypeAISliceDraft,
		Status:           model.TaskStatusPending,
		Progress:         0,
		Version:          0,
		SysPrompt:        sysPrompt,
		VideoProjectID:   model.NewUintPtr(project.ID),
		VideoProjectName: project.Name,
		Width:            width,
		Height:           height,
		LiveURL:          material.LiveURL,
		LiveName:         material.Name,
		CreatedBy:        createdBy,
		Ext:              ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	if s.aiSliceDraftWorker != nil {
		s.aiSliceDraftWorker.Enqueue()
	}
	return task, nil
}

func (s *taskService) Get(ctx context.Context, id string) (*model.Task, error) {
	task, err := s.taskRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

func (s *taskService) List(ctx context.Context, page, pageSize int, opts TaskListOptions) ([]model.TaskListItem, int64, error) {
	if opts.Type != "" && !isValidTaskType(opts.Type) {
		return nil, 0, ErrTaskInvalidType
	}
	if opts.Status != "" && !isValidTaskStatus(opts.Status) {
		return nil, 0, errors.New("任务状态无效")
	}
	filter, err := buildTaskListFilter(opts)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	return s.taskRepo.List(ctx, filter, offset, pageSize)
}

func (s *taskService) ListRunningByVideoProject(ctx context.Context, videoProjectID uint, taskType string, activeOnly bool) ([]model.TaskListItem, int64, error) {
	if videoProjectID == 0 {
		return nil, 0, errors.New("video_project_id 不能为空")
	}
	if taskType != "" && !isValidTaskType(taskType) {
		return nil, 0, ErrTaskInvalidType
	}
	if _, err := s.videoProjectRepo.GetByID(ctx, videoProjectID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, ErrVideoProjectNotFound
		}
		return nil, 0, err
	}

	statuses := []string{model.TaskStatusPending, model.TaskStatusProcessing}
	if activeOnly {
		statuses = []string{model.TaskStatusProcessing}
	}
	projectID := videoProjectID
	filter := repository.TaskListFilter{
		Type:           taskType,
		Statuses:       statuses,
		VideoProjectID: &projectID,
	}
	return s.taskRepo.List(ctx, filter, 0, runningTasksListLimit)
}

// buildTaskListFilter 解析列表筛选参数并转换为仓储层筛选条件。
func buildTaskListFilter(opts TaskListOptions) (repository.TaskListFilter, error) {
	filter := repository.TaskListFilter{
		Type:     opts.Type,
		Status:   opts.Status,
		Keywords: parseKeywordExpr(opts.Keywords),
	}

	if raw := strings.TrimSpace(opts.StartDate); raw != "" {
		startAt, err := time.ParseInLocation(liveMaterialListDateLayout, raw, time.UTC)
		if err != nil {
			return filter, errors.New("start_date 格式无效，应为 YYYY-MM-DD")
		}
		filter.StartAt = &startAt
	}
	if raw := strings.TrimSpace(opts.EndDate); raw != "" {
		endDate, err := time.ParseInLocation(liveMaterialListDateLayout, raw, time.UTC)
		if err != nil {
			return filter, errors.New("end_date 格式无效，应为 YYYY-MM-DD")
		}
		endAt := endDate.Add(24 * time.Hour)
		filter.EndAt = &endAt
	}
	if filter.StartAt != nil && filter.EndAt != nil && !filter.StartAt.Before(*filter.EndAt) {
		return filter, errors.New("start_date 不能晚于 end_date")
	}
	return filter, nil
}

// requireASRCompletedMaterial 校验直播素材 ASR 已完成，并返回素材实体供创建任务时写入 live_url / live_name 快照。
func (s *taskService) requireASRCompletedMaterial(ctx context.Context, liveID uint) (*model.LiveMaterial, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, liveID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}
	if material.ASRStatus != model.ASRStatusCompleted {
		return nil, ErrTaskASRNotReady
	}
	return material, nil
}

func (s *taskService) resolveSysPrompt(ctx context.Context, sysPromptID uint) (string, error) {
	if sysPromptID == 0 {
		return "", nil
	}
	prompt, err := s.llmSystemPromptRepo.GetByID(ctx, sysPromptID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrLLMSystemPromptNotFound
		}
		return "", err
	}
	return prompt.Content, nil
}

func marshalTaskExt(ext TaskExt) (string, error) {
	raw, err := json.Marshal(ext)
	if err != nil {
		return "", err
	}
	if len(raw) > 1024 {
		return "", errors.New("任务扩展参数过长")
	}
	return string(raw), nil
}

func isValidTaskType(t string) bool {
	switch t {
	case model.TaskTypeAISlice, model.TaskTypeDraft, model.TaskTypeAISliceDraft:
		return true
	default:
		return false
	}
}

func isValidTaskStatus(s string) bool {
	switch s {
	case model.TaskStatusPending, model.TaskStatusProcessing, model.TaskStatusCompleted, model.TaskStatusFailed:
		return true
	default:
		return false
	}
}
