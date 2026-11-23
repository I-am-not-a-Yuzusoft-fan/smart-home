package control

import (
	"fmt"
	"math"
	"testing"
)

func TestArcPolylineSagittaAndEndpoints(t *testing.T) {
	a := Point{ID: "a", X: 0, Y: 0}
	b := Point{ID: "b", X: 100, Y: 0}
	pts := ArcPolyline(a, b, 60)
	if len(pts) < 2 {
		t.Fatalf("60° 弧至少要离散成 2 段，实际 %d", len(pts))
	}
	// 弦长 100、圆心角 60° → 半径 100，矢高 = r(1-cos30°) ≈ 13.4
	// 屏幕坐标 y 向下，正弧度往弦的左侧（向上）弯。
	mid := pts[len(pts)/2]
	if math.Abs(mid.Y-(-13.4)) > 0.3 {
		t.Fatalf("矢高应在 -13.4 附近，实际 y=%.2f", mid.Y)
	}
	if math.Abs(pts[0].X-7.74) > 0.2 || math.Abs(pts[len(pts)-1].X-92.26) > 0.2 {
		t.Fatalf("首末采样点应在 5° 步进处（7.74 / 92.26），实际 %v %v", pts[0], pts[len(pts)-1])
	}
	// 圆心 (50, 86.6)、半径 100，所有采样点都落在这个圆上（1 的浮点容差）。
	for _, p := range pts {
		if d := math.Abs(math.Hypot(p.X-50, p.Y-86.6) - 100); d > 1 {
			t.Fatalf("采样点不在圆上（偏差 %.2f）：%v", d, p)
		}
	}
}

func TestArcPolylineStraightIsEmpty(t *testing.T) {
	if pts := ArcPolyline(Point{X: 0, Y: 0}, Point{X: 100, Y: 0}, 0); pts != nil {
		t.Fatalf("直墙不该产生采样点，实际 %v", pts)
	}
	if pts := ArcPolyline(Point{X: 0, Y: 0}, Point{X: 0, Y: 0}, 90); pts != nil {
		t.Fatalf("重合端点不该产生采样点")
	}
}

func TestDeriveArcRoomArea(t *testing.T) {
	// 400×300 的矩形，顶边换成 60° 弧：面积 = 矩形 + 弓形。
	// 弓形 = r²(θ-sinθ)/2，r=400、θ=60° → 400²*(1.0472-0.8660)/2 ≈ 14496 cm² ≈ 1.45 ㎡。
	corners := []Point{
		{ID: "a", X: 0, Y: 0}, {ID: "b", X: 400, Y: 0},
		{ID: "c", X: 400, Y: 300}, {ID: "d", X: 0, Y: 300},
	}
	pts := ArcPolyline(corners[0], corners[1], 60)
	if len(pts) < 2 {
		t.Fatal("没有采样点")
	}
	ids := make([]string, 0, len(pts))
	extra := make([]Point, 0, len(pts))
	for i, p := range pts {
		id := "arc" + string(rune('a'+i))
		ids = append(ids, id)
		extra = append(extra, Point{ID: id, X: p.X, Y: p.Y})
	}
	corners = append(corners, extra...)
	walls := []Wall{{A: "a", B: ids[0]}}
	for i := 1; i < len(ids); i++ {
		walls = append(walls, Wall{A: ids[i-1], B: ids[i]})
	}
	walls = append(walls,
		Wall{A: ids[len(ids)-1], B: "b"},
		Wall{A: "b", B: "c"}, Wall{A: "c", B: "d"}, Wall{A: "d", B: "a"},
	)
	res := Derive(corners, walls)
	if len(res.Rings) != 1 {
		t.Fatalf("应推出 1 个房间，实际 %d", len(res.Rings))
	}
	got := res.Rings[0].AreaM2
	if math.Abs(got-13.45) > 0.1 {
		t.Fatalf("带 60° 弧的房间面积应在 13.45 ㎡ 附近，实际 %.2f", got)
	}
}

// 半圆阳台：带门的弦墙与半圆弧共用一对端点，应当推出两个房间——矩形 12 ㎡、透镜 ≈6.28 ㎡。
// 这是「墙的身份 = 端点对 + 弧度」存在的理由：同端点的直墙与弧墙是两面墙。
func TestDeriveChordAndArcMakeTwoRooms(t *testing.T) {
	corners := []Point{
		{ID: "a", X: 0, Y: 0},
		{ID: "b", X: 400, Y: 0},
		{ID: "c", X: 400, Y: 300},
		{ID: "d", X: 0, Y: 300},
	}
	walls := []Wall{{A: "a", B: "b"}, {A: "b", B: "c"}, {A: "c", B: "d"}, {A: "d", B: "a"}}

	// 半圆弧把弦 a-b 向上（屏幕 -y）撑成半圆，端点仍是 a 与 b。
	mid := ArcPolyline(corners[0], corners[1], 180)
	prev := "a"
	for i, p := range mid {
		id := fmt.Sprintf("arc%d", i)
		corners = append(corners, Point{ID: id, X: p.X, Y: p.Y})
		walls = append(walls, Wall{A: prev, B: id})
		prev = id
	}
	walls = append(walls, Wall{A: prev, B: "b"})

	res := Derive(corners, walls)
	if len(res.Rings) != 2 {
		t.Fatalf("半圆阳台应推出两个房间，实际 %d 个：%+v", len(res.Rings), res.Rings)
	}
	areas := []float64{res.Rings[0].AreaM2, res.Rings[1].AreaM2}
	// 矩形 4×3=12 ㎡；半圆 π·2²/2 ≈ 6.28 ㎡（5° 采样略小）。
	var rect, lens bool
	for _, a := range areas {
		if math.Abs(a-12) < 0.05 {
			rect = true
		}
		if math.Abs(a-6.27) < 0.05 {
			lens = true
		}
	}
	if !rect || !lens {
		t.Fatalf("应得到 12 ㎡ 与 ≈6.27 ㎡ 两个房间，实际 %v", areas)
	}
}
