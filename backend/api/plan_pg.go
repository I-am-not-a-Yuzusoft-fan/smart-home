package api

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgconn"
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
			return tx.Commit(ctx)
		}
		return err
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
