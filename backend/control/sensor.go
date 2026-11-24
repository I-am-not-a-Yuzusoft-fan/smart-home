package control

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

type SensorReporter struct {
	pool        *pgxpool.Pool
	broadcaster Broadcaster

	// mu 保护 simulatedAt：5 秒上报 goroutine 与 HTTP tick handler 会并发访问。
	mu          sync.Mutex
	simulatedAt time.Time
}

func NewSensorReporter(pool *pgxpool.Pool, broadcaster Broadcaster) *SensorReporter {
	return &SensorReporter{pool: pool, broadcaster: broadcaster, simulatedAt: time.Now()}
}

func (r *SensorReporter) Report(ctx context.Context) error {
	return r.apply(ctx, r.now())
}

func (r *SensorReporter) Advance(ctx context.Context, minutes int) (time.Time, error) {
	r.mu.Lock()
	r.simulatedAt = r.simulatedAt.Add(time.Duration(minutes) * time.Minute)
	simTime := r.simulatedAt
	r.mu.Unlock()
	err := r.apply(ctx, simTime)
	return simTime, err
}

// now 是模拟时钟的读入口；所有派生量都必须通过它取时间。
func (r *SensorReporter) now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.simulatedAt
}

func (r *SensorReporter) apply(ctx context.Context, simTime time.Time) error {
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
			// 日内正弦：9 点 25℃，15 点 28℃，凌晨 3 点 22℃；每小时约 0.8℃，
			// 这样 tick 一小时在任一时段都能看出变化（旧实现按「当天第几分钟」线性衰减，白天会饱和）。
			temp := math.Round(dailyCurveAt(simTime, 25, 3)*10) / 10
			humidity := math.Round(dailyCurveAt(simTime, 55, -12)*10) / 10
			updates = append(updates,
				sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "temperature", value: temp},
				sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "humidity", value: humidity},
			)
		case "sensor_lux":
			updates = append(updates, sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "illuminance", value: illuminanceAt(simTime)})
		case "sensor_pir":
			updates = append(updates, sensorUpdate{itemID: uuid.UUID(item.ID.Bytes), key: "occupied", value: occupiedWindowAt(simTime)})
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txq := q.WithTx(tx)
	for _, update := range updates {
		value, _ := json.Marshal(update.value)
		same := false
		current, err := txq.GetState(ctx, storegen.GetStateParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
		})
		if err == nil {
			var old any
			if json.Unmarshal(current.Value, &old) == nil {
				same = jsonEqual(old, update.value)
			}
		}
		// 即使读数没变也要刷新 updated_at：否则「最近更新时间」会一直停在首次上报，
		// 前端的陈旧提示会把稳定的传感器误判成掉线。
		if err := txq.UpsertState(ctx, storegen.UpsertStateParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
			Value: value, Source: "device", UpdatedAt: pgTime(now),
		}); err != nil {
			return err
		}
		if same {
			continue
		}
		if err := txq.InsertHistory(ctx, storegen.InsertHistoryParams{
			ItemID: pgUUID(update.itemID), CapabilityKey: update.key,
			Value: value, Source: "device", RecordedAt: pgTime(now),
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// 读数未变时不写 state_history（只存变化），但仍推送刷新后的 ts 给前端，
	// 否则前端 liveStates 的 ts 超过 60 秒不刷新会把所有稳定传感器误判为不可用。
	if r.broadcaster != nil {
		for _, update := range updates {
			r.broadcaster.Broadcast(StateMessage{
				ItemID: update.itemID, CapabilityKey: update.key,
				Value: update.value, Source: "device", UpdatedAt: now,
			})
		}
	}
	return nil
}

type sensorUpdate struct {
	itemID uuid.UUID
	key    string
	value  any
}

func hourOfDayAt(t time.Time) float64 {
	return float64(t.Hour()) + float64(t.Minute())/60
}

// illuminanceAt 按 6–19 点的日照正弦给照度，夜间 20 lux。
func illuminanceAt(t time.Time) int {
	hour := hourOfDayAt(t)
	if hour < 6 || hour >= 19 {
		return 20
	}
	return int(math.Round(200 + 1000*math.Sin(math.Pi*(hour-6)/13)))
}

// dailyCurveAt 返回 mean + amp*sin(2π(h-9)/24)：9 点取 mean，15 点取 mean+amp，3 点取 mean-amp。
func dailyCurveAt(t time.Time, mean, amp float64) float64 {
	return mean + amp*math.Sin(2*math.Pi*(hourOfDayAt(t)-9)/24)
}

func occupiedWindowAt(t time.Time) bool {
	hour := t.Hour()
	return hour >= 7 && hour <= 22
}

func (r *SensorReporter) illuminance() int {
	return illuminanceAt(r.now())
}

func (r *SensorReporter) dailyCurve(mean, amp float64) float64 {
	return dailyCurveAt(r.now(), mean, amp)
}

// simulatedHour 供测试与 PIR 窗口使用（整点）。
func (r *SensorReporter) simulatedHour() int {
	return r.now().Hour()
}

func (r *SensorReporter) occupiedWindow() bool {
	return occupiedWindowAt(r.now())
}

func jsonEqual(a, b any) bool {
	av, _ := json.Marshal(a)
	bv, _ := json.Marshal(b)
	return string(av) == string(bv)
}
