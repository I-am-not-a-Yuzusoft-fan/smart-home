package control

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

type SensorReporter struct {
	pool        *pgxpool.Pool
	broadcaster Broadcaster
	simulatedAt time.Time
}

func NewSensorReporter(pool *pgxpool.Pool, broadcaster Broadcaster) *SensorReporter {
	return &SensorReporter{pool: pool, broadcaster: broadcaster, simulatedAt: time.Now()}
}

func (r *SensorReporter) Report(ctx context.Context) error {
	_, err := r.Advance(ctx, 0)
	return err
}

func (r *SensorReporter) Advance(ctx context.Context, minutes int) (time.Time, error) {
	r.simulatedAt = r.simulatedAt.Add(time.Duration(minutes) * time.Minute)
	now := time.Now()
	q := storegen.New(r.pool)
	updates := []struct {
		itemID uuid.UUID
		key    string
		value  any
	}{
		{uuid.MustParse("00000000-0000-4000-8000-000000000304"), "temperature", math.Round((31.5-math.Min(6, float64(minutes)/20))*10) / 10},
		{uuid.MustParse("00000000-0000-4000-8000-000000000304"), "humidity", math.Round(math.Max(35, 68-float64(minutes)/10)*10) / 10},
		{uuid.MustParse("00000000-0000-4000-8000-000000000305"), "illuminance", illumAtMinute(minutes)},
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return r.simulatedAt, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txq := q.WithTx(tx)
	for _, update := range updates {
		value, _ := json.Marshal(update.value)
		if err := txq.UpsertState(ctx, storegen.UpsertStateParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
			Value: value, Source: "device", UpdatedAt: pgTime(now),
		}); err != nil {
			return r.simulatedAt, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return r.simulatedAt, err
	}
	for _, update := range updates {
		r.broadcaster.Broadcast(StateMessage{
			ItemID: update.itemID, CapabilityKey: update.key,
			Value: update.value, Source: "device", UpdatedAt: now,
		})
	}
	return r.simulatedAt, nil
}

func illumAtMinute(minutes int) int {
	hour := (rMinute(minutes) / 60) % 24
	if hour >= 7 && hour <= 18 {
		return 1200
	}
	return 30
}

var rMinute = func(minutes int) int {
	return (9*60 + minutes) % (24 * 60)
}
