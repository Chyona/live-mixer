// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"

	"live-mixer/internal/model"

	"gorm.io/gorm"
)

// LiveMaterialRepository 直播素材数据访问接口。
type LiveMaterialRepository interface {
	// Create 插入一条直播素材记录。
	Create(ctx context.Context, material *model.LiveMaterial) error
	// GetByID 根据主键查询直播素材。
	GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error)
	// UpdateNameRemark 仅更新素材名称与备注，防止误改其它字段。
	UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error
}

type liveMaterialRepository struct {
	db *gorm.DB
}

// NewLiveMaterialRepository 创建直播素材仓储实例。
func NewLiveMaterialRepository(db *gorm.DB) LiveMaterialRepository {
	return &liveMaterialRepository{db: db}
}

func (r *liveMaterialRepository) Create(ctx context.Context, material *model.LiveMaterial) error {
	return r.db.WithContext(ctx).Create(material).Error
}

func (r *liveMaterialRepository) GetByID(ctx context.Context, id uint) (*model.LiveMaterial, error) {
	var material model.LiveMaterial
	err := r.db.WithContext(ctx).First(&material, id).Error
	if err != nil {
		return nil, err
	}
	return &material, nil
}

func (r *liveMaterialRepository) UpdateNameRemark(ctx context.Context, material *model.LiveMaterial) error {
	// Select 限定更新列，确保 live_url、ASR 等字段不会被意外覆盖。
	return r.db.WithContext(ctx).
		Model(material).
		Select("name", "remark").
		Updates(material).Error
}
