package control

import (
	"sync"
	"time"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/policy"
)

type SimEnvSnapshot struct {
	Season          string
	EffectiveSeason policy.Season
	OutdoorTemp     *float64
	OutdoorHumidity *float64
	UpdatedAt       time.Time
}

// SimEnvStore 保存进程内仿真季节与室外环境设定，供 /api/sim/env 与 ModeRunner 共享。
type SimEnvStore struct {
	mu              sync.RWMutex
	season          string
	outdoorTemp     *float64
	outdoorHumidity *float64
	updatedAt       time.Time
}

func NewSimEnvStore() *SimEnvStore {
	return &SimEnvStore{season: "auto"}
}

func (s *SimEnvStore) snapshotLocked(now time.Time) SimEnvSnapshot {
	return SimEnvSnapshot{
		Season:          s.season,
		EffectiveSeason: policy.ResolveSeason(s.season, now),
		OutdoorTemp:     cloneFloatPtr(s.outdoorTemp),
		OutdoorHumidity: cloneFloatPtr(s.outdoorHumidity),
		UpdatedAt:       s.updatedAt,
	}
}

func (s *SimEnvStore) Get(now time.Time) SimEnvSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked(now)
}

func (s *SimEnvStore) Set(season string, outdoorTemp, outdoorHumidity *float64, now time.Time) SimEnvSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if season == "" {
		season = "auto"
	}
	s.season = season
	s.outdoorTemp = cloneFloatPtr(outdoorTemp)
	s.outdoorHumidity = cloneFloatPtr(outdoorHumidity)
	s.updatedAt = now
	return s.snapshotLocked(now)
}

func cloneFloatPtr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
