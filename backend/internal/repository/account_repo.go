// Package repository 数据访问层，封装 GORM 数据库操作。
package repository

import (
	"context"

	"live-mixer/internal/model"
	"gorm.io/gorm"
)

// AccountRepository 账号数据访问接口。
type AccountRepository interface {
	Create(ctx context.Context, account *model.Account) error
	GetByID(ctx context.Context, id uint) (*model.Account, error)
	GetByUsername(ctx context.Context, username string) (*model.Account, error)
	// ListByIDs 按主键批量查询账号，返回 id → Account 映射（缺失的 id 不会出现在 map 中）。
	ListByIDs(ctx context.Context, ids []uint) (map[uint]*model.Account, error)
	List(ctx context.Context, offset, limit int) ([]model.Account, int64, error)
	Update(ctx context.Context, account *model.Account) error
	Delete(ctx context.Context, id uint) error
}

type accountRepository struct {
	db *gorm.DB
}

// NewAccountRepository 创建账号仓储实例。
func NewAccountRepository(db *gorm.DB) AccountRepository {
	return &accountRepository{db: db}
}

func (r *accountRepository) Create(ctx context.Context, account *model.Account) error {
	return r.db.WithContext(ctx).Create(account).Error
}

func (r *accountRepository) GetByID(ctx context.Context, id uint) (*model.Account, error) {
	var account model.Account
	err := r.db.WithContext(ctx).First(&account, id).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *accountRepository) GetByUsername(ctx context.Context, username string) (*model.Account, error) {
	var account model.Account
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// ListByIDs 批量按主键查询账号。
func (r *accountRepository) ListByIDs(ctx context.Context, ids []uint) (map[uint]*model.Account, error) {
	out := make(map[uint]*model.Account, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// 去重，避免 IN 子句膨胀。
	unique := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return out, nil
	}

	var accounts []model.Account
	if err := r.db.WithContext(ctx).Where("id IN ?", unique).Find(&accounts).Error; err != nil {
		return nil, err
	}
	for i := range accounts {
		acc := accounts[i]
		out[acc.ID] = &acc
	}
	return out, nil
}

func (r *accountRepository) List(ctx context.Context, offset, limit int) ([]model.Account, int64, error) {
	var accounts []model.Account
	var total int64

	query := r.db.WithContext(ctx).Model(&model.Account{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Offset(offset).Limit(limit).Order("id DESC").Find(&accounts).Error; err != nil {
		return nil, 0, err
	}
	return accounts, total, nil
}

func (r *accountRepository) Update(ctx context.Context, account *model.Account) error {
	return r.db.WithContext(ctx).Save(account).Error
}

func (r *accountRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Account{}, id).Error
}
