package device

import (
	"context"
	"errors"
	"math/rand"
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

type SimulatorAdapter struct {
	Delay       time.Duration
	FailureRate float64
}

func NewSimulatorAdapter(delay time.Duration, failureRate float64) *SimulatorAdapter {
	return &SimulatorAdapter{Delay: delay, FailureRate: failureRate}
}

func (a *SimulatorAdapter) Dispatch(ctx context.Context, command Command) (any, error) {
	timer := time.NewTimer(a.Delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ErrTimeout
	case <-timer.C:
	}
	if a.FailureRate > 0 && rand.Float64() < a.FailureRate {
		return nil, ErrUnavailable
	}
	return command.Value, nil
}
