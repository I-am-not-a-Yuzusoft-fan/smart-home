package device

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSimulatorAdapterUpdateConfig(t *testing.T) {
	adapter := NewSimulatorAdapter(5*time.Millisecond, 0)
	if cfg := adapter.Config(); cfg.DelayMs != 5 || cfg.FailureRate != 0 {
		t.Fatalf("unexpected initial config: %+v", cfg)
	}

	val, err := adapter.Dispatch(context.Background(), Command{
		RequestID:     uuid.New(),
		ItemID:        uuid.New(),
		CapabilityKey: "on",
		Value:         true,
	})
	if err != nil || val != true {
		t.Fatalf("expected success at failureRate=0, got val=%v err=%v", val, err)
	}

	updated := adapter.UpdateConfig(0, 1.0)
	if updated.DelayMs != 0 || updated.FailureRate != 1.0 {
		t.Fatalf("unexpected updated config: %+v", updated)
	}

	_, err = adapter.Dispatch(context.Background(), Command{
		RequestID:     uuid.New(),
		ItemID:        uuid.New(),
		CapabilityKey: "on",
		Value:         false,
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable at failureRate=1, got %v", err)
	}
}
