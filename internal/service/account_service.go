// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"
	"live-mixer/pkg/utils"
	"gorm.io/gorm"
)

// AccountService 账号业务接口。
type AccountService interface {
	CreateAccount(ctx context.Context, username, email, password, nickname, avatar, roles string) (*model.Account, error)
	GetAccount(ctx context.Context, id uint) (*model.Account, error)
	ListAccounts(ctx context.Context, page, pageSize int) ([]model.Account, int64, error)
	UpdateAccount(ctx context.Context, id uint, nickname string, avatar, roles *string, isActive *int8) (*model.Account, error)
	DeleteAccount(ctx context.Context, id uint) error
}

type accountService struct {
	accountRepo repository.AccountRepository
}

// NewAccountService 创建账号业务服务实例。
func NewAccountService(accountRepo repository.AccountRepository) AccountService {
	return &accountService{accountRepo: accountRepo}
}

func (s *accountService) CreateAccount(ctx context.Context, username, email, password, nickname, avatar, roles string) (*model.Account, error) {
	// 检查账号名是否已存在
	if _, err := s.accountRepo.GetByUsername(ctx, username); err == nil {
		return nil, errors.New("账号名已存在")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	hashed, err := utils.HashPassword(password)
	if err != nil {
		return nil, err
	}

	account := &model.Account{
		Username: username,
		Email:    email,
		Password: hashed,
		Nickname: nickname,
		Avatar:   avatar,
		Roles:    roles,
		IsActive: 1,
	}
	if err := s.accountRepo.Create(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *accountService) GetAccount(ctx context.Context, id uint) (*model.Account, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("账号不存在")
		}
		return nil, err
	}
	return account, nil
}

func (s *accountService) ListAccounts(ctx context.Context, page, pageSize int) ([]model.Account, int64, error) {
	offset := (page - 1) * pageSize
	return s.accountRepo.List(ctx, offset, pageSize)
}

func (s *accountService) UpdateAccount(ctx context.Context, id uint, nickname string, avatar, roles *string, isActive *int8) (*model.Account, error) {
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("账号不存在")
		}
		return nil, err
	}
	if nickname != "" {
		account.Nickname = nickname
	}
	// 头像与角色使用指针，区分「未传参」与「传空值清空」
	if avatar != nil {
		account.Avatar = *avatar
	}
	if roles != nil {
		account.Roles = *roles
	}
	if isActive != nil {
		account.IsActive = *isActive
	}
	if err := s.accountRepo.Update(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *accountService) DeleteAccount(ctx context.Context, id uint) error {
	return s.accountRepo.Delete(ctx, id)
}
