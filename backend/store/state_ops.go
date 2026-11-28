package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// ListStateHistoryRange 在数据层执行带时间区间筛选的 state_history 查询。
func ListStateHistoryRange(
	ctx context.Context,
	pool *pgxpool.Pool,
	itemID uuid.UUID,
	capabilityKey *string,
	from, to *time.Time,
	limit int32,
) ([]storegen.StateHistory, error) {
	var keyArg pgtype.Text
	if capabilityKey != nil && *capabilityKey != "" {
		keyArg = pgtype.Text{String: *capabilityKey, Valid: true}
	}
	var fromArg, toArg pgtype.Timestamptz
	if from != nil {
		fromArg = pgtype.Timestamptz{Time: *from, Valid: true}
	}
	if to != nil {
		toArg = pgtype.Timestamptz{Time: *to, Valid: true}
	}
	rows, err := pool.Query(ctx, `
		SELECT id, item_id, capability_key, value, source, recorded_at
		FROM state_history
		WHERE item_id = $1
		  AND ($2::text IS NULL OR capability_key = $2)
		  AND ($3::timestamptz IS NULL OR recorded_at >= $3)
		  AND ($4::timestamptz IS NULL OR recorded_at <= $4)
		ORDER BY recorded_at DESC, id DESC
		LIMIT $5
	`, pgtype.UUID{Bytes: itemID, Valid: true}, keyArg, fromArg, toArg, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]storegen.StateHistory, 0)
	for rows.Next() {
		var r storegen.StateHistory
		if err := rows.Scan(&r.ID, &r.ItemID, &r.CapabilityKey, &r.Value, &r.Source, &r.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CleanupRetention 按 NFR-16 清理超过 30 天的 state_history 与超过 90 天的 command_log。
func CleanupRetention(ctx context.Context, pool *pgxpool.Pool) (int64, int64, error) {
	tagHist, err := pool.Exec(ctx, `DELETE FROM state_history WHERE recorded_at < now() - interval '30 days'`)
	if err != nil {
		return 0, 0, err
	}
	tagCmd, err := pool.Exec(ctx, `DELETE FROM command_log WHERE created_at < now() - interval '90 days'`)
	if err != nil {
		return tagHist.RowsAffected(), 0, err
	}
	return tagHist.RowsAffected(), tagCmd.RowsAffected(), nil
}
