package api

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	openapi_types "github.com/oapi-codegen/runtime/types"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// PGAccounts 用生成的查询实现 AccountStore。
//
// 放在接口层而不是 store 层：依赖方向是「接口层 → 数据层」，
// 让 store 反过来依赖 api.Account 会把依赖方向拧成环。
type PGAccounts struct {
	queries *storegen.Queries
}

// NewPGAccounts 包装一组 sqlc 查询。
func NewPGAccounts(q *storegen.Queries) *PGAccounts { return &PGAccounts{queries: q} }

// AccountByUsername 按用户名查账号。
func (a *PGAccounts) AccountByUsername(ctx context.Context, username string) (Account, error) {
	row, err := a.queries.GetUserByUsername(ctx, username)
	if err != nil {
		return Account{}, translate(err)
	}
	return account(row.ID, row.Username, row.DisplayName, row.PasswordHash, row.Role), nil
}

// AccountByID 按 id 查账号，用于会话续期时确认用户仍然存在。
func (a *PGAccounts) AccountByID(ctx context.Context, id openapi_types.UUID) (Account, error) {
	row, err := a.queries.GetUserByID(ctx, pgUUID(id))
	if err != nil {
		return Account{}, translate(err)
	}
	return account(row.ID, row.Username, row.DisplayName, row.PasswordHash, row.Role), nil
}

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAccountNotFound
	}
	return err
}

func account(id pgtype.UUID, username, displayName, hash, role string) Account {
	return Account{
		ID:           apiUUID(id),
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: hash,
		Role:         role,
	}
}

func apiUUID(p pgtype.UUID) openapi_types.UUID {
	var u openapi_types.UUID
	if p.Valid {
		copy(u[:], p.Bytes[:])
	}
	return u
}

func pgUUID(u openapi_types.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(u), Valid: true}
}
