package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// PGItemTypes 是 ItemTypeStore 的 PostgreSQL 实现。
// 类型 CRUD 都是单语句，不需要事务，因此只持有生成查询。
type PGItemTypes struct {
	queries *storegen.Queries
}

// NewPGItemTypes 用连接池构造类型模板存储。
func NewPGItemTypes(pool *pgxpool.Pool) *PGItemTypes {
	return &PGItemTypes{queries: storegen.New(pool)}
}

func (p *PGItemTypes) ListItemTypes(ctx context.Context) ([]ItemType, error) {
	rows, err := p.queries.ListItemTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ItemType, 0, len(rows))
	for _, row := range rows {
		typ, err := itemTypeFromRow(row.Key, row.Kind, row.Name, row.Icons, row.DefaultWidth, row.DefaultDepth, row.Mount, row.Capabilities, row.CapabilitiesVersion, row.Driver)
		if err != nil {
			return nil, err
		}
		out = append(out, typ)
	}
	return out, nil
}

func (p *PGItemTypes) GetItemType(ctx context.Context, key string) (ItemType, error) {
	row, err := p.queries.GetItemType(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ItemType{}, ErrItemTypeNotFound
	}
	if err != nil {
		return ItemType{}, err
	}
	return itemTypeFromRow(row.Key, row.Kind, row.Name, row.Icons, row.DefaultWidth, row.DefaultDepth, row.Mount, row.Capabilities, row.CapabilitiesVersion, row.Driver)
}

func (p *PGItemTypes) CreateItemType(ctx context.Context, in ItemType) (ItemType, error) {
	capabilities, err := json.Marshal(in.Capabilities)
	if err != nil {
		return ItemType{}, err
	}
	row, err := p.queries.InsertItemType(ctx, storegen.InsertItemTypeParams{
		Key:          in.Key,
		Kind:         in.Kind,
		Name:         in.Name,
		Icons:        toPGText(in.Icons),
		DefaultWidth: in.DefaultWidth,
		DefaultDepth: in.DefaultDepth,
		Capabilities: capabilities,
		Driver:       toPGText(in.Driver),
		Mount:        toPGText(&in.Mount),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ItemType{}, ErrItemTypeExists
		}
		return ItemType{}, err
	}
	return itemTypeFromRow(row.Key, row.Kind, row.Name, row.Icons, row.DefaultWidth, row.DefaultDepth, row.Mount, row.Capabilities, row.CapabilitiesVersion, row.Driver)
}

func (p *PGItemTypes) UpdateItemType(ctx context.Context, key string, patch ItemTypePatch) (ItemType, error) {
	capabilities := json.RawMessage("[]")
	if patch.CapabilitiesSet {
		encoded, err := json.Marshal(patch.Capabilities)
		if err != nil {
			return ItemType{}, err
		}
		capabilities = encoded
	}
	row, err := p.queries.UpdateItemType(ctx, storegen.UpdateItemTypeParams{
		Key:             key,
		NameSet:         patch.NameSet,
		Name:            patch.Name,
		IconsSet:        patch.IconsSet,
		Icons:           patch.Icons,
		WidthSet:        patch.WidthSet,
		Width:           patch.Width,
		DepthSet:        patch.DepthSet,
		Depth:           patch.Depth,
		CapabilitiesSet: patch.CapabilitiesSet,
		Capabilities:    capabilities,
		DriverSet:       patch.DriverSet,
		Driver:          patch.Driver,
		MountSet:        patch.MountSet,
		Mount:           toPGText(&patch.Mount),
		VersionSet:      patch.VersionSet,
		ExpectedVersion: int32(patch.ExpectedVersion),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if patch.VersionSet {
			return ItemType{}, ErrItemTypeConflict
		}
		return ItemType{}, ErrItemTypeNotFound
	}
	if err != nil {
		return ItemType{}, err
	}
	return itemTypeFromRow(row.Key, row.Kind, row.Name, row.Icons, row.DefaultWidth, row.DefaultDepth, row.Mount, row.Capabilities, row.CapabilitiesVersion, row.Driver)
}

func (p *PGItemTypes) DeleteItemType(ctx context.Context, key string) (int64, error) {
	deleted, err := p.queries.DeleteItemType(ctx, key)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return 0, ErrItemTypeInUse
	}
	return deleted, err
}

func (p *PGItemTypes) CountItemsByType(ctx context.Context, key string) (int64, error) {
	return p.queries.CountItemsByType(ctx, key)
}

func itemTypeFromRow(key, kind, name string, icons pgtype.Text, width, depth float64, mount string, rawCapabilities json.RawMessage, version int32, driver pgtype.Text) (ItemType, error) {
	capabilities := make([]Capability, 0)
	if len(rawCapabilities) > 0 && string(rawCapabilities) != "null" {
		if err := json.Unmarshal(rawCapabilities, &capabilities); err != nil {
			return ItemType{}, err
		}
	}
	return ItemType{
		Key:                 key,
		Kind:                kind,
		Name:                name,
		Icons:               fromPGText(icons),
		DefaultWidth:        width,
		DefaultDepth:        depth,
		Mount:               mount,
		Capabilities:        capabilities,
		CapabilitiesVersion: int(version),
		Driver:              fromPGText(driver),
	}, nil
}

func toPGText(v *string) pgtype.Text {
	if v == nil || *v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}

func fromPGText(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// CountStateRowsForKey 统计某个类型下、某个能力 key 已有的状态行数。
func (p *PGItemTypes) CountStateRowsForKey(ctx context.Context, typeKey, capabilityKey string) (int64, error) {
	return p.queries.CountStateRowsForKey(ctx, storegen.CountStateRowsForKeyParams{
		TypeKey:       typeKey,
		CapabilityKey: capabilityKey,
	})
}
