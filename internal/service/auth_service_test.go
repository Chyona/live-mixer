package service

import (
	"context"
	"errors"
	"testing"

	"live-mixer/internal/model"
	"live-mixer/pkg/utils"

	"gorm.io/gorm"
)

// mockAccountRepo 用于单元测试的账号仓储 mock。
type mockAccountRepo struct {
	accounts map[string]*model.Account
}

func (m *mockAccountRepo) Create(ctx context.Context, account *model.Account) error {
	return nil
}

func (m *mockAccountRepo) GetByID(ctx context.Context, id uint) (*model.Account, error) {
	return nil, gorm.ErrRecordNotFound
}

func (m *mockAccountRepo) GetByUsername(ctx context.Context, username string) (*model.Account, error) {
	account, ok := m.accounts[username]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return account, nil
}

func (m *mockAccountRepo) ListByIDs(ctx context.Context, ids []uint) (map[uint]*model.Account, error) {
	out := make(map[uint]*model.Account, len(ids))
	for _, id := range ids {
		for _, acc := range m.accounts {
			if acc != nil && acc.ID == id {
				out[id] = acc
				break
			}
		}
	}
	return out, nil
}

func (m *mockAccountRepo) List(ctx context.Context, offset, limit int) ([]model.Account, int64, error) {
	return nil, 0, nil
}

func (m *mockAccountRepo) Update(ctx context.Context, account *model.Account) error {
	return nil
}

func (m *mockAccountRepo) Delete(ctx context.Context, id uint) error {
	return nil
}

func TestAuthService_Login_Success(t *testing.T) {
	hashed, _ := utils.HashPassword("admin")
	repo := &mockAccountRepo{
		accounts: map[string]*model.Account{
			"admin": {
				ID:       123,
				Username: "admin",
				Password: hashed,
				Nickname: "张三",
				Avatar:   "https://cdn.example.com/avatar/123.jpg",
				Roles:    "ADMIN",
				IsActive: 1,
			},
		},
	}

	svc := NewAuthService(repo, "test-secret", 7200)
	result, err := svc.Login(context.Background(), "admin", "admin")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if result.Token == "" {
		t.Error("Login() token should not be empty")
	}
	if result.ExpiresIn != 7200 {
		t.Errorf("ExpiresIn = %d, want 7200", result.ExpiresIn)
	}
	if result.ID != "123" {
		t.Errorf("ID = %q, want %q", result.ID, "123")
	}
	if result.Username != "admin" {
		t.Errorf("Username = %q, want admin", result.Username)
	}
	if result.Nickname != "张三" {
		t.Errorf("Nickname = %q, want 张三", result.Nickname)
	}
	if len(result.Roles) != 1 || result.Roles[0] != "ADMIN" {
		t.Errorf("Roles = %v, want [ADMIN]", result.Roles)
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	hashed, _ := utils.HashPassword("admin")
	repo := &mockAccountRepo{
		accounts: map[string]*model.Account{
			"admin": {Username: "admin", Password: hashed, IsActive: 1},
		},
	}

	svc := NewAuthService(repo, "test-secret", 7200)
	_, err := svc.Login(context.Background(), "admin", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if !errors.Is(err, errors.New("用户名或密码错误")) && err.Error() != "用户名或密码错误" {
		t.Errorf("error = %q, want 用户名或密码错误", err.Error())
	}
}

func TestAuthService_Login_UserNotFound(t *testing.T) {
	svc := NewAuthService(&mockAccountRepo{accounts: map[string]*model.Account{}}, "test-secret", 7200)
	_, err := svc.Login(context.Background(), "unknown", "admin")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
	if err.Error() != "用户名或密码错误" {
		t.Errorf("error = %q, want 用户名或密码错误", err.Error())
	}
}

func TestAuthService_Login_DisabledAccount(t *testing.T) {
	hashed, _ := utils.HashPassword("admin")
	repo := &mockAccountRepo{
		accounts: map[string]*model.Account{
			"admin": {Username: "admin", Password: hashed, IsActive: 0},
		},
	}

	svc := NewAuthService(repo, "test-secret", 7200)
	_, err := svc.Login(context.Background(), "admin", "admin")
	if err == nil {
		t.Fatal("expected error for disabled account")
	}
	if err.Error() != "账号已被禁用" {
		t.Errorf("error = %q, want 账号已被禁用", err.Error())
	}
}
