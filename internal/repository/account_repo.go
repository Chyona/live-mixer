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
