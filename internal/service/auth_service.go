// Package service 业务逻辑层，编排 repository 完成业务处理。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"live-mixer/internal/repository"
	jwtpkg "live-mixer/pkg/jwt"
	"live-mixer/pkg/utils"
	"gorm.io/gorm"
)

// LoginResult 登录成功后的响应数据。
type LoginResult struct {
	Token     string   `json:"token"`
	ExpiresIn int      `json:"expires_in"`
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	Nickname  string   `json:"nickname"`
	Avatar    string   `json:"avatar"`
	Roles     []string `json:"roles"`
}

// AuthService 认证业务接口。
type AuthService interface {
	Login(ctx context.Context, username, password string) (*LoginResult, error)
}

type authService struct {
	accountRepo repository.AccountRepository
	jwtSecret   string
	expiresIn   int
}

// NewAuthService 创建认证业务服务实例。
func NewAuthService(accountRepo repository.AccountRepository, jwtSecret string, expiresIn int) AuthService {
	return &authService{
		accountRepo: accountRepo,
		jwtSecret:   jwtSecret,
		expiresIn:   expiresIn,
	}
}

func (s *authService) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	// 根据用户名查询账号
	account, err := s.accountRepo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}

	// 校验密码
	if !utils.ComparePassword(account.Password, password) {
		return nil, errors.New("用户名或密码错误")
	}

	// 禁用账号不允许登录
	if account.IsActive != 1 {
		return nil, errors.New("账号已被禁用")
	}

	roles := utils.ParseRoles(account.Roles)

	// 签发 JWT，将常用用户信息写入 Claims，后续请求可直接从 Token 读取
	token, err := jwtpkg.GenerateToken(s.jwtSecret, s.expiresIn, jwtpkg.UserClaims{
		UserID:   account.ID,
		Username: account.Username,
		Nickname: account.Nickname,
		Avatar:   account.Avatar,
		Roles:    roles,
	})
	if err != nil {
		return nil, fmt.Errorf("生成 Token 失败: %w", err)
	}

	return &LoginResult{
		Token:     token,
		ExpiresIn: s.expiresIn,
		ID:        strconv.FormatUint(uint64(account.ID), 10),
		Username:  account.Username,
		Nickname:  account.Nickname,
		Avatar:    account.Avatar,
		Roles:     roles,
	}, nil
}
