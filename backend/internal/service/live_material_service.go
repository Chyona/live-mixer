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
	// Create 创建直播素材，createdBy 来自 JWT 当前用户；分辨率由 ASR 预处理 ffprobe 回写。
	// url_type 由后台根据 live_url 自动识别（含 .m3u8 → m3u8，否则 file），不由客户端传入。
	Create(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error)
	// Update 更新直播素材，仅允许修改 name、remark。
	Update(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error)
	// List 分页查询直播素材列表，不含 live_asr 字段。
	List(ctx context.Context, page, pageSize int, opts LiveMaterialListOptions) ([]model.LiveMaterialListItem, int64, error)
	// Get 根据 ID 获取直播素材完整信息（含 live_asr）。
	Get(ctx context.Context, id uint) (*model.LiveMaterial, error)
	// Delete 删除直播素材，并级联删除关联剪辑项目。
	Delete(ctx context.Context, id uint) error
	// RetryASR 将失败的 ASR 重置为 pending，由后台 Worker 扫库重试。
	RetryASR(ctx context.Context, id uint) (*model.LiveMaterial, error)
	// DownloadASRSubtitle 返回可用于直接下载的 ASR 字幕 TXT 与建议文件名。
	DownloadASRSubtitle(ctx context.Context, id uint) (content []byte, fileName string, err error)
}

type liveMaterialService struct {
	liveMaterialRepo repository.LiveMaterialRepository
	asrWorker        LiveMaterialASRWorker
}

// NewLiveMaterialService 创建直播素材业务服务实例。
func NewLiveMaterialService(liveMaterialRepo repository.LiveMaterialRepository, asrWorker LiveMaterialASRWorker) LiveMaterialService {
	return &liveMaterialService{liveMaterialRepo: liveMaterialRepo, asrWorker: asrWorker}
}

func (s *liveMaterialService) Create(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error) {
	// 去除首尾空格，避免仅空白字符通过校验。
	name = strings.TrimSpace(name)
	liveURL = strings.TrimSpace(liveURL)
	if name == "" {
		return nil, errors.New("素材名称不能为空")
	}
	if liveURL == "" {
		return nil, errors.New("直播链接不能为空")
	}
	urlType := detectLiveURLType(liveURL)
	// 文件类链接仍校验 ASR 支持的后缀；m3u8 流跳过文件格式检测。
	if urlType == model.URLTypeFile {
		if _, err := asr.DetectFormat(liveURL); err != nil {
			if strings.Contains(err.Error(), "不支持的") {
				return nil, ErrUnsupportedMediaFormat
			}
			return nil, err
		}
	}

	material := &model.LiveMaterial{
		Name:          name,
		Remark:        remark,
		LiveURL:       liveURL,
		URLType:       urlType,
		// 创建时默认为非推流；后续由推流跟播逻辑写入 live/ending/ended。
		LiveStatus:    model.LiveStatusNone,
		Ext:           ext,
		LiveASR:       "{}",
		ASRSummaries:  []model.ASRSummarySegment{},
		ASRParagraphs: []model.ASRParagraph{},
		Duration:      0,
		// 分辨率由 ASR Worker 下载源媒体后 ffprobe 回写，创建时固定为 0。
		Width:       0,
		Height:      0,
		ASRStatus:   model.ASRStatusPending,
		ASRProgress: 0,
		ASRVersion:  0,
		CreatedBy:   createdBy,
	}
	if err := s.liveMaterialRepo.Create(ctx, material); err != nil {
		return nil, s.resolveCreateUniqueConflict(ctx, name, liveURL, err)
	}
	// 仅写库为 pending；唤醒 Worker 尽快扫库抢占（即使无唤醒，定时 poll 也会兜底）。
	if s.asrWorker != nil {
		s.asrWorker.Enqueue()
	}
	return material, nil
}

// detectLiveURLType 根据 live_url 识别类型：路径含 .m3u8 视为 HLS，否则为文件。
func detectLiveURLType(liveURL string) string {
	if strings.Contains(strings.ToLower(liveURL), ".m3u8") {
		return model.URLTypeM3U8
	}
	return model.URLTypeFile
}

func (s *liveMaterialService) Update(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error) {
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

	// 仅修改允许编辑的字段，其它字段保持数据库原值。
	material.Name = name
	material.Remark = remark

	if err := s.liveMaterialRepo.UpdateNameRemark(ctx, material); err != nil {
		return nil, mapLiveMaterialUniqueError(err)
	}
	return material, nil
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
	if strings.Contains(msg, "live_url") {
		return ErrLiveMaterialURLExists
	}
	if strings.Contains(msg, "name") {
		return ErrLiveMaterialNameExists
	}
	return ErrLiveMaterialDuplicate
}

// resolveCreateUniqueConflict 创建唯一冲突时查出已有记录，包装为 LiveMaterialExistsError。
func (s *liveMaterialService) resolveCreateUniqueConflict(ctx context.Context, name, liveURL string, createErr error) error {
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
		existing, getErr = s.liveMaterialRepo.GetByLiveURL(ctx, liveURL)
	case errors.Is(mapped, ErrLiveMaterialNameExists):
		existing, getErr = s.liveMaterialRepo.GetByName(ctx, name)
	default:
		existing, getErr = s.liveMaterialRepo.GetByLiveURL(ctx, liveURL)
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
