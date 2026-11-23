package api_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
)

// 弧墙 v1：PATCH 能存弧度、房间按弧重算、|弧度| ≥ 180 拒绝、弧墙上不能开门窗。
func TestArcWallRoundTripAndGuards(t *testing.T) {
	h := newTestServer(t)
	pool := testPool(t)
	admin := loginAs(t, h, "zhang")
	hash, walls := rectPlan(t, h, admin, 4100, 4100, 200)
	defer cleanupRoom(t, pool, h, admin, hash, walls)

	// 把顶墙弯成 90°。
	top := walls[0]
	body := fmt.Sprintf(`{"ax":4100,"ay":4100,"bx":4300,"by":4100,"thickness":10,"arcExtent":90}`)
	res := do(t, h, http.MethodPatch, "/api/walls/"+top, body, admin)
	if res.Code != http.StatusOK {
		t.Fatalf("改弧度应 200，实际 %d：%s", res.Code, res.Body.String())
	}
	plan := decodePlan(t, res.Body.Bytes())
	got := 0.0
	for _, w := range plan.Walls {
		if w.ID == top {
			got = w.ArcExtent
		}
	}
	if got != 90 {
		t.Fatalf("弧度应存为 90，实际 %v", got)
	}
	// 房间面积要比 4 ㎡ 大：90° 弧把顶边撑出去一个弓形（半径 100、矢高 29.3）。
	// 库里还有 seed 的客厅，所以按「多边形顶点多且面积略大于 4」认自己那间。
	var mine *planRoom
	for i := range plan.Rooms {
		if plan.Rooms[i].AreaM2 > 4.0 && plan.Rooms[i].AreaM2 < 5.0 && len(plan.Rooms[i].Ring) > 8 {
			mine = &plan.Rooms[i]
		}
	}
	if mine == nil {
		t.Fatalf("弯成弧后应出现一个面积略大于 4 ㎡、顶点被采样的房间，实际 %+v", plan.Rooms)
	}
	if mine.AreaM2 < 4.5 {
		t.Fatalf("90° 弧的弓形应让面积涨到 4.5 ㎡ 以上，实际 %.2f", mine.AreaM2)
	}

	// 正好 180° 是合法的半圆（以前用开区间会把它拒掉，界面上也就"落定不了"）。
	half := `{"ax":4100,"ay":4100,"bx":4300,"by":4100,"thickness":10,"arcExtent":180}`
	if res := do(t, h, http.MethodPatch, "/api/walls/"+top, half, admin); res.Code != http.StatusOK {
		t.Fatalf("180° 半圆应 200，实际 %d：%s", res.Code, res.Body.String())
	}
	// 超过半圈没有意义，拒绝。
	bad := `{"ax":4100,"ay":4100,"bx":4300,"by":4100,"thickness":10,"arcExtent":181}`
	if res := do(t, h, http.MethodPatch, "/api/walls/"+top, bad, admin); res.Code != http.StatusBadRequest {
		t.Fatalf("181° 应 400，实际 %d", res.Code)
	}

	// 弧墙上不能开门。
	open := fmt.Sprintf(`{"wallId":%q,"kind":"door","offset":50,"width":80}`, top)
	if res := do(t, h, http.MethodPost, "/api/openings", open, admin); res.Code != http.StatusBadRequest ||
		!strings.Contains(res.Body.String(), "弧墙") {
		t.Fatalf("弧墙开门应 400 且提到弧墙，实际 %d：%s", res.Code, res.Body.String())
	}
}

// 拖端点时前端只发坐标（不带 arcExtent / thickness），后端必须沿用原值，
// 否则弧墙会被拉直、20 厘米外墙会被悄悄改回 10。
func TestPartialWallUpdateKeepsArcAndThickness(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodPost, "/api/walls",
		`{"ax":1000,"ay":1000,"bx":1400,"by":1000,"arcExtent":60,"thickness":20}`, admin)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("建弧墙应 201，实际 %d：%s", w.Code, w.Body.String())
	}
	var plan planBody
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("解析户型失败: %v", err)
	}
	wallID := plan.Walls[len(plan.Walls)-1].ID
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/walls/"+wallID, "", admin) })

	// 只改坐标：等同拖端点
	w = do(t, h, http.MethodPatch, "/api/walls/"+wallID,
		`{"ax":1010,"ay":1010,"bx":1410,"by":1010}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("改墙应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("解析户型失败: %v", err)
	}
	var got struct {
		ArcExtent float64 `json:"arcExtent"`
		Thickness float64 `json:"thickness"`
	}
	for _, wall := range plan.Walls {
		if wall.ID == wallID {
			got.ArcExtent = wall.ArcExtent
			got.Thickness = wall.Thickness
		}
	}
	if got.ArcExtent != 60 {
		t.Fatalf("只改坐标后弧度应保持 60，实际 %v", got.ArcExtent)
	}
	if got.Thickness != 20 {
		t.Fatalf("只改坐标后厚度应保持 20，实际 %v", got.Thickness)
	}
}

// 只传弧度的 PATCH 不能把端点写成 0：前端"把一面直墙改成弧墙"走的就是这条路。
func TestPatchWallArcOnlyKeepsEndpoints(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodPost, "/api/walls",
		`{"ax":1000,"ay":2000,"bx":1400,"by":2000}`, admin)
	if w.Code != http.StatusCreated {
		t.Fatalf("建墙应 201，实际 %d：%s", w.Code, w.Body.String())
	}
	var plan planBody
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("解析户型失败: %v", err)
	}
	wallID := plan.Walls[len(plan.Walls)-1].ID
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/walls/"+wallID, "", admin) })

	// 只带弧度：前端"画弧"把已有直墙改成弧墙时发的最小请求体。
	w = do(t, h, http.MethodPatch, "/api/walls/"+wallID, `{"arcExtent":60}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("只改弧度应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatalf("解析户型失败: %v", err)
	}
	for _, wall := range plan.Walls {
		if wall.ID != wallID {
			continue
		}
		if wall.ArcExtent != 60 {
			t.Fatalf("弧度应为 60，实际 %v", wall.ArcExtent)
		}
		if wall.AX != 1000 || wall.AY != 2000 || wall.BX != 1400 || wall.BY != 2000 {
			t.Fatalf("端点被改坏了：(%v,%v)-(%v,%v)", wall.AX, wall.AY, wall.BX, wall.BY)
		}
		return
	}
	t.Fatal("响应里找不到被改的墙")
}

// 半圆阳台：弦上那面直墙与外面那段弧墙共用一对端点，两面都能存、房间是两个。
// 顺带验证「完全相同的墙」仍然幂等、同端点同弧度的改墙会 409、房间用途能选阳台。
func TestChordAndArcShareEndpoints(t *testing.T) {
	h := newTestServer(t)
	pool := testPool(t)
	admin := loginAs(t, h, "zhang")
	hash, walls := rectPlan(t, h, admin, 4400, 4400, 200)
	defer cleanupRoom(t, pool, h, admin, hash, walls)

	// 顶墙 (4400,4400)-(4600,4400) 本来就在；在同一对端点上再挂一段 180° 弧墙。
	base := len(decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", admin).Body.Bytes()).Walls)
	// +90° 往弦的上方（房间外侧）鼓；180° 半圆在离散时符号会退化，这里不用它。
	body := fmt.Sprintf(`{"ax":4400,"ay":4400,"bx":4600,"by":4400,"arcExtent":90}`)
	res := do(t, h, http.MethodPost, "/api/walls", body, admin)
	if res.Code != http.StatusCreated {
		t.Fatalf("同端点的弧墙应 201，实际 %d：%s", res.Code, res.Body.String())
	}
	if res.Header().Get("X-Wall-Existing") != "" {
		t.Fatalf("新建的弧墙不该带 X-Wall-Existing")
	}
	plan := decodePlan(t, res.Body.Bytes())
	if len(plan.Walls) != base+1 {
		t.Fatalf("同端点弧墙应共存（墙数 +1），实际 %d 面（原 %d）", len(plan.Walls), base)
	}
	arcs := 0
	var arcWallID string
	for _, w := range plan.Walls {
		if w.ArcExtent != 0 {
			arcs++
			arcWallID = w.ID
		}
	}
	if arcs != 1 {
		t.Fatalf("应有一面弧墙，实际 %d 面", arcs)
	}
	// 弧墙与矩形共用端点，cleanupRoom 只删矩形那四面，这里单独收尾。
	defer func() { do(t, h, http.MethodDelete, "/api/walls/"+arcWallID, "", admin) }()

	// 房间：4 ㎡ 的矩形 + 0.57 ㎡ 的弓形（弦 2 m、90° → 半径 1.414 m，弓形 = r²(θ−sinθ)/2）。
	var lensHash string
	var rectArea float64
	for _, r := range plan.Rooms {
		if math.Abs(r.AreaM2-0.57) < 0.05 {
			lensHash = r.Hash
		}
		if math.Abs(r.AreaM2-4.0) < 0.05 {
			rectArea = r.AreaM2
		}
	}
	if rectArea == 0 || lensHash == "" {
		t.Fatalf("应同时有 4 ㎡ 矩形与 ≈0.57 ㎡ 的弓形阳台，实际 %+v", plan.Rooms)
	}

	// 阳台用途：把透镜形命名为「南阳台」并选 balcony。
	patch := fmt.Sprintf(`{"name":"南阳台","kind":"balcony"}`)
	res = do(t, h, http.MethodPatch, "/api/rooms/"+lensHash, patch, admin)
	if res.Code != http.StatusOK {
		t.Fatalf("给阳台命名应 200，实际 %d：%s", res.Code, res.Body.String())
	}
	plan = decodePlan(t, res.Body.Bytes())
	found := false
	for _, r := range plan.Rooms {
		if r.Hash == lensHash && r.Name != nil && *r.Name == "南阳台" && r.Kind != nil && *r.Kind == "balcony" {
			found = true
		}
	}
	if !found {
		t.Fatalf("阳台应记为 kind=balcony，实际 %+v", plan.Rooms)
	}

	// 完全相同的墙再画一次：仍 201，但带 X-Wall-Existing，墙数不变。
	straight := fmt.Sprintf(`{"ax":4400,"ay":4400,"bx":4600,"by":4400}`)
	res = do(t, h, http.MethodPost, "/api/walls", straight, admin)
	if res.Code != http.StatusCreated {
		t.Fatalf("重复建墙仍应 201（幂等），实际 %d", res.Code)
	}
	if res.Header().Get("X-Wall-Existing") != "1" {
		t.Fatalf("重复建墙应带 X-Wall-Existing: 1，实际 %q", res.Header().Get("X-Wall-Existing"))
	}
	if n := len(decodePlan(t, res.Body.Bytes()).Walls); n != base+1 {
		t.Fatalf("重复建墙不该多出墙，实际 %d 面", n)
	}

	// 把弧墙改成直墙 → 与已有弦墙完全相同 → 409。
	var arcID string
	for _, w := range decodePlan(t, res.Body.Bytes()).Walls {
		if w.ArcExtent != 0 {
			arcID = w.ID
		}
	}
	res = do(t, h, http.MethodPatch, "/api/walls/"+arcID, `{"arcExtent":0}`, admin)
	if res.Code != http.StatusConflict {
		t.Fatalf("改成与弦墙完全相同应 409，实际 %d：%s", res.Code, res.Body.String())
	}
}
