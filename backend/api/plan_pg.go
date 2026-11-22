package api

import (
	"context"
	"errors"
	"fmt"

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
