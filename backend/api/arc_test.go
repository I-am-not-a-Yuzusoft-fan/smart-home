package api_test

import (
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

	// 超界拒绝。
	bad := `{"ax":4100,"ay":4100,"bx":4300,"by":4100,"thickness":10,"arcExtent":180}`
	if res := do(t, h, http.MethodPatch, "/api/walls/"+top, bad, admin); res.Code != http.StatusBadRequest {
		t.Fatalf("180° 应 400，实际 %d", res.Code)
	}

	// 弧墙上不能开门。
	open := fmt.Sprintf(`{"wallId":%q,"kind":"door","offset":50,"width":80}`, top)
	if res := do(t, h, http.MethodPost, "/api/openings", open, admin); res.Code != http.StatusBadRequest ||
		!strings.Contains(res.Body.String(), "弧墙") {
		t.Fatalf("弧墙开门应 400 且提到弧墙，实际 %d：%s", res.Code, res.Body.String())
	}
}
