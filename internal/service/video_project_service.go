// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ErrVideoProjectNotFound 剪辑项目不存在。
var ErrVideoProjectNotFound = errors.New("剪辑项目不存在")

// ErrVideoProjectNameExists 项目名称已存在（唯一约束）。
var ErrVideoProjectNameExists = errors.New("项目名称已存在")

// ErrLiveMaterialNotFoundForProject 创建剪辑项目时关联的直播素材不存在。
var ErrLiveMaterialNotFoundForProject = errors.New("关联的直播素材不存在")

// 剪辑项目画布仅支持两档分辨率。
const (
	projectCanvasLandscapeW = 1920
	projectCanvasLandscapeH = 1080
	projectCanvasPortraitW  = 1080
	projectCanvasPortraitH  = 1920
)

// VideoProjectListOptions 剪辑项目列表查询选项（来自 HTTP 查询参数）。
type VideoProjectListOptions struct {
	Keywords  string
	StartDate string
	EndDate   string
}

// CreateVideoProjectInput 创建剪辑项目入参。
// Clips0 / Clips1 为可选：nil 或空切片均写入 JSON 空数组 []。
// ProjectSource 可选，未传时为空字符串。
// Width / Height 可选：未传（均为 0）时按关联素材分辨率在 1920×1080 / 1080×1920 中自动选档。
// EnableCaptions 可选：nil 时默认 1（添加字幕）；非 nil 时须为 0 或 1。
type CreateVideoProjectInput struct {
	Name           string
	Remark         string
	LiveID         uint
	PromptID       uint
	Clips0         []model.ClipRange
	Clips1         []model.ClipWithText
	Width          int
	Height         int
	ProjectSource  string
	EnableCaptions *int
}

// VideoProjectUpdateInput 剪辑项目更新入参。
// 指针字段为 nil 表示「请求未传该字段，保持原值」；非 nil 则校验通过后写入。
type VideoProjectUpdateInput struct {
	Name           *string
	Remark         *string
	PromptID       *uint
	Clips0         *[]model.ClipRange
	Clips1         *[]model.ClipWithText
	Width          *int
	Height         *int
	ProjectSource  *string
	EnableCaptions *int
}

// VideoProjectService 剪辑项目业务接口。
type VideoProjectService interface {
	// Create 创建剪辑项目，createdBy 来自 JWT 当前用户；promptID 为 0 时使用默认值 1。
	Create(ctx context.Context, createdBy uint, input CreateVideoProjectInput) (*model.VideoProject, error)
	// Update 更新剪辑项目可编辑字段（仅更新请求中显式传入且合法的字段）。
	Update(ctx context.Context, id uint, input VideoProjectUpdateInput) (*model.VideoProject, error)
	// Delete 删除剪辑项目。
	Delete(ctx context.Context, id uint) error
	// List 分页查询剪辑项目列表（含关联直播素材名称 live_name）。
	List(ctx context.Context, page, pageSize int, opts VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error)
	// ListByLiveMaterial 分页查询指定直播素材关联的剪辑项目；素材不存在时返回 ErrLiveMaterialNotFound。
	ListByLiveMaterial(ctx context.Context, liveID uint, page, pageSize int) ([]model.VideoProjectListItem, int64, error)
	// Get 根据 ID 获取剪辑项目详情。
	Get(ctx context.Context, id uint) (*model.VideoProject, error)
}

type videoProjectService struct {
	videoProjectRepo repository.VideoProjectRepository
	liveMaterialRepo repository.LiveMaterialRepository
	logger           *zap.Logger
}

// NewVideoProjectService 创建剪辑项目业务服务实例。
func NewVideoProjectService(videoProjectRepo repository.VideoProjectRepository, liveMaterialRepo repository.LiveMaterialRepository) VideoProjectService {
	return NewVideoProjectServiceWithLogger(videoProjectRepo, liveMaterialRepo, nil)
}

// NewVideoProjectServiceWithLogger 创建带日志的剪辑项目业务服务；logger 为 nil 时使用 Nop。
func NewVideoProjectServiceWithLogger(
	videoProjectRepo repository.VideoProjectRepository,
	liveMaterialRepo repository.LiveMaterialRepository,
	logger *zap.Logger,
) VideoProjectService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &videoProjectService{
		videoProjectRepo: videoProjectRepo,
		liveMaterialRepo: liveMaterialRepo,
		logger:           logger,
	}
}

func (s *videoProjectService) Create(ctx context.Context, createdBy uint, input CreateVideoProjectInput) (*model.VideoProject, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("项目名称不能为空")
	}
	if input.LiveID == 0 {
		return nil, errors.New("直播素材 ID 不能为空")
	}
	material, err := s.liveMaterialRepo.GetByID(ctx, input.LiveID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFoundForProject
		}
		return nil, err
	}

	width, height, err := resolveProjectCanvasSize(input.Width, input.Height, material.Width, material.Height)
	if err != nil {
		return nil, err
	}
	if input.Width == 0 && input.Height == 0 && (material.Width <= 0 || material.Height <= 0) {
		s.logger.Info("剪辑项目画布回退为默认竖屏：素材分辨率未知",
			zap.Uint("live_id", material.ID),
			zap.Int("material_width", material.Width),
			zap.Int("material_height", material.Height),
			zap.Int("project_width", width),
			zap.Int("project_height", height),
		)
	} else {
		s.logger.Info("剪辑项目画布尺寸已确定",
			zap.Uint("live_id", material.ID),
			zap.Int("material_width", material.Width),
			zap.Int("material_height", material.Height),
			zap.Int("req_width", input.Width),
			zap.Int("req_height", input.Height),
			zap.Int("project_width", width),
			zap.Int("project_height", height),
		)
	}

	promptID := input.PromptID
	if promptID == 0 {
		promptID = model.DefaultVideoProjectPromptID
	}

	clips0, err := normalizeAndValidateClips0(input.Clips0)
	if err != nil {
		return nil, err
	}
	clips1, err := normalizeAndValidateClips1(input.Clips1)
	if err != nil {
		return nil, err
	}

	enableCaptions := model.EnableCaptionsOn
	if input.EnableCaptions != nil {
		if err := validateEnableCaptions(*input.EnableCaptions); err != nil {
			return nil, err
		}
		enableCaptions = *input.EnableCaptions
	}

	project := &model.VideoProject{
		Name:           name,
		Remark:         input.Remark,
		LiveID:         input.LiveID,
		PromptID:       promptID,
		Clips0:         clips0,
		Clips1:         clips1,
		Width:          width,
		Height:         height,
		ProjectSource:  strings.TrimSpace(input.ProjectSource),
		EnableCaptions: enableCaptions,
		CreatedBy:      createdBy,
	}
	if err := s.videoProjectRepo.Create(ctx, project); err != nil {
		return nil, mapVideoProjectUniqueError(err)
	}
	return project, nil
}

func (s *videoProjectService) Update(ctx context.Context, id uint, input VideoProjectUpdateInput) (*model.VideoProject, error) {
	project, err := s.videoProjectRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}

	// 以下各字段均为「传了才更新」：指针 nil 表示请求未携带该字段。
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, errors.New("项目名称不能为空")
		}
		project.Name = name
	}
	if input.Remark != nil {
		project.Remark = *input.Remark
	}
	if input.PromptID != nil {
		promptID := *input.PromptID
		if promptID == 0 {
			promptID = model.DefaultVideoProjectPromptID
		}
		project.PromptID = promptID
	}
	if input.Clips0 != nil {
		clips0, err := normalizeAndValidateClips0(*input.Clips0)
		if err != nil {
			return nil, err
		}
		project.Clips0 = clips0
	}
	if input.Clips1 != nil {
		clips1, err := normalizeAndValidateClips1(*input.Clips1)
		if err != nil {
			return nil, err
		}
		project.Clips1 = clips1
	}
	// 宽高须成对更新，且只能是支持的两档画布之一。
	if input.Width != nil || input.Height != nil {
		if input.Width == nil || input.Height == nil {
			return nil, errors.New("width/height 须成对传入")
		}
		if err := validateProjectCanvasPair(*input.Width, *input.Height); err != nil {
			return nil, err
		}
		project.Width = *input.Width
		project.Height = *input.Height
	}
	if input.ProjectSource != nil {
		project.ProjectSource = strings.TrimSpace(*input.ProjectSource)
	}
	if input.EnableCaptions != nil {
		if err := validateEnableCaptions(*input.EnableCaptions); err != nil {
			return nil, err
		}
		project.EnableCaptions = *input.EnableCaptions
	}

	if err := s.videoProjectRepo.Update(ctx, project); err != nil {
		return nil, mapVideoProjectUniqueError(err)
	}
	return project, nil
}

// mapVideoProjectUniqueError 将 name 唯一约束冲突转为业务错误。
func mapVideoProjectUniqueError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if (strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")) &&
		strings.Contains(msg, "name") {
		return ErrVideoProjectNameExists
	}
	return err
}

func (s *videoProjectService) Delete(ctx context.Context, id uint) error {
	if err := s.videoProjectRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrVideoProjectNotFound
		}
		return err
	}
	return nil
}

func (s *videoProjectService) List(ctx context.Context, page, pageSize int, opts VideoProjectListOptions) ([]model.VideoProjectListItem, int64, error) {
	filter, err := buildVideoProjectListFilter(opts)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	return s.videoProjectRepo.List(ctx, filter, offset, pageSize)
}

func (s *videoProjectService) ListByLiveMaterial(ctx context.Context, liveID uint, page, pageSize int) ([]model.VideoProjectListItem, int64, error) {
	if liveID == 0 {
		return nil, 0, errors.New("直播素材 ID 不能为空")
	}
	if _, err := s.liveMaterialRepo.GetByID(ctx, liveID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, ErrLiveMaterialNotFound
		}
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	return s.videoProjectRepo.List(ctx, repository.VideoProjectListFilter{LiveID: &liveID}, offset, pageSize)
}

func (s *videoProjectService) Get(ctx context.Context, id uint) (*model.VideoProject, error) {
	project, err := s.videoProjectRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVideoProjectNotFound
		}
		return nil, err
	}
	return project, nil
}

// resolveProjectCanvasSize 解析创建项目时的画布尺寸：
// - 均为 0：按素材宽高比在 1920×1080 / 1080×1920 中选更接近的一档；素材未知时默认竖屏；
// - 仅一侧非 0：报错；
// - 均大于 0：必须恰好为支持的两档之一。
func resolveProjectCanvasSize(reqW, reqH, materialW, materialH int) (int, int, error) {
	if reqW < 0 || reqH < 0 {
		return 0, 0, errors.New("width/height 不能为负数")
	}
	if (reqW == 0) != (reqH == 0) {
		return 0, 0, errors.New("width/height 须成对传入，或不传以按素材自动推断")
	}
	if reqW > 0 && reqH > 0 {
		if err := validateProjectCanvasPair(reqW, reqH); err != nil {
			return 0, 0, err
		}
		return reqW, reqH, nil
	}
	w, h := pickProjectCanvasByMaterial(materialW, materialH)
	return w, h, nil
}

// validateProjectCanvasPair 校验宽高是否为支持的两档之一。
func validateProjectCanvasPair(width, height int) error {
	if (width == projectCanvasLandscapeW && height == projectCanvasLandscapeH) ||
		(width == projectCanvasPortraitW && height == projectCanvasPortraitH) {
		return nil
	}
	return fmt.Errorf("width/height 仅支持 %dx%d 或 %dx%d",
		projectCanvasLandscapeW, projectCanvasLandscapeH,
		projectCanvasPortraitW, projectCanvasPortraitH,
	)
}

// validateEnableCaptions 校验是否添加字幕开关（仅允许 0/1）。
func validateEnableCaptions(v int) error {
	if v != model.EnableCaptionsOff && v != model.EnableCaptionsOn {
		return errors.New("enable_captions 仅支持 0 或 1")
	}
	return nil
}

// pickProjectCanvasByMaterial 按素材宽高比选择更接近的画布档；素材无效时默认竖屏。
func pickProjectCanvasByMaterial(materialW, materialH int) (int, int) {
	if materialW <= 0 || materialH <= 0 {
		return projectCanvasPortraitW, projectCanvasPortraitH
	}
	r := float64(materialW) / float64(materialH)
	landscapeRatio := float64(projectCanvasLandscapeW) / float64(projectCanvasLandscapeH) // 16/9
	portraitRatio := float64(projectCanvasPortraitW) / float64(projectCanvasPortraitH)   // 9/16
	dLandscape := math.Abs(r - landscapeRatio)
	dPortrait := math.Abs(r - portraitRatio)
	// 并列时选用竖屏（与草稿默认画布一致）。
	if dPortrait <= dLandscape {
		return projectCanvasPortraitW, projectCanvasPortraitH
	}
	return projectCanvasLandscapeW, projectCanvasLandscapeH
}

// buildVideoProjectListFilter 解析列表筛选参数并转换为仓储层筛选条件。
func buildVideoProjectListFilter(opts VideoProjectListOptions) (repository.VideoProjectListFilter, error) {
	filter := repository.VideoProjectListFilter{
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

// normalizeAndValidateClips0 校验并规范化 clips0（nil 转为空切片）。
func normalizeAndValidateClips0(clips []model.ClipRange) ([]model.ClipRange, error) {
	if clips == nil {
		clips = []model.ClipRange{}
	}
	for i, clip := range clips {
		if clip.StartTime < 0 || clip.EndTime <= clip.StartTime {
			return nil, fmt.Errorf("clips0[%d] 时间段无效：start_time 须小于 end_time 且均非负", i)
		}
	}
	return clips, nil
}

// normalizeAndValidateClips1 校验并规范化 clips1（nil 转为空切片）。
func normalizeAndValidateClips1(clips []model.ClipWithText) ([]model.ClipWithText, error) {
	if clips == nil {
		clips = []model.ClipWithText{}
	}
	for i, clip := range clips {
		if clip.StartTime < 0 || clip.EndTime <= clip.StartTime {
			return nil, fmt.Errorf("clips1[%d] 时间段无效：start_time 须小于 end_time 且均非负", i)
		}
	}
	return clips, nil
}
