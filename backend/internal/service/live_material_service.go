// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// LiveMaterialListOptions 直播素材列表查询选项（来自 HTTP 查询参数）。
type LiveMaterialListOptions struct {
	StartDate   string
	EndDate     string
	Keywords    string
	ASRKeywords string
}

const liveMaterialListDateLayout = "2006-01-02"

// ErrUnsupportedMediaFormat 创建素材时不支持的音视频格式。
var ErrUnsupportedMediaFormat = errors.New("不支持的音视频格式，支持: mp3, wav, mp4, mov, ogg, raw")

// ErrLiveMaterialNameExists 素材名称已存在（唯一约束）。
var ErrLiveMaterialNameExists = errors.New("素材名称已存在")

// ErrLiveMaterialURLExists 直播链接已存在（唯一约束）。
var ErrLiveMaterialURLExists = errors.New("直播链接已存在")

// ErrLiveMaterialDuplicate 素材名称或直播链接已存在（无法区分约束时）。
var ErrLiveMaterialDuplicate = errors.New("素材名称或直播链接已存在")

// ErrLiveMaterialNotFound 直播素材不存在。
var ErrLiveMaterialNotFound = errors.New("直播素材不存在")

// CodeLiveMaterialExists 创建素材时记录已存在的业务错误码。
const CodeLiveMaterialExists = 40901

// LiveMaterialExistsError 创建时发现素材已存在；携带已有完整记录供接口返回。
type LiveMaterialExistsError struct {
	Material *model.LiveMaterial
	Cause    error
}

func (e *LiveMaterialExistsError) Error() string {
	if e == nil {
		return ErrLiveMaterialDuplicate.Error()
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return ErrLiveMaterialDuplicate.Error()
}

func (e *LiveMaterialExistsError) Unwrap() error {
	if e == nil || e.Cause == nil {
		return ErrLiveMaterialDuplicate
	}
	return e.Cause
}

// ErrASRAlreadyProcessing ASR 正在识别中，不允许重复提交。
var ErrASRAlreadyProcessing = errors.New("ASR 进行中，请勿重复提交")

// ErrASRRetryOnlyFailed 仅失败状态允许重试 ASR。
var ErrASRRetryOnlyFailed = errors.New("仅 ASR 失败状态可重试")

// ErrASRSubtitleNotReady ASR 未完成，无法导出字幕。
var ErrASRSubtitleNotReady = errors.New("ASR 未完成，无法导出字幕")

// ErrASRSubtitleEmpty ASR 字幕内容为空。
var ErrASRSubtitleEmpty = errors.New("ASR 字幕为空，无法导出")

// LiveMaterialService 直播素材业务接口。
type LiveMaterialService interface {
	Create(ctx context.Context, createdBy uint, in CreateLiveMaterialInput) (*model.LiveMaterial, error)
	Update(ctx context.Context, id uint, name, remark, m3u8URL string) (*model.LiveMaterial, error)
	List(ctx context.Context, page, pageSize int, opts LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error)
	Get(ctx context.Context, id uint) (*model.LiveMaterial, error)
	Delete(ctx context.Context, id uint) error
	RetryASR(ctx context.Context, id uint) (*model.LiveMaterial, error)
	RetryIngest(ctx context.Context, id uint, m3u8URL string) (*model.LiveMaterial, error)
	DownloadASRSubtitle(ctx context.Context, id uint) (content []byte, fileName string, err error)
}

type liveMaterialService struct {
	liveMaterialRepo repository.LiveMaterialRepository
	asrWorker        LiveMaterialASRWorker
	ingestWorker     LiveIngestWorker
	ingestRepo       repository.LiveIngestRepository
	allocator        LiveRecordURLAllocator
}

// NewLiveMaterialService 创建直播素材业务服务实例。
func NewLiveMaterialService(liveMaterialRepo repository.LiveMaterialRepository, asrWorker LiveMaterialASRWorker) LiveMaterialService {
	return NewLiveMaterialServiceFull(liveMaterialRepo, asrWorker, nil, nil, nil)
}

// NewLiveMaterialServiceFull 注入跟播 Worker 与对象存储预分配。
func NewLiveMaterialServiceFull(
	liveMaterialRepo repository.LiveMaterialRepository,
	asrWorker LiveMaterialASRWorker,
	ingestWorker LiveIngestWorker,
	ingestRepo repository.LiveIngestRepository,
	allocator LiveRecordURLAllocator,
) LiveMaterialService {
	return &liveMaterialService{
		liveMaterialRepo: liveMaterialRepo,
		asrWorker:        asrWorker,
		ingestWorker:     ingestWorker,
		ingestRepo:       ingestRepo,
		allocator:        allocator,
	}
}

func (s *liveMaterialService) Create(ctx context.Context, createdBy uint, in CreateLiveMaterialInput) (*model.LiveMaterial, error) {
	material, err := buildCreateMaterial(createdBy, in, s.allocator)
	if err != nil {
		return nil, err
	}
	if err := s.liveMaterialRepo.Create(ctx, material); err != nil {
		return nil, s.resolveCreateUniqueConflict(ctx, material.Name, material.LiveURL, material.M3U8URL, err)
	}
	if material.IsReplaySource() {
		if s.asrWorker != nil {
			s.asrWorker.Enqueue()
		}
	} else if s.ingestWorker != nil {
		s.ingestWorker.Enqueue()
	}
	return material, nil
}

func (s *liveMaterialService) Update(ctx context.Context, id uint, name, remark, m3u8URL string) (*model.LiveMaterial, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("素材名称不能为空")
	}

	material.Name = name
	material.Remark = remark
	if err := s.liveMaterialRepo.UpdateNameRemark(ctx, material); err != nil {
		return nil, mapLiveMaterialUniqueError(err)
	}

	m3u8URL = strings.TrimSpace(m3u8URL)
	if m3u8URL != "" && m3u8URL != material.M3U8URL {
		if !material.CanUpdateM3U8() {
			return nil, errors.New("当前状态不允许修改 m3u8 地址")
		}
		if !isValidHTTPURL(m3u8URL) || !media.IsM3U8URL(m3u8URL) {
			return nil, ErrUpcomingRequiresM3U8
		}
		if err := s.liveMaterialRepo.UpdateM3U8URL(ctx, id, m3u8URL); err != nil {
			return nil, mapLiveMaterialUniqueError(err)
		}
	}

	return s.liveMaterialRepo.GetByID(ctx, id)
}

// mapLiveMaterialUniqueError 将 name / live_url 唯一约束冲突转为业务错误。
func mapLiveMaterialUniqueError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unique") && !strings.Contains(msg, "duplicate") {
		return err
	}
	if strings.Contains(msg, "m3u8_url") {
		return ErrLiveMaterialURLExists
	}
	if strings.Contains(msg, "live_url") {
		return ErrLiveMaterialURLExists
	}
	if strings.Contains(msg, "name") {
		return ErrLiveMaterialNameExists
	}
	return ErrLiveMaterialDuplicate
}

// resolveCreateUniqueConflict 创建唯一冲突时查出已有记录，包装为 LiveMaterialExistsError。
func (s *liveMaterialService) resolveCreateUniqueConflict(ctx context.Context, name, liveURL, m3u8URL string, createErr error) error {
	mapped := mapLiveMaterialUniqueError(createErr)
	if !isLiveMaterialUniqueConflict(mapped) {
		return mapped
	}

	var (
		existing *model.LiveMaterial
		getErr   error
	)
	switch {
	case errors.Is(mapped, ErrLiveMaterialURLExists):
		if m3u8URL != "" {
			existing, getErr = s.liveMaterialRepo.GetByM3U8URL(ctx, m3u8URL)
		}
		if existing == nil {
			existing, getErr = s.liveMaterialRepo.GetByLiveURL(ctx, liveURL)
		}
	case errors.Is(mapped, ErrLiveMaterialNameExists):
		existing, getErr = s.liveMaterialRepo.GetByName(ctx, name)
	default:
		existing, getErr = s.liveMaterialRepo.GetByLiveURL(ctx, liveURL)
		if getErr != nil && m3u8URL != "" {
			existing, getErr = s.liveMaterialRepo.GetByM3U8URL(ctx, m3u8URL)
		}
		if getErr != nil {
			existing, getErr = s.liveMaterialRepo.GetByName(ctx, name)
		}
	}
	if getErr != nil || existing == nil {
		return mapped
	}
	return &LiveMaterialExistsError{Material: existing, Cause: mapped}
}

func isLiveMaterialUniqueConflict(err error) bool {
	return errors.Is(err, ErrLiveMaterialURLExists) ||
		errors.Is(err, ErrLiveMaterialNameExists) ||
		errors.Is(err, ErrLiveMaterialDuplicate)
}

func (s *liveMaterialService) List(ctx context.Context, page, pageSize int, opts LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error) {
	filter, err := buildLiveMaterialListFilter(opts)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	return s.liveMaterialRepo.List(ctx, filter, offset, pageSize)
}

// buildLiveMaterialListFilter 解析列表筛选参数并转换为仓储层筛选条件。
func buildLiveMaterialListFilter(opts LiveMaterialListOptions) (repository.LiveMaterialListFilter, error) {
	filter := repository.LiveMaterialListFilter{
		Keywords:    parseKeywordExpr(opts.Keywords),
		ASRKeywords: parseKeywordExpr(opts.ASRKeywords),
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

func (s *liveMaterialService) Get(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}
	return material, nil
}

func (s *liveMaterialService) Delete(ctx context.Context, id uint) error {
	if err := s.liveMaterialRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLiveMaterialNotFound
		}
		return err
	}
	return nil
}

func (s *liveMaterialService) RetryASR(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}

	switch material.ASRStatus {
	case model.ASRStatusFailed:
		// 仅失败可重试：改回 pending，由 Worker 扫库领取。
	case model.ASRStatusProcessing:
		return nil, ErrASRAlreadyProcessing
	default:
		return nil, ErrASRRetryOnlyFailed
	}

	if err := s.liveMaterialRepo.ResetASRToPending(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrASRRetryOnlyFailed
		}
		return nil, err
	}
	material, err = s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.asrWorker != nil {
		s.asrWorker.Enqueue()
	}
	return material, nil
}

func (s *liveMaterialService) RetryIngest(ctx context.Context, id uint, m3u8URL string) (*model.LiveMaterial, error) {
	if s.ingestRepo == nil {
		return nil, errors.New("跟播服务未配置")
	}
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLiveMaterialNotFound
		}
		return nil, err
	}
	if material.LiveStatus != model.LiveStatusFailed {
		return nil, ErrIngestRetryOnlyFailed
	}
	m3u8URL = strings.TrimSpace(m3u8URL)
	if err := s.ingestRepo.ResetFailedIngest(ctx, id, m3u8URL); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIngestRetryOnlyFailed
		}
		return nil, err
	}
	material, err = s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.ingestWorker != nil {
		s.ingestWorker.Enqueue()
	}
	return material, nil
}

func (s *liveMaterialService) DownloadASRSubtitle(ctx context.Context, id uint) ([]byte, string, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", ErrLiveMaterialNotFound
		}
		return nil, "", err
	}

	if material.ASRStatus != model.ASRStatusCompleted {
		return nil, "", ErrASRSubtitleNotReady
	}

	if len(material.ASRParagraphs) == 0 {
		return nil, "", ErrASRSubtitleEmpty
	}

	titles := make([]string, 0, len(material.ASRSummaries))
	for _, seg := range material.ASRSummaries {
		titles = append(titles, seg.Title)
	}

	paragraphs := make([]asr.SubtitleParagraph, 0, len(material.ASRParagraphs))
	for _, p := range material.ASRParagraphs {
		paragraphs = append(paragraphs, asr.SubtitleParagraph{
			Speaker:   p.Speaker,
			Text:      p.Text,
			StartTime: p.StartTime,
		})
	}

	content := asr.BuildSubtitleTXT(titles, paragraphs)
	fileName := fmt.Sprintf("asr_subtitle_%d.txt", material.ID)
	return []byte(content), fileName, nil
}
