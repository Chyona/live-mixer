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

// CreateAISliceInput AI 切片任务创建入参。
type CreateAISliceInput struct {
	LiveID            uint
	Name              string
	Remark            string
	SysPromptID       uint
	UsrPrompt         string
	TargetDurationMs  int64
}

// CreateDraftInput 剪映草稿任务创建入参。
type CreateDraftInput struct {
	LiveID          uint
	VideoProjectID  uint
	Name            string
	Clips0          []model.ClipRange
	CanvasWidth     int
	CanvasHeight    int
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
	taskRepo             repository.TaskRepository
	liveMaterialRepo     repository.LiveMaterialRepository
	videoProjectRepo     repository.VideoProjectRepository
	llmSystemPromptRepo  repository.LLMSystemPromptRepository
}

// NewTaskService 创建任务业务服务实例。
func NewTaskService(
	taskRepo repository.TaskRepository,
	liveMaterialRepo repository.LiveMaterialRepository,
	videoProjectRepo repository.VideoProjectRepository,
	llmSystemPromptRepo repository.LLMSystemPromptRepository,
) TaskService {
	return &taskService{
		taskRepo:            taskRepo,
		liveMaterialRepo:    liveMaterialRepo,
		videoProjectRepo:    videoProjectRepo,
		llmSystemPromptRepo: llmSystemPromptRepo,
	}
}

func (s *taskService) CreateAISlice(ctx context.Context, createdBy uint, input CreateAISliceInput) (*model.Task, error) {
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
	})
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:      model.TaskTypeAISlice,
		Status:    model.TaskStatusPending,
		SysPrompt: sysPrompt,
		UsrPrompt: input.UsrPrompt,
		CreatedBy: createdBy,
		Ext:       ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// TODO: 入队 Worker，调用 LLM 分析 ASR 并写入 video_project.clips0/clips1。
	return task, nil
}

func (s *taskService) CreateDraft(ctx context.Context, createdBy uint, input CreateDraftInput) (*model.Task, error) {
	liveID := input.LiveID
	clips0 := input.Clips0

	if input.VideoProjectID != 0 {
		project, err := s.videoProjectRepo.GetByID(ctx, input.VideoProjectID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrVideoProjectNotFound
			}
			return nil, err
		}
		if liveID == 0 {
			liveID = project.LiveID
		}
		if len(clips0) == 0 {
			var parsed []model.ClipRange
			if err := json.Unmarshal([]byte(project.Clips0), &parsed); err != nil {
				return nil, errors.New("video_project.clips0 格式无效")
			}
			clips0 = parsed
		}
	}

	if liveID == 0 {
		return nil, errors.New("live_id 不能为空（或提供有效的 video_project_id）")
	}
	if len(clips0) == 0 {
		return nil, errors.New("clips0 不能为空（或提供含 clips0 的 video_project_id）")
	}
	if err := validateClipRanges(clips0); err != nil {
		return nil, err
	}
	if _, err := s.liveMaterialRepo.GetByID(ctx, liveID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}

	width, height := input.CanvasWidth, input.CanvasHeight
	if width <= 0 {
		width = 1080
	}
	if height <= 0 {
		height = 1920
	}

	extPayload := TaskExt{
		LiveID:         liveID,
		VideoProjectID: input.VideoProjectID,
		Name:           strings.TrimSpace(input.Name),
		CanvasWidth:    width,
		CanvasHeight:   height,
	}
	// 无项目 ID 时需在 ext 中保留 clips0；已有项目则后续由 Worker 读取，避免超出 ext 长度。
	if input.VideoProjectID == 0 {
		extPayload.Clips0 = clips0
	} else if len(input.Clips0) > 0 {
		extPayload.Clips0 = input.Clips0
	}

	ext, err := marshalTaskExt(extPayload)
	if err != nil {
		return nil, err
	}

	task := &model.Task{
		Type:      model.TaskTypeDraft,
		Status:    model.TaskStatusPending,
		CreatedBy: createdBy,
		Ext:       ext,
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}
	// TODO: 入队 Worker，ffmpeg 切片并调用 capcut-mate 生成剪映草稿。
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
