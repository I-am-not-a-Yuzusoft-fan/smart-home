package control

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/policy"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// YieldKey 精确到「某设备某能力」粒度（FR-5.9）。
type YieldKey struct {
	ItemID        uuid.UUID
	CapabilityKey string
}

// YieldEntry 记录一次手动接管的时刻与当时所在房间的环境基线读数。
type YieldEntry struct {
	ManualAt    time.Time
	HasBaseline bool
	Baseline    policy.EnvReadings
}

// YieldStore 进程内保存手动接管让位窗口记录（单实例假设，零 DDL 变更）。
type YieldStore struct {
	mu      sync.RWMutex
	entries map[YieldKey]YieldEntry
	nowFn   func() time.Time
}

func NewYieldStore() *YieldStore {
	return &YieldStore{
		entries: make(map[YieldKey]YieldEntry),
		nowFn:   time.Now,
	}
}

func (s *YieldStore) SetNowFunc(fn func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn == nil {
		s.nowFn = time.Now
		return
	}
	s.nowFn = fn
}

func (s *YieldStore) Now() time.Time {
	s.mu.RLock()
	fn := s.nowFn
	s.mu.RUnlock()
	return fn()
}

func (s *YieldStore) RecordManual(itemID uuid.UUID, capabilityKey string, baseline policy.EnvReadings, hasBaseline bool, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[YieldKey{ItemID: itemID, CapabilityKey: capabilityKey}] = YieldEntry{
		ManualAt:    at,
		HasBaseline: hasBaseline,
		Baseline:    baseline,
	}
}

func (s *YieldStore) Get(itemID uuid.UUID, capabilityKey string) (YieldEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[YieldKey{ItemID: itemID, CapabilityKey: capabilityKey}]
	return e, ok
}

func (s *YieldStore) Clear(itemID uuid.UUID, capabilityKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, YieldKey{ItemID: itemID, CapabilityKey: capabilityKey})
}

func (s *YieldStore) ClearMany(keys []YieldKey) {
	if len(keys) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.entries, k)
	}
}

// readRoomEnvReadings 读取指定房间的温度、湿度与照度快照。
// allowFallback=false 时严格限定本房间（用于手动让位基线，避免把别的房间传感器绑定到无传感器的房间）。
func readRoomEnvReadings(ctx context.Context, pool *pgxpool.Pool, roomHash pgtype.Text, allowFallback bool) (policy.EnvReadings, bool) {
	q := storegen.New(pool)
	items, err := q.ListItems(ctx, storegen.ListItemsParams{})
	if err != nil {
		return policy.EnvReadings{}, false
	}
	pickSensor := func(typeKey string) *uuid.UUID {
		var fallback *uuid.UUID
		for _, it := range items {
			if it.TypeKey != typeKey {
				continue
			}
			id := uuid.UUID(it.ID.Bytes)
			if allowFallback && fallback == nil {
				fallback = &id
			}
			if roomHash.Valid && it.RoomHash.Valid && it.RoomHash.String == roomHash.String {
				return &id
			}
		}
		return fallback
	}

	readNumericStates := func(sensorID *uuid.UUID) map[string]float64 {
		res := map[string]float64{}
		if sensorID == nil {
			return res
		}
		states, err := q.ListStatesByItem(ctx, pgUUID(*sensorID))
		if err != nil {
			return res
		}
		for _, st := range states {
			var v any
			if err := json.Unmarshal(st.Value, &v); err != nil || v == nil || v == "unavailable" {
				continue
			}
			if f, ok := toFloat64(v); ok {
				res[st.CapabilityKey] = f
			}
		}
		return res
	}

	var out policy.EnvReadings
	var hasAny bool
	th := readNumericStates(pickSensor("sensor_th"))
	if v, ok := th["temperature"]; ok {
		out.Temperature = &v
		hasAny = true
	}
	if v, ok := th["humidity"]; ok {
		out.Humidity = &v
		hasAny = true
	}
	lux := readNumericStates(pickSensor("sensor_lux"))
	if v, ok := lux["illuminance"]; ok {
		out.Illuminance = &v
		hasAny = true
	}
	return out, hasAny
}
