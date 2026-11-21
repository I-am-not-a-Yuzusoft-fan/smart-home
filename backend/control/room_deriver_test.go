package control_test

import (
	"math"
	"testing"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
)

func pt(id string, x, y float64) control.Point { return control.Point{ID: id, X: x, Y: y} }

// rect 是一个 400×300 厘米的矩形，按顺时针（屏幕上）给四个端点。
func rect() ([]control.Point, []control.Wall) {
	corners := []control.Point{
		pt("a", 0, 0), pt("b", 400, 0), pt("c", 400, 300), pt("d", 0, 300),
	}
	walls := []control.Wall{
		{A: "a", B: "b"}, {A: "b", B: "c"}, {A: "c", B: "d"}, {A: "d", B: "a"},
	}
	return corners, walls
}

func TestDeriveClosedRectangle(t *testing.T) {
	corners, walls := rect()
	got := control.Derive(corners, walls)

	if len(got.Rings) != 1 {
		t.Fatalf("闭合矩形应推出 1 个房间，实际 %d", len(got.Rings))
	}
	if got.OpenWalls != 0 {
		t.Errorf("闭合矩形不该有未围合的墙，实际 %d", got.OpenWalls)
	}
	ring := got.Rings[0]
	if len(ring.Points) != 4 {
		t.Errorf("房间应有 4 个顶点，实际 %d", len(ring.Points))
	}
	if math.Abs(ring.AreaM2-12) > 0.01 {
		t.Errorf("400×300 厘米应 12 平方米，实际 %.2f", ring.AreaM2)
	}
}

func TestDeriveTwoRoomsSharingWall(t *testing.T) {
	// 两个房间共用中间一面墙：左 200×300、右 200×300。
	corners := []control.Point{
		pt("a", 0, 0), pt("b", 200, 0), pt("c", 400, 0),
		pt("d", 0, 300), pt("e", 200, 300), pt("f", 400, 300),
	}
	walls := []control.Wall{
		{A: "a", B: "b"}, {A: "b", B: "c"},
		{A: "c", B: "f"}, {A: "f", B: "e"}, {A: "e", B: "d"}, {A: "d", B: "a"},
		{A: "b", B: "e"}, // 中间隔断
	}
	got := control.Derive(corners, walls)

	if len(got.Rings) != 2 {
		t.Fatalf("两个房间应推出 2 个环，实际 %d", len(got.Rings))
	}
	for _, r := range got.Rings {
		if math.Abs(r.AreaM2-6) > 0.01 {
			t.Errorf("每个房间应 6 平方米，实际 %.2f", r.AreaM2)
		}
	}
}

// 矩形加一根伸进房间的隔断（T 形）：隔断不围出房间，也不该把矩形拆坏。
func TestDeriveTSpurDoesNotCreateRoom(t *testing.T) {
	corners, walls := rect()
	corners = append(corners, pt("e", 200, 150))
	walls = append(walls, control.Wall{A: "b", B: "e"})

	got := control.Derive(corners, walls)
	if len(got.Rings) != 1 {
		t.Fatalf("带悬空隔断的矩形仍应只有 1 个房间，实际 %d", len(got.Rings))
	}
	if got.OpenWalls != 1 {
		t.Errorf("悬空的隔断应计入未围合的墙，实际 %d", got.OpenWalls)
	}
}

// 少一面墙：不能凭空冒出房间，且要告诉调用方"有墙没围上"。
func TestDeriveOpenRectangleHasNoRoom(t *testing.T) {
	corners, walls := rect()
	got := control.Derive(corners, walls[:3])

	if len(got.Rings) != 0 {
		t.Fatalf("缺口矩形不该有房间，实际 %d", len(got.Rings))
	}
	if got.OpenWalls != 3 {
		t.Errorf("三面悬空墙都应计入未围合，实际 %d", got.OpenWalls)
	}
}

func TestDeriveEmpty(t *testing.T) {
	got := control.Derive(nil, nil)
	if len(got.Rings) != 0 || got.OpenWalls != 0 {
		t.Fatalf("空户型应无房间、无未围合墙，实际 %+v", got)
	}
}

// 哈希是房间命名的键：同一条环换个起点、换个方向，哈希必须不变。
func TestHashIsStableAcrossStartAndDirection(t *testing.T) {
	corners, walls := rect()
	ring := control.Derive(corners, walls).Rings[0]
	base := ring.Hash

	rotated := append(append([]control.Point{}, ring.Points[2:]...), ring.Points[:2]...)
	if got := control.Hash(rotated); got != base {
		t.Errorf("换起点后哈希应不变：%s != %s", got, base)
	}

	reversed := make([]control.Point, len(ring.Points))
	for i := range ring.Points {
		reversed[i] = ring.Points[len(ring.Points)-1-i]
	}
	if got := control.Hash(reversed); got != base {
		t.Errorf("反向绕行后哈希应不变：%s != %s", got, base)
	}
}

// 两个不相连的矩形各成一个房间。
func TestDeriveDisconnectedRooms(t *testing.T) {
	corners := []control.Point{
		pt("a", 0, 0), pt("b", 100, 0), pt("c", 100, 100), pt("d", 0, 100),
		pt("e", 500, 0), pt("f", 600, 0), pt("g", 600, 100), pt("h", 500, 100),
	}
	walls := []control.Wall{
		{A: "a", B: "b"}, {A: "b", B: "c"}, {A: "c", B: "d"}, {A: "d", B: "a"},
		{A: "e", B: "f"}, {A: "f", B: "g"}, {A: "g", B: "h"}, {A: "h", B: "e"},
	}
	got := control.Derive(corners, walls)
	if len(got.Rings) != 2 {
		t.Fatalf("两个独立矩形应推出 2 个房间，实际 %d", len(got.Rings))
	}
}
