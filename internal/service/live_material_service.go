// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"strings"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// ErrUnsupportedMediaFormat 创建素材时不支持的音视频格式。
var ErrUnsupportedMediaFormat = errors.New("不支持的音视频格式，支持: mp3, wav, mp4, ogg, raw")

// LiveMaterialService 直播素材业务接口。
type LiveMaterialService interface {
	// Create 创建直播素材，createdBy 来自 JWT 当前用户。
	Create(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error)
	// Update 更新直播素材，仅允许修改 name、remark。
	Update(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error)
	// List 分页查询直播素材列表，不含 live_asr 字段。
	List(ctx context.Context, page, pageSize int) ([]model.LiveMaterialListItem, int64, error)
	// Get 根据 ID 获取直播素材完整信息（含 live_asr）。
	Get(ctx context.Context, id uint) (*model.LiveMaterial, error)
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
	if _, err := asr.DetectFormat(liveURL); err != nil {
		if strings.Contains(err.Error(), "不支持的") {
			return nil, ErrUnsupportedMediaFormat
		}
		return nil, err
	}

	material := &model.LiveMaterial{
		Name:        name,
		Remark:      remark,
		LiveURL:     liveURL,
		Ext:         ext,
		LiveASR:     "{}",
		Duration:    0,
		ASRStatus:   model.ASRStatusPending,
		ASRProgress: 0,
		CreatedBy:   createdBy,
	}
	if err := s.liveMaterialRepo.Create(ctx, material); err != nil {
		return nil, err
	}
	if s.asrWorker != nil {
		s.asrWorker.Enqueue(material.ID)
	}
	return material, nil
}

func (s *liveMaterialService) Update(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("直播素材不存在")
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
		return nil, err
	}
	return material, nil
}

func (s *liveMaterialService) List(ctx context.Context, page, pageSize int) ([]model.LiveMaterialListItem, int64, error) {
	offset := (page - 1) * pageSize
	return s.liveMaterialRepo.List(ctx, offset, pageSize)
}

func (s *liveMaterialService) Get(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	material, err := s.liveMaterialRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("直播素材不存在")
		}
		return nil, err
	}
	return material, nil
}
