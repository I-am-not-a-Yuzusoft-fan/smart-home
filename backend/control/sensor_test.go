package control

import (
	"testing"
	"time"
)

func TestSensorTimeDerivation(t *testing.T) {
	r := &SensorReporter{simulatedAt: time.Date(2026, 11, 23, 9, 0, 0, 0, time.Local)}
	if r.simulatedHour() != 9 {
		t.Fatalf("应从模拟时间得到 9 点，实际 %d", r.simulatedHour())
	}
	if !r.occupiedWindow() {
		t.Fatal("9 点应有人")
	}
	r.simulatedAt = r.simulatedAt.Add(15 * time.Hour)
	if r.occupiedWindow() {
		t.Fatal("次日凌晨不应有人")
	}
}
