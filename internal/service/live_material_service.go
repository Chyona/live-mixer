// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"strings"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// LiveMaterialService 直播素材业务接口。
type LiveMaterialService interface {
	// Create 创建直播素材，createdBy 来自 JWT 当前用户。
	Create(ctx context.Context, createdBy uint, name, liveURL, remark, ext string) (*model.LiveMaterial, error)
	// Update 更新直播素材，仅允许修改 name、remark。
	Update(ctx context.Context, id uint, name, remark string) (*model.LiveMaterial, error)
	// List 分页查询直播素材列表，返回全部字段。
	List(ctx context.Context, page, pageSize int) ([]model.LiveMaterial, int64, error)
}

type liveMaterialService struct {
	liveMaterialRepo repository.LiveMaterialRepository
}

// NewLiveMaterialService 创建直播素材业务服务实例。
func NewLiveMaterialService(liveMaterialRepo repository.LiveMaterialRepository) LiveMaterialService {
	return &liveMaterialService{liveMaterialRepo: liveMaterialRepo}
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

func (s *liveMaterialService) List(ctx context.Context, page, pageSize int) ([]model.LiveMaterial, int64, error) {
	offset := (page - 1) * pageSize
	return s.liveMaterialRepo.List(ctx, offset, pageSize)
}
