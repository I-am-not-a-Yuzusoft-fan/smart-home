package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// ErrItemNotFound 表示物品不存在。
var ErrItemNotFound = errors.New("物品不存在")

// Item 是物品的持久化视图；kind 与 roomName 由类型/房间表 join 得到。
type Item struct {
	ID       uuid.UUID
	TypeKey  string
	Kind     string
	Name     string
	RoomHash *string
	RoomName *string
	X, Y     float64
	Rotation float64
	Status   *string
	Attrs    json.RawMessage
	// 挂墙物品：位置由 (wallId, wallOffset) 定位，x/y/rotation 是服务端算出来的派生值。
	WallID     *uuid.UUID
	WallOffset *float64
}

// ItemFilter 是列表筛选条件；nil 表示不限。
type ItemFilter struct {
	RoomHash *string
	Kind     *string
	Query    *string
}

// InitialState 是创建设备时写入的第一条 capability_state。
type InitialState struct {
	Key   string
	Value json.RawMessage
}

// ItemCreate 是创建物品的输入；States 由类型元数据推导。
type ItemCreate struct {
	TypeKey    string
	Name       string
	RoomHash   *string
	X, Y       float64
	Rotation   float64
	Status     *string
	Attrs      json.RawMessage
	States     []InitialState
	WallID     *uuid.UUID
	WallOffset *float64
}

// ItemUpdate 是逐字段 PATCH 的输入。
type ItemUpdate struct {
	ID          uuid.UUID
	NameSet     bool
	Name        string
	RoomSet     bool
	RoomHash    string
	PositionSet bool
	X, Y        float64
	Rotation    float64
	StatusSet   bool
	Status      string
	AttrsSet    bool
	Attrs       json.RawMessage
	WallSet     bool
	WallID      *uuid.UUID
	WallOffset  *float64
}

// Items 是物品的 PostgreSQL 存储。
type Items struct {
	pool *pgxpool.Pool
	q    *storegen.Queries
}

// NewItems 构造物品存储。
func NewItems(pool *pgxpool.Pool) *Items {
	return &Items{pool: pool, q: storegen.New(pool)}
}

func (s *Items) ListItems(ctx context.Context, filter ItemFilter) ([]Item, error) {
	rows, err := s.q.ListItems(ctx, storegen.ListItemsParams{
		RoomHash: textValue(filter.RoomHash),
		Kind:     textValue(filter.Kind),
		Q:        textValue(filter.Query),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		item, err := itemFromRow(row.ID, row.TypeKey, row.Kind, row.Name, row.RoomHash, row.RoomName, row.X, row.Y, row.Rotation, row.Status, row.Attrs, row.WallID, row.WallOffset)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Items) GetItem(ctx context.Context, id uuid.UUID) (Item, error) {
	row, err := s.q.GetItem(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrItemNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return itemFromRow(row.ID, row.TypeKey, row.Kind, row.Name, row.RoomHash, row.RoomName, row.X, row.Y, row.Rotation, row.Status, row.Attrs, row.WallID, row.WallOffset)
}

// CreateItem 在一个事务里写 item 与初始 capability_state。
func (s *Items) CreateItem(ctx context.Context, in ItemCreate) (Item, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	rawAttrs := in.Attrs
	if len(rawAttrs) == 0 {
		rawAttrs = json.RawMessage(`{}`)
	}
	id, err := q.InsertItem(ctx, storegen.InsertItemParams{
		TypeKey:    in.TypeKey,
		Name:       in.Name,
		RoomHash:   textValue(in.RoomHash),
		X:          in.X,
		Y:          in.Y,
		Rotation:   in.Rotation,
		Status:     textValue(in.Status),
		Attrs:      rawAttrs,
		WallID:     uuidPtrValue(in.WallID),
		WallOffset: floatPtrValue(in.WallOffset),
	})
	if err != nil {
		return Item{}, err
	}
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	for _, state := range in.States {
		if err := q.UpsertState(ctx, storegen.UpsertStateParams{
			ItemID:        id,
			CapabilityKey: state.Key,
			Value:         state.Value,
			Source:        "device",
			UpdatedAt:     now,
		}); err != nil {
			return Item{}, err
		}
	}
	row, err := q.GetItem(ctx, id)
	if err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return itemFromRow(row.ID, row.TypeKey, row.Kind, row.Name, row.RoomHash, row.RoomName, row.X, row.Y, row.Rotation, row.Status, row.Attrs, row.WallID, row.WallOffset)
}

func (s *Items) UpdateItem(ctx context.Context, in ItemUpdate) (Item, error) {
	rows, err := s.q.UpdateItem(ctx, storegen.UpdateItemParams{
		ID:         pgUUID(in.ID),
		NameSet:    in.NameSet,
		Name:       in.Name,
		RoomSet:    in.RoomSet,
		RoomHash:   in.RoomHash,
		PosSet:     in.PositionSet,
		X:          in.X,
		Y:          in.Y,
		Rotation:   in.Rotation,
		StatusSet:  in.StatusSet,
		Status:     in.Status,
		AttrsSet:   in.AttrsSet,
		Attrs:      in.Attrs,
		WallSet:    in.WallSet,
		WallID:     uuidPtrValue(in.WallID),
		WallOffset: floatPtrValue(in.WallOffset),
	})
	if err != nil {
		return Item{}, err
	}
	if rows == 0 {
		return Item{}, ErrItemNotFound
	}
	return s.GetItem(ctx, in.ID)
}

func (s *Items) DeleteItem(ctx context.Context, id uuid.UUID) (int64, error) {
	return s.q.DeleteItem(ctx, pgUUID(id))
}

func itemFromRow(id pgtype.UUID, typeKey, kind, name string, roomHash, roomName pgtype.Text, x, y, rotation float64, status pgtype.Text, attrs json.RawMessage, wallID pgtype.UUID, wallOffset pgtype.Float8) (Item, error) {
	itemID, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		return Item{}, err
	}
	return Item{
		ID:         itemID,
		TypeKey:    typeKey,
		Kind:       kind,
		Name:       name,
		RoomHash:   fromPGText(roomHash),
		RoomName:   fromPGText(roomName),
		X:          x,
		Y:          y,
		Rotation:   rotation,
		Status:     fromPGText(status),
		Attrs:      attrs,
		WallID:     uuidPtrFromPG(wallID),
		WallOffset: floatPtrFromPG(wallOffset),
	}, nil
}

func textValue(v *string) pgtype.Text {
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

func uuidPtrValue(v *uuid.UUID) pgtype.UUID {
	if v == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: [16]byte(*v), Valid: true}
}

func uuidPtrFromPG(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	id := uuid.UUID(v.Bytes)
	return &id
}

func floatPtrValue(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
}

func floatPtrFromPG(v pgtype.Float8) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}
