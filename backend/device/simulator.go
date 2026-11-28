package device

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("DEVICE_UNAVAILABLE")
	ErrTimeout     = errors.New("DEVICE_TIMEOUT")
)

type Command struct {
	RequestID     uuid.UUID
	ItemID        uuid.UUID
	CapabilityKey string
	Value         any
	Source        string
	Driver        string
}

type Adapter interface {
	Dispatch(ctx context.Context, command Command) (any, error)
}

type SimulatorConfig struct {
	DelayMs     int     `json:"delayMs"`
	FailureRate float64 `json:"failureRate"`
}

type SimulatorAdapter struct {
	mu          sync.RWMutex
	Delay       time.Duration
	FailureRate float64
}

func NewSimulatorAdapter(delay time.Duration, failureRate float64) *SimulatorAdapter {
	return &SimulatorAdapter{Delay: delay, FailureRate: failureRate}
}

func (a *SimulatorAdapter) Config() SimulatorConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return SimulatorConfig{
		DelayMs:     int(a.Delay.Milliseconds()),
		FailureRate: a.FailureRate,
	}
}

func (a *SimulatorAdapter) UpdateConfig(delay time.Duration, failureRate float64) SimulatorConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Delay = delay
	a.FailureRate = failureRate
	return SimulatorConfig{
		DelayMs:     int(a.Delay.Milliseconds()),
		FailureRate: a.FailureRate,
	}
}

func (a *SimulatorAdapter) Dispatch(ctx context.Context, command Command) (any, error) {
	a.mu.RLock()
	delay := a.Delay
	failureRate := a.FailureRate
	a.mu.RUnlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ErrTimeout
		case <-timer.C:
		}
	}
	if failureRate > 0 && rand.Float64() < failureRate {
		return nil, ErrUnavailable
	}
	return command.Value, nil
}
