package api

import (
	"math"
	"testing"
)

// TestFitUnderlayCentersInsideBounds 锁的是"铺满包围盒并居中"的算术：
// 比例取两轴里更紧的那个，图片中心落在包围盒中心。
func TestFitUnderlayCentersInsideBounds(t *testing.T) {
	scale, ox, oy := fitUnderlay(1000, 500, 0, 0, 400, 300)
	if math.Abs(scale-0.36) > 1e-9 {
		t.Fatalf("比例应为 0.36，实际 %v", scale)
	}
	// 图变成 360×180，居中后左上角是 (20, 60)。
	if math.Abs(ox-20) > 1e-9 || math.Abs(oy-60) > 1e-9 {
		t.Fatalf("左上角应为 (20, 60)，实际 (%v, %v)", ox, oy)
	}
}

func TestFitUnderlayFallsBackWhenPlanEmpty(t *testing.T) {
	scale, ox, oy := fitUnderlay(400, 300, 0, 0, 0, 0)
	if math.Abs(scale-0.9) > 1e-9 {
		t.Fatalf("空户型比例应为 0.9，实际 %v", scale)
	}
	if math.Abs(ox-20) > 1e-9 || math.Abs(oy-15) > 1e-9 {
		t.Fatalf("空户型应居中：期望 (20, 15)，实际 (%v, %v)", ox, oy)
	}
}

func TestDetectUnderlayRejectsText(t *testing.T) {
	if _, _, _, err := detectUnderlay([]byte("not an image")); err == nil {
		t.Fatal("纯文本应被拒绝")
	}
}
