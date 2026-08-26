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
	"live-mixer/internal/pkg/asr"
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
	StartDate string // YYYY-MM-DD，按 created_at 筛选
	EndDate   string // YYYY-MM-DD，按 created_at 筛选
	Keywords  string // 关键词表达式："," 为与，"|" 为或；模糊匹配 task.video_project_name
}

// CreateAISliceInput AI 切片任务创建入参。
// 新路径：live_id + clips0，由后端创建项目并发布任务（按有效 ASR 约每 30 分钟拆成多个项目/任务）。
// 兼容路径：仅传 video_project_id，使用已有项目。
type CreateAISliceInput struct {
	VideoProjectID uint
	LiveID         uint
	PromptID       uint
	Clips0         []model.ClipRange
	ProjectSource  string
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
	LiveID         uint
	PromptID       uint
	Clips0         []model.ClipRange
	ProjectSource  string
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
	CreateAISlice(ctx context.Context, createdBy uint, input CreateAISliceInput) ([]*model.Task, error)
	CreateDraft(ctx context.Context, createdBy uint, input CreateDraftInput) (*model.Task, error)
	CreateAISliceDraft(ctx context.Context, createdBy uint, input CreateAISliceDraftInput) ([]*model.Task, error)
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

func (s *taskService) CreateAISlice(ctx context.Context, createdBy uint, input CreateAISliceInput) ([]*model.Task, error) {
	if input.LiveID != 0 {
		source := strings.TrimSpace(input.ProjectSource)
		if source == "" {
			source = "manual"
		}
		return s.createAISliceJobs(ctx, createdBy, createAISliceJobsInput{
			LiveID:        input.LiveID,
			PromptID:      input.PromptID,
			Clips0:        input.Clips0,
			ProjectSource: source,
			NamePrefix:    aiSliceProjectNamePrefix,
			TaskType:      model.TaskTypeAISlice,
		})
	}
	task, err := s.createAISliceFromExistingProject(ctx, createdBy, input.VideoProjectID, model.TaskTypeAISlice, 0, 0)
	if err != nil {
		return nil, err
	}
	if s.aiSliceWorker != nil {
		s.aiSliceWorker.Enqueue()
	}
	return []*model.Task{task}, nil
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

func (s *taskService) CreateAISliceDraft(ctx context.Context, createdBy uint, input CreateAISliceDraftInput) ([]*model.Task, error) {
	if input.LiveID != 0 {
		source := strings.TrimSpace(input.ProjectSource)
		if source == "" {
			source = "timeline"
		}
		return s.createAISliceJobs(ctx, createdBy, createAISliceJobsInput{
			LiveID:        input.LiveID,
			PromptID:      input.PromptID,
			Clips0:        input.Clips0,
			ProjectSource: source,
			NamePrefix:    aiSliceDraftProjectNamePrefix,
			TaskType:      model.TaskTypeAISliceDraft,
			CanvasWidth:   input.CanvasWidth,
			CanvasHeight:  input.CanvasHeight,
		})
	}
	task, err := s.createAISliceFromExistingProject(ctx, createdBy, input.VideoProjectID, model.TaskTypeAISliceDraft, input.CanvasWidth, input.CanvasHeight)
	if err != nil {
		return nil, err
	}
	if s.aiSliceDraftWorker != nil {
		s.aiSliceDraftWorker.Enqueue()
	}
	return []*model.Task{task}, nil
}

type createAISliceJobsInput struct {
	LiveID        uint
	PromptID      uint
	Clips0        []model.ClipRange
	ProjectSource string
	NamePrefix    string
	TaskType      string
	CanvasWidth   int
	CanvasHeight  int
}

func (s *taskService) createAISliceJobs(ctx context.Context, createdBy uint, in createAISliceJobsInput) ([]*model.Task, error) {
	if in.LiveID == 0 {
		return nil, errors.New("live_id 不能为空")
	}
	if len(in.Clips0) == 0 {
		return nil, errors.New("clips0 不能为空，请先设置待分析时间段")
	}
	if err := prepare.ValidateClipRanges(in.Clips0); err != nil {
		return nil, err
	}

	material, err := s.requireASRCompletedMaterial(ctx, in.LiveID)
	if err != nil {
		return nil, err
	}

	promptID := in.PromptID
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

	merged := sortAndMergeOverlappingClipRanges(in.Clips0)
	if len(merged) == 0 {
		return nil, errors.New("clips0 不能为空，请先设置待分析时间段")
	}
	utterances := asr.FormatUtterancesForAPI(material.LiveASR)
	groups := splitClips0IntoProjects(merged, utterances, material.ASRParagraphs)
	if len(groups) == 0 {
		return nil, errors.New("clips0 不能为空，请先设置待分析时间段")
	}

	width, height, err := resolveProjectCanvasSize(0, 0, material.Width, material.Height)
	if err != nil {
		return nil, err
	}

	names := autoSliceProjectNames(in.NamePrefix, len(groups), time.Now())
	tasks := make([]*model.Task, 0, len(groups))
	for i, clips := range groups {
		clips0, err := normalizeAndValidateClips0(clips)
		if err != nil {
			return nil, err
		}
		project := &model.VideoProject{
			Name:           names[i],
			LiveID:         in.LiveID,
			PromptID:       promptID,
			Clips0:         clips0,
			Clips1:         []model.ClipWithText{},
			Width:          width,
			Height:         height,
			ProjectSource:  in.ProjectSource,
			EnableCaptions: model.EnableCaptionsOn,
			CreatedBy:      createdBy,
		}
		if err := s.videoProjectRepo.Create(ctx, project); err != nil {
			return nil, mapVideoProjectUniqueError(err)
		}

		taskWidth, taskHeight := width, height
		if in.TaskType == model.TaskTypeAISliceDraft {
			taskWidth, taskHeight = draft.ResolveCanvasSize(in.CanvasWidth, in.CanvasHeight, project)
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
			Type:             in.TaskType,
			Status:           model.TaskStatusPending,
			Progress:         0,
			Version:          0,
			SysPrompt:        sysPrompt,
			VideoProjectID:   model.NewUintPtr(project.ID),
			VideoProjectName: project.Name,
			Width:            taskWidth,
			Height:           taskHeight,
			LiveURL:          material.LiveURL,
			LiveName:         material.Name,
			CreatedBy:        createdBy,
			Ext:              ext,
		}
		if err := s.taskRepo.Create(ctx, task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	switch in.TaskType {
	case model.TaskTypeAISlice:
		if s.aiSliceWorker != nil {
			s.aiSliceWorker.Enqueue()
		}
	case model.TaskTypeAISliceDraft:
		if s.aiSliceDraftWorker != nil {
			s.aiSliceDraftWorker.Enqueue()
		}
	}
	return tasks, nil
}

func (s *taskService) createAISliceFromExistingProject(ctx context.Context, createdBy uint, videoProjectID uint, taskType string, canvasWidth, canvasHeight int) (*model.Task, error) {
	if videoProjectID == 0 {
		return nil, errors.New("live_id 或 video_project_id 不能为空")
	}

	project, err := s.videoProjectRepo.GetByID(ctx, videoProjectID)
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

	width, height := project.Width, project.Height
	if taskType == model.TaskTypeAISliceDraft {
		width, height = draft.ResolveCanvasSize(canvasWidth, canvasHeight, project)
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
		Type:             taskType,
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
