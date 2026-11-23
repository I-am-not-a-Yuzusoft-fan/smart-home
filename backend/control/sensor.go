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
	return r.apply(ctx)
}

func (r *SensorReporter) Advance(ctx context.Context, minutes int) (time.Time, error) {
	r.simulatedAt = r.simulatedAt.Add(time.Duration(minutes) * time.Minute)
	err := r.apply(ctx)
	return r.simulatedAt, err
}

func (r *SensorReporter) apply(ctx context.Context) error {
	now := time.Now()
	q := storegen.New(r.pool)
	items, err := q.ListItems(ctx, storegen.ListItemsParams{})
	if err != nil {
		return err
	}

	updates := make([]sensorUpdate, 0)
	for _, item := range items {
		switch item.TypeKey {
		case "sensor_th":
			minute := r.simulatedMinute()
			temp := math.Round((31.5-math.Min(6, float64(minute)/20))*10) / 10
			humidity := math.Round(math.Max(35, 68-float64(minute)/10)*10) / 10
			updates = append(updates,
				sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "temperature", value: temp},
				sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "humidity", value: humidity},
			)
		case "sensor_lux":
			hour := r.simulatedHour()
			lux := 30
			if hour >= 7 && hour <= 18 {
				lux = 1200
			}
			updates = append(updates, sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "illuminance", value: lux})
		case "sensor_pir":
			occupied := r.occupiedWindow()
			updates = append(updates, sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "occupied", value: occupied})
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txq := q.WithTx(tx)
	changed := make([]sensorUpdate, 0, len(updates))
	for _, update := range updates {
		value, _ := json.Marshal(update.value)
		current, err := txq.GetState(ctx, storegen.GetStateParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
		})
		if err == nil {
			var old any
			if json.Unmarshal(current.Value, &old) == nil {
				if jsonEqual(old, update.value) {
					continue
				}
			}
		}
		if err := txq.UpsertState(ctx, storegen.UpsertStateParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
			Value: value, Source: "device", UpdatedAt: pgTime(now),
		}); err != nil {
			return err
		}
		if err := txq.InsertHistory(ctx, storegen.InsertHistoryParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
			Value: value, Source: "device", RecordedAt: pgTime(now),
		}); err != nil {
			return err
		}
		changed = append(changed, update)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, update := range changed {
		r.broadcaster.Broadcast(StateMessage{
			ItemID: update.itemID, CapabilityKey: update.key,
			Value: update.value, Source: "device", UpdatedAt: now,
		})
	}
	return nil
}

type sensorUpdate struct {
	itemID uuid.UUID
	key    string
	value  any
}

func (r *SensorReporter) simulatedMinute() int {
	hour := r.simulatedAt.Hour()
	minute := r.simulatedAt.Minute()
	return hour*60 + minute
}

func (r *SensorReporter) simulatedHour() int {
	return r.simulatedAt.Hour()
}

func (r *SensorReporter) occupiedWindow() bool {
	hour := r.simulatedHour()
	return hour >= 7 && hour <= 22
}

func jsonEqual(a, b any) bool {
	av, _ := json.Marshal(a)
	bv, _ := json.Marshal(b)
	return string(av) == string(bv)
}
