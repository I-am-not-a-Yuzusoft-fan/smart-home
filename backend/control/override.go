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

// OverrideKey 精确到「某设备某能力」粒度（FR-5.9）。
type OverrideKey struct {
	ItemID        uuid.UUID
	CapabilityKey string
}

// OverrideEntry 记录一次手动接管的时刻与当时房间环境基线读数。
type OverrideEntry struct {
	ManualAt    time.Time
	HasBaseline bool
	Baseline    policy.EnvReadings
}

// OverrideStore 进程内保存手动接管让位记录（单实例假设，零 DDL 变更）。
type OverrideStore struct {
	mu      sync.RWMutex
	entries map[OverrideKey]OverrideEntry
	nowFn   func() time.Time
}

func NewOverrideStore() *OverrideStore {
	return &OverrideStore{
		entries: make(map[OverrideKey]OverrideEntry),
		nowFn:   time.Now,
	}
}

func (s *OverrideStore) SetNowFunc(fn func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fn == nil {
		s.nowFn = time.Now
		return
	}
	s.nowFn = fn
}

func (s *OverrideStore) Now() time.Time {
	s.mu.RLock()
	fn := s.nowFn
	s.mu.RUnlock()
	return fn()
}

func (s *OverrideStore) RecordManual(itemID uuid.UUID, capabilityKey string, baseline policy.EnvReadings, hasBaseline bool, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[OverrideKey{ItemID: itemID, CapabilityKey: capabilityKey}] = OverrideEntry{
		ManualAt:    at,
		HasBaseline: hasBaseline,
		Baseline:    baseline,
	}
}

func (s *OverrideStore) Get(itemID uuid.UUID, capabilityKey string) (OverrideEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[OverrideKey{ItemID: itemID, CapabilityKey: capabilityKey}]
	return e, ok
}

func (s *OverrideStore) Clear(itemID uuid.UUID, capabilityKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, OverrideKey{ItemID: itemID, CapabilityKey: capabilityKey})
}

func (s *OverrideStore) ClearMany(keys []OverrideKey) {
	if len(keys) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.entries, k)
	}
}

// readRoomEnvReadings 读取指定房间（若无则回退到平面图首个对应传感器）的温度、湿度与照度快照。
func readRoomEnvReadings(ctx context.Context, pool *pgxpool.Pool, roomHash pgtype.Text) (policy.EnvReadings, bool) {
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
			if fallback == nil {
				fallback = &id
			}
			if roomHash.Valid && it.RoomHash.Valid && it.RoomHash.String == roomHash.String {
				return &id
			}
		}
		return fallback
	}

	var out policy.EnvReadings
	var hasAny bool
	if thID := pickSensor("sensor_th"); thID != nil {
		if states, err := q.ListStatesByItem(ctx, pgUUID(*thID)); err == nil {
			for _, st := range states {
				var v any
				if err := json.Unmarshal(st.Value, &v); err != nil || v == nil || v == "unavailable" {
					continue
				}
				if f, ok := toFloat64(v); ok {
					switch st.CapabilityKey {
					case "temperature":
						val := f
						out.Temperature = &val
						hasAny = true
					case "humidity":
						val := f
						out.Humidity = &val
						hasAny = true
					}
				}
			}
		}
	}
	if luxID := pickSensor("sensor_lux"); luxID != nil {
		if states, err := q.ListStatesByItem(ctx, pgUUID(*luxID)); err == nil {
			for _, st := range states {
				if st.CapabilityKey != "illuminance" {
					continue
				}
				var v any
				if err := json.Unmarshal(st.Value, &v); err != nil || v == nil || v == "unavailable" {
					continue
				}
				if f, ok := toFloat64(v); ok {
					val := f
					out.Illuminance = &val
					hasAny = true
				}
			}
		}
	}
	return out, hasAny
}
