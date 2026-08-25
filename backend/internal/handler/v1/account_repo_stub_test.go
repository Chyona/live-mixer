package v1

import (
	"context"

	"live-mixer/internal/model"
	"live-mixer/internal/repository"

	"gorm.io/gorm"
)

// stubAccountRepo 用于 handler 单测解析 created_by 展示名。
type stubAccountRepo struct {
	byID map[uint]*model.Account
}

func (s *stubAccountRepo) Create(ctx context.Context, account *model.Account) error { return nil }
func (s *stubAccountRepo) GetByID(ctx context.Context, id uint) (*model.Account, error) {
	if s == nil || s.byID == nil {
		return nil, gorm.ErrRecordNotFound
	}
	acc, ok := s.byID[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return acc, nil
}
func (s *stubAccountRepo) GetByUsername(ctx context.Context, username string) (*model.Account, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubAccountRepo) ListByIDs(ctx context.Context, ids []uint) (map[uint]*model.Account, error) {
	out := make(map[uint]*model.Account, len(ids))
	if s == nil || s.byID == nil {
		return out, nil
	}
	for _, id := range ids {
		if acc, ok := s.byID[id]; ok {
			out[id] = acc
		}
	}
	return out, nil
}
func (s *stubAccountRepo) List(ctx context.Context, offset, limit int) ([]model.Account, int64, error) {
	return nil, 0, nil
}
func (s *stubAccountRepo) Update(ctx context.Context, account *model.Account) error { return nil }
func (s *stubAccountRepo) Delete(ctx context.Context, id uint) error               { return nil }

// accountsStub 构造仅含 GetByID/ListByIDs 的账号仓储桩。
func accountsStub(accounts ...*model.Account) repository.AccountRepository {
	byID := make(map[uint]*model.Account, len(accounts))
	for _, a := range accounts {
		if a != nil {
			byID[a.ID] = a
		}
	}
	return &stubAccountRepo{byID: byID}
}
