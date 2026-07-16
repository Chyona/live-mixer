package v1

import (
	"context"

	"live-mixer/internal/repository"
)

// createdByResolver 将账号 ID 解析为 REST 响应中的 created_by 展示名。
type createdByResolver struct {
	accountRepo repository.AccountRepository
}

func newCreatedByResolver(accountRepo repository.AccountRepository) createdByResolver {
	return createdByResolver{accountRepo: accountRepo}
}

// nameOf 返回单个账号的展示名：优先 nickname，否则 username；查不到则为空。
func (r createdByResolver) nameOf(ctx context.Context, accountID uint) string {
	if r.accountRepo == nil || accountID == 0 {
		return ""
	}
	account, err := r.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return ""
	}
	return account.DisplayName()
}

// namesOf 批量解析账号展示名。
func (r createdByResolver) namesOf(ctx context.Context, accountIDs []uint) map[uint]string {
	out := make(map[uint]string, len(accountIDs))
	if r.accountRepo == nil || len(accountIDs) == 0 {
		return out
	}
	accounts, err := r.accountRepo.ListByIDs(ctx, accountIDs)
	if err != nil {
		return out
	}
	for id, account := range accounts {
		if account != nil {
			out[id] = account.DisplayName()
		}
	}
	return out
}

// uniqueAccountIDs 收集去重后的非零账号 ID。
func uniqueAccountIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
