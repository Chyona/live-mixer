// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	Type   string
	Status string
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

// CreateAISliceDraftInput 一键成片任务创建入参。
type CreateAISliceDraftInput struct {
	LiveID           uint
	Name             string
	Remark           string
	SysPromptID      uint
	UsrPrompt        string
	TargetDurationMs int64
	CanvasWidth      int
	CanvasHeight     int
}

// TaskExt 写入 task.ext 的结构化元数据。
type TaskExt struct {
	LiveID           uint              `json:"live_id,omitempty"`
	VideoProjectID   uint              `json:"video_project_id,omitempty"`
	Name             string            `json:"name,omitempty"`
	Remark           string            `json:"remark,omitempty"`
	SysPromptID      uint              `json:"sys_prompt_id,omitempty"`
	TargetDurationMs int64             `json:"target_duration_ms,omitempty"`
	CanvasWidth      int               `json:"canvas_width,omitempty"`
	CanvasHeight     int               `json:"canvas_height,omitempty"`
	Clips0           []model.ClipRange `json:"clips0,omitempty"`
}

// TaskService 异步任务业务接口。
type TaskService interface {
	CreateAISlice(ctx context.Context, createdBy uint, input CreateAISliceInput) (*model.Task, error)
	CreateDraft(ctx context.Context, createdBy uint, input CreateDraftInput) (*model.Task, error)
	CreateAISliceDraft(ctx context.Context, createdBy uint, input CreateAISliceDraftInput) (*model.Task, error)
	Get(ctx context.Context, id uint) (*model.Task, error)
	List(ctx context.Context, page, pageSize int, opts TaskListOptions) ([]model.Task, int64, error)
}

type taskService struct {
	taskRepo            repository.TaskRepository
	liveMaterialRepo    repository.LiveMaterialRepository
	videoProjectRepo    repository.VideoProjectRepository
	llmSystemPromptRepo repository.LLMSystemPromptRepository
	aiSliceWorker       AISliceWorker // 可选；为 nil 时仅落库不调度
	draftWorker         DraftWorker   // 可选；为 nil 时仅落库不调度
}

// NewTaskService 创建任务业务服务实例。
// aiSliceWorker / draftWorker 可为 nil（例如纯单测场景）；生产环境应注入并 Start。
func NewTaskService(
	taskRepo repository.TaskRepository,
	liveMaterialRepo repository.LiveMaterialRepository,
	videoProjectRepo repository.VideoProjectRepository,
	llmSystemPromptRepo repository.LLMSystemPromptRepository,
	aiSliceWorker AISliceWorker,
	draftWorker DraftWorker,
) TaskService {
	return &taskService{
		taskRepo:            taskRepo,
		liveMaterialRepo:    liveMaterialRepo,
		videoProjectRepo:    videoProjectRepo,
		llmSystemPromptRepo: llmSystemPromptRepo,
		aiSliceWorker:       aiSliceWorker,
		draftWorker:         draftWorker,
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
	if err := s.requireASRCompleted(ctx, project.LiveID); err != nil {
		return nil, err
	}

	ext, err := marshalTaskExt(TaskExt{
		LiveID:           project.LiveID,
		VideoProjectID:   project.ID,
		TargetDurationMs: 60000, // 默认目标成片时长 60 秒
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:      model.TaskTypeAISlice,
		Status:    model.TaskStatusPending,
		Progress:  0,
		CreatedBy: createdBy,
		Ext:       ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// 唤醒 AI 切片 Worker；多实例下由 DB 原子抢占领取，进度/状态写入数据库供轮询。
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
	clips, err := resolveDraftClipRanges(project)
	if err != nil {
		return nil, err
	}
	if err := validateClipRanges(clips); err != nil {
		return nil, err
	}

	if _, err := s.liveMaterialRepo.GetByID(ctx, project.LiveID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}

	width, height := input.CanvasWidth, input.CanvasHeight
	if width <= 0 {
		width = draftDefaultCanvasWidth
	}
	if height <= 0 {
		height = draftDefaultCanvasHeight
	}

	// clips 不写入 ext，由 Worker 从 video_project 读取，避免超出 1024 字节限制。
	ext, err := marshalTaskExt(TaskExt{
		LiveID:         project.LiveID,
		VideoProjectID: project.ID,
		CanvasWidth:    width,
		CanvasHeight:   height,
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:      model.TaskTypeDraft,
		Status:    model.TaskStatusPending,
		Progress:  0,
		CreatedBy: createdBy,
		Ext:       ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// 唤醒草稿 Worker；多实例下由 DB 原子抢占领取，进度/状态写入数据库供轮询。
	if s.draftWorker != nil {
		s.draftWorker.Enqueue()
	}
	return task, nil
}

func (s *taskService) CreateAISliceDraft(ctx context.Context, createdBy uint, input CreateAISliceDraftInput) (*model.Task, error) {
	if input.LiveID == 0 {
		return nil, errors.New("live_id 不能为空")
	}
	if err := s.requireASRCompleted(ctx, input.LiveID); err != nil {
		return nil, err
	}

	targetMs := input.TargetDurationMs
	if targetMs <= 0 {
		targetMs = 60000
	}
	width, height := input.CanvasWidth, input.CanvasHeight
	if width <= 0 {
		width = 1080
	}
	if height <= 0 {
		height = 1920
	}

	sysPrompt, err := s.resolveSysPrompt(ctx, input.SysPromptID)
	if err != nil {
		return nil, err
	}

	ext, err := marshalTaskExt(TaskExt{
		LiveID:           input.LiveID,
		Name:             strings.TrimSpace(input.Name),
		Remark:           strings.TrimSpace(input.Remark),
		SysPromptID:      input.SysPromptID,
		TargetDurationMs: targetMs,
		CanvasWidth:      width,
		CanvasHeight:     height,
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:      model.TaskTypeAISliceDraft,
		Status:    model.TaskStatusPending,
		Progress:  0,
		SysPrompt: sysPrompt,
		UsrPrompt: input.UsrPrompt,
		CreatedBy: createdBy,
		Ext:       ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// TODO: 入队 Worker，先 AI 切片再 ffmpeg + capcut-mate 生成草稿。
	return task, nil
}

func (s *taskService) Get(ctx context.Context, id uint) (*model.Task, error) {
	task, err := s.taskRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

func (s *taskService) List(ctx context.Context, page, pageSize int, opts TaskListOptions) ([]model.Task, int64, error) {
	if opts.Type != "" && !isValidTaskType(opts.Type) {
		return nil, 0, ErrTaskInvalidType
	}
	if opts.Status != "" && !isValidTaskStatus(opts.Status) {
		return nil, 0, errors.New("任务状态无效")
	}
	offset := (page - 1) * pageSize
	return s.taskRepo.List(ctx, repository.TaskListFilter{
		Type:   opts.Type,
		Status: opts.Status,
	}, offset, pageSize)
}

func (s *taskService) requireASRCompleted(ctx context.Context, liveID uint) error {
	material, err := s.liveMaterialRepo.GetByID(ctx, liveID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLiveMaterialNotFound
		}
		return err
	}
	if material.ASRStatus != model.ASRStatusCompleted {
		return ErrTaskASRNotReady
	}
	return nil
}

func (s *taskService) resolveSysPrompt(ctx context.Context, sysPromptID uint) (string, error) {
	if sysPromptID == 0 {
		return "", nil
	}
	prompt, err := s.llmSystemPromptRepo.GetByID(ctx, sysPromptID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", errors.New("系统提示词不存在")
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

func validateClipRanges(clips []model.ClipRange) error {
	for i, clip := range clips {
		if clip.StartTime < 0 || clip.EndTime <= clip.StartTime {
			return errors.New("clips0 时间段无效：start_time 须小于 end_time 且均非负")
		}
		_ = i
	}
	return nil
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
