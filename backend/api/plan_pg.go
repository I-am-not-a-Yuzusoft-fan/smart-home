package api

import (
	"context"
	"errors"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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

// DeleteWall 删墙并返回受影响行数；0 表示没有这面墙。
func (p *PGPlan) DeleteWall(ctx context.Context, id openapi_types.UUID) (int64, error) {
	return p.queries.DeleteWall(ctx, pgUUID(id))
}
