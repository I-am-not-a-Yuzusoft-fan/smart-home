package api

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	openapi_types "github.com/oapi-codegen/runtime/types"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// PGPlan 是 PlanStore 的 PostgreSQL 实现。
//
// 放在 api 包，和 PGAccounts 同一个理由：依赖方向是接口层 → 数据层。
// 如果放到 store 层并返回 api 的结构，依赖就拧成环了。
type PGPlan struct {
	pool    *pgxpool.Pool
	queries *storegen.Queries
}

// NewPGPlan 用连接池构造户型存储。
func NewPGPlan(pool *pgxpool.Pool) *PGPlan {
	return &PGPlan{pool: pool, queries: storegen.New(pool)}
}

// LoadPlan 读全部端点与墙。两条查询分开，端点表很小，不值得 join 成一条再拆。
func (p *PGPlan) LoadPlan(ctx context.Context) ([]PlanCorner, []PlanWall, error) {
	corners, err := p.queries.ListCorners(ctx)
	if err != nil {
		return nil, nil, err
	}
	walls, err := p.queries.ListWalls(ctx)
	if err != nil {
		return nil, nil, err
	}

	outCorners := make([]PlanCorner, 0, len(corners))
	for _, c := range corners {
		outCorners = append(outCorners, PlanCorner{ID: apiUUID(c.ID), X: c.X, Y: c.Y})
	}
	outWalls := make([]PlanWall, 0, len(walls))
	for _, w := range walls {
		outWalls = append(outWalls, PlanWall{
			ID:        apiUUID(w.ID),
			AID:       apiUUID(w.A),
			BID:       apiUUID(w.B),
			AX:        w.Ax,
			AY:        w.Ay,
			BX:        w.Bx,
			BY:        w.By,
			Thickness: w.Thickness,
		})
	}
	return outCorners, outWalls, nil
}

// CreateWall 在一个事务里：端点按坐标 upsert，再插入墙。
//
// 同一对端点只留一面墙，反着画（B→A）也算同一面——这种情况下什么也不做，
// 让调用方读到的仍是已有的那面。
func (p *PGPlan) CreateWall(ctx context.Context, in WallInput) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := p.queries.WithTx(tx)

	// 两个端点固定按 (x, y) 顺序 upsert：两个人同时画"同一面墙的两个方向"时，
	// 若顺序相反，各持一把锁再互等就会死锁（PG 40P01）。
	first := storegen.UpsertCornerParams{X: in.AX, Y: in.AY}
	second := storegen.UpsertCornerParams{X: in.BX, Y: in.BY}
	aIsFirst := true
	if first.X > second.X || (first.X == second.X && first.Y > second.Y) {
		first, second = second, first
		aIsFirst = false
	}

	a, err := q.UpsertCorner(ctx, first)
	if err != nil {
		return err
	}
	b, err := q.UpsertCorner(ctx, second)
	if err != nil {
		return err
	}
	if !aIsFirst {
		a, b = b, a
	}

	_, err = q.GetWallByPair(ctx, storegen.GetWallByPairParams{A: a, B: b})
	switch {
	case err == nil:
		// 已经有这面墙了，提交事务（端点 upsert 可能刚创建过一个孤立端点）。
		// 每个改动都要 +1 版本号：撤销/重做靠它做乐观并发。
		if _, err := q.BumpPlanVersion(ctx); err != nil {
			return fmt.Errorf("递增版本号: %w", err)
		}
		return tx.Commit(ctx)
	case errors.Is(err, pgx.ErrNoRows):
		// 继续插入
	default:
		return err
	}

	if _, err := q.InsertWall(ctx, storegen.InsertWallParams{A: a, B: b, Thickness: in.Thickness}); err != nil {
		// 并发下别人刚好插进来：唯一索引挡住后按"已存在"处理，不算失败。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// 每个改动都要 +1 版本号：撤销/重做靠它做乐观并发。
			if _, err := q.BumpPlanVersion(ctx); err != nil {
				return fmt.Errorf("递增版本号: %w", err)
			}
			return tx.Commit(ctx)
		}
		return err
	}
	// 每个改动都要 +1 版本号：撤销/重做靠它做乐观并发。
	if _, err := q.BumpPlanVersion(ctx); err != nil {
		return fmt.Errorf("递增版本号: %w", err)
	}
	return tx.Commit(ctx)
}

// LoadRoomNames 读全部房间命名。房间本身不落库，这里只有"名字"。
// UpdateWall 改一面墙的两个端点（或厚度）。
//
// 端点按坐标 upsert：拖到别处已有端点上等于合并到那个点，拖到空处等于新端点。
// 若改完与另一面墙完全重合，返回 ErrWallConflict——不自作主张把两面墙并成一面，
// 那样用户会丢失"这里原来是两面墙"的事实。
func (p *PGPlan) UpdateWall(ctx context.Context, id openapi_types.UUID, in WallInput) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := p.queries.WithTx(tx)

	if _, err := q.GetWall(ctx, pgtype.UUID{Bytes: id, Valid: true}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWallNotFound
		}
		return err
	}

	// 与 CreateWall 同样的顺序：按 (x, y) 排序后 upsert，避免反向并发死锁。
	first := storegen.UpsertCornerParams{X: in.AX, Y: in.AY}
	second := storegen.UpsertCornerParams{X: in.BX, Y: in.BY}
	aIsFirst := true
	if first.X > second.X || (first.X == second.X && first.Y > second.Y) {
		first, second = second, first
		aIsFirst = false
	}

	a, err := q.UpsertCorner(ctx, first)
	if err != nil {
		return err
	}
	b, err := q.UpsertCorner(ctx, second)
	if err != nil {
		return err
	}
	if !aIsFirst {
		a, b = b, a
	}

	// 这一对端点上已经有别的墙 → 409，不合并。
	existing, err := q.GetWallByPair(ctx, storegen.GetWallByPairParams{A: a, B: b})
	switch {
	case err == nil:
		if existing.Bytes != id {
			return ErrWallConflict
		}
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return err
	}

	if _, err := q.UpdateWall(ctx, storegen.UpdateWallParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, A: a, B: b, Thickness: in.Thickness,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrWallConflict
		}
		return err
	}
	if err := q.DeleteOrphanCorners(ctx); err != nil {
		return err
	}
	// 挂在这面墙上的物品跟着一起挪：位置按新的端点重算，偏移超出新墙长时夹住。
	if err := repositionWallItems(ctx, q, id, in); err != nil {
		return fmt.Errorf("重算挂墙物品: %w", err)
	}
	// 开口同理：墙变短时把门/窗夹回墙内，不然门会一半悬空。
	if err := clampWallOpenings(ctx, q, id, in); err != nil {
		return fmt.Errorf("夹紧墙上的开口: %w", err)
	}
	// 每个改动都要 +1 版本号：撤销/重做靠它做乐观并发。
	if _, err := q.BumpPlanVersion(ctx); err != nil {
		return fmt.Errorf("递增版本号: %w", err)
	}
	return tx.Commit(ctx)
}

func (p *PGPlan) LoadRoomNames(ctx context.Context) ([]RoomName, error) {
	rows, err := p.queries.ListRoomNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoomName, 0, len(rows))
	for _, r := range rows {
		out = append(out, RoomName{Hash: r.PolygonHash, Name: r.Name, Kind: r.Kind})
	}
	return out, nil
}

// DeleteWall 删墙并返回受影响行数；0 表示没有这面墙。
func (p *PGPlan) DeleteWall(ctx context.Context, id openapi_types.UUID) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := p.queries.WithTx(tx)
	n, err := q.DeleteWall(ctx, pgUUID(id))
	if err != nil {
		return 0, err
	}
	// 删完墙，端点可能没人用了；留着会变成画布上点不中的残点。
	if err := q.DeleteOrphanCorners(ctx); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// LoadVersion 读平面图版本号。
func (p *PGPlan) LoadVersion(ctx context.Context) (int64, error) {
	return p.queries.GetPlanVersion(ctx)
}

// ReplacePlan 按 id 对齐地整份替换墙列表，并在同一事务里把版本号 +1。
//
// 三个要点：
//  1. 先 `FOR UPDATE` 锁住版本行，才能让「读版本 → 比对 → 写 → +1」不被插队；
//  2. 有 id 的墙原地更新（保住 id），没 id 的插入，列表里没有的删除；
//  3. 端点按 (x, y) upsert，删完墙再清孤立端点——和被删墙一起消失的端点不该留在库里。
func (p *PGPlan) ReplacePlan(ctx context.Context, version int64, walls []WallSnapshot) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := storegen.New(tx)
	current, err := q.LockPlanVersion(ctx)
	if err != nil {
		return fmt.Errorf("读取版本号: %w", err)
	}
	if current != version {
		return ErrPlanConflict
	}

	// 端点：先把每一面墙的两个端点按坐标换成 id。
	type plannedWall struct {
		id        *string
		a, b      pgtype.UUID
		thickness float64
	}
	planned := make([]plannedWall, 0, len(walls))
	for _, w := range walls {
		aID, err := q.UpsertCorner(ctx, storegen.UpsertCornerParams{X: w.AX, Y: w.AY})
		if err != nil {
			return fmt.Errorf("写入端点: %w", err)
		}
		bID, err := q.UpsertCorner(ctx, storegen.UpsertCornerParams{X: w.BX, Y: w.BY})
		if err != nil {
			return fmt.Errorf("写入端点: %w", err)
		}
		planned = append(planned, plannedWall{id: w.ID, a: aID, b: bID, thickness: w.Thickness})
	}

	// 现存的墙：不在新列表里的删掉。
	existing, err := q.ListWallIDs(ctx)
	if err != nil {
		return fmt.Errorf("列出墙: %w", err)
	}
	keep := make(map[string]bool, len(planned))
	for _, w := range planned {
		if w.id != nil {
			keep[*w.id] = true
		}
	}
	for _, id := range existing {
		key := uuidString(id)
		if keep[key] {
			continue
		}
		if err := q.DeleteWallByID(ctx, id); err != nil {
			return fmt.Errorf("删除多余墙: %w", err)
		}
	}

	// 逐面墙：有 id 的更新，没 id 的插入。
	for _, w := range planned {
		if w.id == nil {
			if _, err := q.InsertWall(ctx, storegen.InsertWallParams{A: w.a, B: w.b, Thickness: w.thickness}); err != nil {
				return fmt.Errorf("插入墙: %w", err)
			}
			continue
		}
		id, err := parseUUID(*w.id)
		if err != nil {
			return err
		}
		n, err := q.UpdateWallGeom(ctx, storegen.UpdateWallGeomParams{ID: id, A: w.a, B: w.b, Thickness: w.thickness})
		if err != nil {
			return fmt.Errorf("更新墙: %w", err)
		}
		if n == 0 {
			// 客户端认为存在、库里没有（别人删了）：按新墙插入，保住这次撤销的结果。
			if err := q.InsertWallWithID(ctx, storegen.InsertWallWithIDParams{ID: id, A: w.a, B: w.b, Thickness: w.thickness}); err != nil {
				return fmt.Errorf("补插墙: %w", err)
			}
		}
	}

	if err := q.DeleteOrphanCorners(ctx); err != nil {
		return fmt.Errorf("清理孤立端点: %w", err)
	}
	// 每个改动都要 +1 版本号：撤销/重做靠它做乐观并发。
	if _, err := q.BumpPlanVersion(ctx); err != nil {
		return fmt.Errorf("递增版本号: %w", err)
	}
	return tx.Commit(ctx)
}

// uuidString 把 pgtype.UUID 变成规范字符串，用墙 id 做集合比对。
func uuidString(id pgtype.UUID) string {
	u := apiUUID(id)
	return u.String()
}

// parseUUID 把字符串形式的 uuid 解析成 pgtype.UUID。
func parseUUID(raw string) (pgtype.UUID, error) {
	var u openapi_types.UUID
	if err := u.UnmarshalText([]byte(raw)); err != nil {
		return pgtype.UUID{}, fmt.Errorf("无效的墙 id %q: %w", raw, err)
	}
	return pgUUID(u), nil
}

// UpsertRoomName 只写名字与用途：几何由墙推导，库里不存房间多边形。
func (p *PGPlan) UpsertRoomName(ctx context.Context, hash, name, kind string) error {
	return p.queries.UpsertRoomName(ctx, storegen.UpsertRoomNameParams{
		PolygonHash: hash,
		Name:        name,
		Kind:        kind,
	})
}

// DeleteRoomName 清掉某个哈希上的名字。返回 0 行也当成功——"之后没有名字"就是调用方要的。
func (p *PGPlan) DeleteRoomName(ctx context.Context, hash string) (int64, error) {
	return p.queries.DeleteRoomName(ctx, hash)
}

// LoadOpenings 读全部门窗。开口很少，随 Plan 一次返回，省一次往返。
func (p *PGPlan) LoadOpenings(ctx context.Context) ([]PlanOpening, error) {
	rows, err := p.queries.ListOpenings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PlanOpening, 0, len(rows))
	for _, row := range rows {
		out = append(out, PlanOpening{
			ID:     apiUUID(row.ID),
			WallID: apiUUID(row.WallID),
			Kind:   row.Kind,
			Offset: row.Offset,
			Width:  row.Width,
			Height: row.Height,
			Sill:   row.Sill,
		})
	}
	return out, nil
}

// GetWall 读单面墙的端点坐标。
func (p *PGPlan) GetWall(ctx context.Context, id openapi_types.UUID) (PlanWall, error) {
	row, err := p.queries.GetWallByID(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanWall{}, ErrWallNotFound
	}
	if err != nil {
		return PlanWall{}, err
	}
	return PlanWall{
		ID:        apiUUID(row.ID),
		AID:       apiUUID(row.A),
		BID:       apiUUID(row.B),
		AX:        row.Ax,
		AY:        row.Ay,
		BX:        row.Bx,
		BY:        row.By,
		Thickness: row.Thickness,
	}, nil
}

// CreateOpening 校验「开口在墙内」后插入；默认值按类型给：门 210/0，窗 120/90。
func (p *PGPlan) CreateOpening(ctx context.Context, in OpeningInput) (openapi_types.UUID, error) {
	wall, err := p.GetWall(ctx, in.WallID)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	length := math.Hypot(wall.BX-wall.AX, wall.BY-wall.AY)
	// 允许 0.5 厘米的浮点误差：前端按墙长摆满整面墙时不该被 1e-9 卡住。
	if in.Offset < 0 || in.Width <= 0 || in.Offset+in.Width > length+0.5 {
		return openapi_types.UUID{}, ErrOpeningTooWide
	}
	height, sill := 210.0, 0.0
	if in.Kind == "window" {
		height, sill = 120, 90
	}
	if in.Height != nil {
		height = *in.Height
	}
	if in.Sill != nil {
		sill = *in.Sill
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return openapi_types.UUID{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.queries.WithTx(tx)

	id, err := q.InsertOpening(ctx, storegen.InsertOpeningParams{
		WallID: pgtype.UUID{Bytes: in.WallID, Valid: true},
		Kind:   in.Kind,
		Offset: in.Offset,
		Width:  in.Width,
		Height: height,
		Sill:   sill,
	})
	if err != nil {
		return openapi_types.UUID{}, err
	}
	if _, err := q.BumpPlanVersion(ctx); err != nil {
		return openapi_types.UUID{}, fmt.Errorf("递增版本号: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return openapi_types.UUID{}, err
	}
	return apiUUID(id), nil
}

// UpdateOpening 逐字段合并后再校验一次，越界直接拒绝。
func (p *PGPlan) UpdateOpening(ctx context.Context, id openapi_types.UUID, in OpeningPatchInput) error {
	existing, err := p.queries.GetOpening(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOpeningNotFound
	}
	if err != nil {
		return err
	}
	wall, err := p.GetWall(ctx, apiUUID(existing.WallID))
	if err != nil {
		return err
	}
	offset, width, height, sill := existing.Offset, existing.Width, existing.Height, existing.Sill
	if in.OffsetSet {
		offset = in.Offset
	}
	if in.WidthSet {
		width = in.Width
	}
	if in.HeightSet {
		height = in.Height
	}
	if in.SillSet {
		sill = in.Sill
	}
	length := math.Hypot(wall.BX-wall.AX, wall.BY-wall.AY)
	if offset < 0 || width <= 0 || offset+width > length+0.5 {
		return ErrOpeningTooWide
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.queries.WithTx(tx)
	if _, err := q.UpdateOpening(ctx, storegen.UpdateOpeningParams{
		ID:     pgtype.UUID{Bytes: id, Valid: true},
		Offset: offset,
		Width:  width,
		Height: height,
		Sill:   sill,
	}); err != nil {
		return err
	}
	if _, err := q.BumpPlanVersion(ctx); err != nil {
		return fmt.Errorf("递增版本号: %w", err)
	}
	return tx.Commit(ctx)
}

// DeleteOpening 幂等删除；真的删掉了才 +1 版本号。
func (p *PGPlan) DeleteOpening(ctx context.Context, id openapi_types.UUID) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.queries.WithTx(tx)
	rows, err := q.DeleteOpening(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		if _, err := q.BumpPlanVersion(ctx); err != nil {
			return 0, fmt.Errorf("递增版本号: %w", err)
		}
	}
	return rows, tx.Commit(ctx)
}

// repositionWallItems 在墙改完之后重算挂墙物品的世界坐标与朝向。
// 物品的 x/y/rotation 是派生值，唯一真相是 (wall_id, wall_offset)。
func repositionWallItems(ctx context.Context, q *storegen.Queries, wallID openapi_types.UUID, in WallInput) error {
	items, err := q.ListItemsOnWall(ctx, pgtype.UUID{Bytes: wallID, Valid: true})
	if err != nil {
		return err
	}
	wall := PlanWall{AX: in.AX, AY: in.AY, BX: in.BX, BY: in.BY}
	for _, it := range items {
		offset := 0.0
		if it.WallOffset.Valid {
			offset = it.WallOffset.Float64
		}
		x, y, rotation, clamped := placeOnWall(wall, offset, it.DefaultWidth)
		if err := q.UpdateItemPlacement(ctx, storegen.UpdateItemPlacementParams{
			ID:         it.ID,
			X:          x,
			Y:          y,
			Rotation:   rotation,
			WallOffset: pgtype.Float8{Float64: clamped, Valid: true},
		}); err != nil {
			return err
		}
	}
	return nil
}

// clampWallOpenings 把墙上的门窗夹回新的墙长范围内。
// 墙变长时不动它们（偏移仍然有效），变短时才夹。
func clampWallOpenings(ctx context.Context, q *storegen.Queries, wallID openapi_types.UUID, in WallInput) error {
	rows, err := q.ListOpeningsOnWall(ctx, pgtype.UUID{Bytes: wallID, Valid: true})
	if err != nil {
		return err
	}
	length := math.Hypot(in.BX-in.AX, in.BY-in.AY)
	for _, row := range rows {
		maxOffset := math.Max(0, length-row.Width)
		if row.Offset <= maxOffset {
			continue
		}
		if err := q.UpdateOpeningOffset(ctx, storegen.UpdateOpeningOffsetParams{
			ID: row.ID, Offset: maxOffset,
		}); err != nil {
			return err
		}
	}
	return nil
}

// LoadUnderlay 读底图元数据；没有底图返回 nil, nil 而不是报错。
func (p *PGPlan) LoadUnderlay(ctx context.Context) (*PlanUnderlay, error) {
	row, err := p.queries.GetUnderlay(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &PlanUnderlay{
		ContentType: row.ContentType,
		WidthPx:     int(row.WidthPx),
		HeightPx:    int(row.HeightPx),
		Sha256:      row.Sha256,
		CmPerPx:     row.CmPerPx,
		OffsetXCm:   row.OffsetXCm,
		OffsetYCm:   row.OffsetYCm,
		RotationDeg: int(row.RotationDeg),
		Opacity:     row.Opacity,
		Visible:     row.Visible,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

// LoadUnderlayImage 单独取字节。
func (p *PGPlan) LoadUnderlayImage(ctx context.Context) (string, []byte, error) {
	row, err := p.queries.GetUnderlayImage(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, err
	}
	return row.ContentType, row.Bytes, nil
}

// SaveUnderlay 整张替换；换图等于换定标，参数一并重置。
func (p *PGPlan) SaveUnderlay(ctx context.Context, in UnderlayUpload) error {
	return p.queries.UpsertUnderlay(ctx, storegen.UpsertUnderlayParams{
		ContentType: in.ContentType,
		WidthPx:     int32(in.WidthPx),
		HeightPx:    int32(in.HeightPx),
		Bytes:       in.Bytes,
		Sha256:      in.Sha256,
		CmPerPx:     in.CmPerPx,
		OffsetXCm:   in.OffsetXCm,
		OffsetYCm:   in.OffsetYCm,
	})
}

// UpdateUnderlayMeta 合并补丁后写回，返回新元数据。
func (p *PGPlan) UpdateUnderlayMeta(ctx context.Context, patch UnderlayMetaPatch) (*PlanUnderlay, error) {
	current, err := p.LoadUnderlay(ctx)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrUnderlayMissing
	}
	next := *current
	if patch.CmPerPx != nil {
		next.CmPerPx = *patch.CmPerPx
	}
	if patch.OffsetXCm != nil {
		next.OffsetXCm = *patch.OffsetXCm
	}
	if patch.OffsetYCm != nil {
		next.OffsetYCm = *patch.OffsetYCm
	}
	if patch.RotationDeg != nil {
		next.RotationDeg = *patch.RotationDeg
	}
	if patch.Opacity != nil {
		next.Opacity = *patch.Opacity
	}
	if patch.Visible != nil {
		next.Visible = *patch.Visible
	}
	if err := p.queries.UpdateUnderlayMeta(ctx, storegen.UpdateUnderlayMetaParams{
		CmPerPx:     next.CmPerPx,
		OffsetXCm:   next.OffsetXCm,
		OffsetYCm:   next.OffsetYCm,
		RotationDeg: int32(next.RotationDeg),
		Opacity:     next.Opacity,
		Visible:     next.Visible,
	}); err != nil {
		return nil, err
	}
	return p.LoadUnderlay(ctx)
}

// DeleteUnderlay 删掉唯一那一行。
func (p *PGPlan) DeleteUnderlay(ctx context.Context) (int64, error) {
	return p.queries.DeleteUnderlay(ctx)
}
