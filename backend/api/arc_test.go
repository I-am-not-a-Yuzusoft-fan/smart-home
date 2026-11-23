package api_test

import (
	"encoding/json"
	"fmt"
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
