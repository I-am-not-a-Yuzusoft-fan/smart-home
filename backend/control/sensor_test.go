package control

import (
	"math"
	"testing"
	"time"
)

func at(hour, minute int) *SensorReporter {
	return &SensorReporter{simulatedAt: time.Date(2026, 11, 23, hour, minute, 0, 0, time.Local)}
}

func TestSensorTimeDerivation(t *testing.T) {
	r := at(9, 0)
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

// 每次 tick 一小时都必须让读数发生变化：白天尤其容易出现「tick 了但数值不动」。
func TestSensorReadingsChangeEveryHour(t *testing.T) {
	for hour := 0; hour < 24; hour++ {
		cur := at(hour, 0)
		next := at(hour+1, 0)
		dTemp := math.Abs(next.dailyCurve(25, 3) - cur.dailyCurve(25, 3))
		dHum := math.Abs(next.dailyCurve(55, -12) - cur.dailyCurve(55, -12))
		if dTemp < 0.1 && dHum < 0.1 {
			t.Fatalf("%02d:00 → %02d:00 温湿度都不变（ΔT=%.3f ΔH=%.3f）", hour, hour+1, dTemp, dHum)
		}
	}
}

func TestSensorCurvesTakeExpectedValues(t *testing.T) {
	if got := at(9, 0).dailyCurve(25, 3); math.Abs(got-25) > 1e-9 {
		t.Fatalf("9 点温度曲线应为 25，实际 %.3f", got)
	}
	if got := at(15, 0).dailyCurve(25, 3); math.Abs(got-28) > 1e-9 {
		t.Fatalf("15 点温度曲线应为 28，实际 %.3f", got)
	}
	if got := at(3, 0).dailyCurve(55, -12); math.Abs(got-67) > 1e-9 {
		t.Fatalf("3 点湿度曲线应为 67，实际 %.3f", got)
	}
}

func TestIlluminanceFollowsDaylight(t *testing.T) {
	if got := at(12, 0).illuminance(); got < 900 {
		t.Fatalf("正午照度应接近 1200，实际 %d", got)
	}
	if got := at(2, 0).illuminance(); got != 20 {
		t.Fatalf("凌晨照度应为 20，实际 %d", got)
	}
	if at(5, 0).illuminance() != 20 || at(20, 0).illuminance() != 20 {
		t.Fatal("日出前/日落后都应为 20 lux")
	}
}
