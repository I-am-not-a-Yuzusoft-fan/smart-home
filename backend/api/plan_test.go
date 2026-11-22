package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// planBody 只取测试要用的字段，避免和生成的模型耦合。
type planRoom struct {
	Hash   string  `json:"hash"`
	AreaM2 float64 `json:"areaM2"`
	Ring   []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"ring"`
	Name *string `json:"name"`
	Kind *string `json:"kind"`
}

type planBody struct {
	Corners []struct {
		ID string  `json:"id"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
	} `json:"corners"`
	Rooms     []planRoom `json:"rooms"`
	OpenWalls int        `json:"openWalls"`
	Version   int64      `json:"version"`
	Walls     []struct {
		ID  string  `json:"id"`
		AID string  `json:"aId"`
		BID string  `json:"bId"`
		AX  float64 `json:"ax"`
		AY  float64 `json:"ay"`
		BX  float64 `json:"bx"`
		BY  float64 `json:"by"`
	} `json:"walls"`
}

func (p planBody) cornerAt(x, y float64) (int, bool) {
	for i, c := range p.Corners {
		if c.X == x && c.Y == y {
			return i, true
		}
	}
	return 0, false
}

func decodePlan(t *testing.T, body []byte) planBody {
	t.Helper()
	var p planBody
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("户型解析失败: %v（原始体 %s）", err, body)
	}
	return p
}

// 端点按坐标去重、同一对端点（含反向）只留一面墙 —— 这是 T2 的持久化语义。
func TestCreateWallDedupesCornersAndPairs(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	before := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())

	// 用一组 seed 里没有的坐标，免得和演示数据撞上
	const ax, ay, bx, by = 1700.0, 1700.0, 2300.0, 1700.0
	first := do(t, h, http.MethodPost, "/api/walls",
		`{"ax":1700,"ay":1700,"bx":2300,"by":1700}`, cookie)
	if first.Code != http.StatusCreated {
		t.Fatalf("画墙应 201，实际 %d：%s", first.Code, first.Body.String())
	}
	afterCreate := decodePlan(t, first.Body.Bytes())
	if len(afterCreate.Walls) != len(before.Walls)+1 {
		t.Fatalf("墙数应 +1：before %d, after %d", len(before.Walls), len(afterCreate.Walls))
	}
	if _, ok := afterCreate.cornerAt(ax, ay); !ok {
		t.Errorf("起点端点应存在，实际端点 %+v", afterCreate.Corners)
	}
	if _, ok := afterCreate.cornerAt(bx, by); !ok {
		t.Errorf("终点端点应存在")
	}

	// 立刻登记清理：后面任何一条 t.Fatalf 都会提前返回，脏数据留下会让下次本地跑必挂。
	t.Cleanup(func() {
		for _, w := range afterCreate.Walls {
			if (w.AX == ax && w.AY == ay && w.BX == bx && w.BY == by) ||
				(w.AX == bx && w.AY == by && w.BX == ax && w.BY == ay) {
				do(t, h, http.MethodDelete, "/api/walls/"+w.ID, "", cookie)
				return
			}
		}
	})

	// 反着画同一面墙：不应新增
	reversed := do(t, h, http.MethodPost, "/api/walls",
		`{"ax":2300,"ay":1700,"bx":1700,"by":1700}`, cookie)
	if reversed.Code != http.StatusCreated {
		t.Fatalf("重复画墙也应 201，实际 %d：%s", reversed.Code, reversed.Body.String())
	}
	afterReversed := decodePlan(t, reversed.Body.Bytes())
	if len(afterReversed.Walls) != len(afterCreate.Walls) {
		t.Fatalf("反着画同一面墙不该新增：%d → %d", len(afterCreate.Walls), len(afterReversed.Walls))
	}

	// 清理：删掉刚建的墙，返回先前状态（端点留着合法）
	var createdID string
	for _, w := range afterReversed.Walls {
		if (w.AX == ax && w.AY == ay && w.BX == bx && w.BY == by) ||
			(w.AX == bx && w.AY == by && w.BX == ax && w.BY == ay) {
			createdID = w.ID
			break
		}
	}
	if createdID == "" {
		t.Fatal("没找到刚创建的墙")
	}
	if del := do(t, h, http.MethodDelete, "/api/walls/"+createdID, "", cookie); del.Code != http.StatusNoContent {
		t.Fatalf("删墙应 204，实际 %d：%s", del.Code, del.Body.String())
	}
	final := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())
	if len(final.Walls) != len(before.Walls) {
		t.Fatalf("删除后墙数应回到 %d，实际 %d", len(before.Walls), len(final.Walls))
	}

	// 再删一次：404
	again := do(t, h, http.MethodDelete, "/api/walls/"+createdID, "", cookie)
	if again.Code != http.StatusNotFound {
		t.Errorf("删除不存在的墙应 404，实际 %d", again.Code)
	}
	if code := errorCode(t, again); code != "NOT_FOUND" {
		t.Errorf("错误码应为 NOT_FOUND，实际 %s", code)
	}
}

func TestCreateWallRejectsDegenerateInput(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"两端重合", `{"ax":100,"ay":100,"bx":100,"by":100}`, "VALIDATION_FAILED"},
		{"坐标越界", `{"ax":-10,"ay":100,"bx":100,"by":100}`, "VALIDATION_FAILED"},
		{"墙厚为零", `{"ax":100,"ay":100,"bx":200,"by":100,"thickness":0}`, "VALIDATION_FAILED"},
		{"多余字段", `{"ax":100,"ay":100,"bx":200,"by":100,"colour":"red"}`, "VALIDATION_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, h, http.MethodPost, "/api/walls", tc.body, cookie)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d：%s", w.Code, w.Body.String())
			}
			if code := errorCode(t, w); code != tc.want {
				t.Errorf("错误码应为 %s，实际 %s", tc.want, code)
			}
		})
	}
}

func TestPlanRequiresSession(t *testing.T) {
	h := newTestServer(t)
	if w := do(t, h, http.MethodGet, "/api/plan", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录读户型应 401，实际 %d", w.Code)
	}
	if w := do(t, h, http.MethodPost, "/api/walls", `{"ax":0,"ay":0,"bx":100,"by":0}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录画墙应 401，实际 %d", w.Code)
	}
}

// 路径参数不是合法 uuid 时，必须仍返回契约里的 JSON 封套——
// 默认的 oapi-codegen 错误处理会输出纯文本，前端 JSON.parse 直接抛 SyntaxError。
func TestInvalidWallIDReturnsJSONEnvelope(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	res := do(t, h, http.MethodDelete, "/api/walls/not-a-uuid", "", cookie)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("非法 uuid 应 400，实际 %d：%s", res.Code, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type 应为 application/json，实际 %q", ct)
	}
	if code := errorCode(t, res); code != "VALIDATION_FAILED" {
		t.Errorf("错误码应为 VALIDATION_FAILED，实际 %s", code)
	}
}

// 契约写 thickness minimum 1，实现必须同样拒绝 0.5——否则契约与行为两张皮。
func TestCreateWallRejectsSubCentimetreThickness(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	res := do(t, h, http.MethodPost, "/api/walls",
		`{"ax":3100,"ay":3100,"bx":3700,"by":3100,"thickness":0.5}`, cookie)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("墙厚 0.5 应 400，实际 %d：%s", res.Code, res.Body.String())
	}
	if code := errorCode(t, res); code != "VALIDATION_FAILED" {
		t.Errorf("错误码应为 VALIDATION_FAILED，实际 %s", code)
	}
}

// 四面墙围成矩形 -> 响应里带回房间；拆掉一面墙 -> 房间消失、未围合的墙数上去。
// 房间由后端推导并在保存墙的响应里返回，前端不自己算（这是 #6 的验收点）。
func TestPlanDerivesRoomsFromWalls(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	before := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())

	// 200×200 的方框，避开 seed 的 400×300 区域
	type pt struct{ x, y float64 }
	corners := []pt{{2000, 2000}, {2200, 2000}, {2200, 2200}, {2000, 2200}}
	walls := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}}

	// 先登记清理：中间任何一条 t.Fatalf 都会提前返回，遗留的墙会让下次运行
	// 的 before.Rooms / before.OpenWalls 全变，本地和 CI 都会莫名其妙地挂。
	var created []string
	t.Cleanup(func() {
		for _, id := range created {
			do(t, h, http.MethodDelete, "/api/walls/"+id, "", cookie)
		}
		// POST 失败时 created 可能不全，按区域兜底再删一遍
		if resp := do(t, h, http.MethodGet, "/api/plan", "", cookie); resp.Code == http.StatusOK {
			for _, w := range decodePlan(t, resp.Body.Bytes()).Walls {
				if inRect(w.AX, w.AY, w.BX, w.BY) {
					do(t, h, http.MethodDelete, "/api/walls/"+w.ID, "", cookie)
				}
			}
		}
	})

	var last planBody
	for _, w := range walls {
		a, b := corners[w[0]], corners[w[1]]
		body := `{"ax":` + ftoa(a.x) + `,"ay":` + ftoa(a.y) +
			`,"bx":` + ftoa(b.x) + `,"by":` + ftoa(b.y) + `}`
		res := do(t, h, http.MethodPost, "/api/walls", body, cookie)
		if res.Code != http.StatusCreated {
			t.Fatalf("画墙应 201，实际 %d：%s", res.Code, res.Body.String())
		}
		last = decodePlan(t, res.Body.Bytes())
	}
	for _, w := range last.Walls {
		if inRect(w.AX, w.AY, w.BX, w.BY) {
			created = append(created, w.ID)
		}
	}
	if len(created) != 4 {
		t.Fatalf("应建出 4 面墙，实际 %d", len(created))
	}

	if got, want := len(last.Rooms), len(before.Rooms)+1; got != want {
		t.Fatalf("闭合后房间数应 %d，实际 %d", want, got)
	}
	var square *float64
	for _, r := range last.Rooms {
		if len(r.Ring) == 4 && r.AreaM2 == 4 {
			area := r.AreaM2
			square = &area
		}
	}
	if square == nil {
		t.Fatalf("应有面积为 4 平方米的方形房间，实际 %+v", last.Rooms)
	}

	// 拆掉一面墙：房间消失，未围合的墙数 +3（剩下三面墙都悬空了）
	res := do(t, h, http.MethodDelete, "/api/walls/"+created[0], "", cookie)
	if res.Code != http.StatusNoContent {
		t.Fatalf("删墙应 204，实际 %d：%s", res.Code, res.Body.String())
	}
	after := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())
	if got, want := len(after.Rooms), len(before.Rooms); got != want {
		t.Errorf("拆墙后房间数应回到 %d，实际 %d", want, got)
	}
	if got, want := after.OpenWalls, before.OpenWalls+3; got != want {
		t.Errorf("拆墙后未围合的墙数应 %d，实际 %d", want, got)
	}
}

func inRect(ax, ay, bx, by float64) bool {
	in := func(x, y float64) bool {
		return (x == 2000 || x == 2200) && (y == 2000 || y == 2200)
	}
	return in(ax, ay) && in(bx, by)
}

func ftoa(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// 拖端点改墙：长度与角度都要能改，旧端点若没人用就清掉。
func TestUpdateWallMovesEndpoint(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	res := do(t, h, http.MethodPost, "/api/walls", `{"ax":3000,"ay":3000,"bx":3400,"by":3000}`, cookie)
	if res.Code != http.StatusCreated {
		t.Fatalf("画墙应 201，实际 %d：%s", res.Code, res.Body.String())
	}
	plan := decodePlan(t, res.Body.Bytes())
	var wallID string
	for _, w := range plan.Walls {
		if w.AY == 3000 && w.BY == 3000 && w.AX == 3000 {
			wallID = w.ID
		}
	}
	if wallID == "" {
		t.Fatalf("没找到刚画的墙")
	}
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/walls/"+wallID, "", cookie) })

	// 端点从 (3000,3000) 拖到 (3000,3100)：长度 400 → 斜边 sqrt(400²+100²)
	res = do(t, h, http.MethodPatch, "/api/walls/"+wallID,
		`{"ax":3000,"ay":3100,"bx":3400,"by":3000}`, cookie)
	if res.Code != http.StatusOK {
		t.Fatalf("改墙应 200，实际 %d：%s", res.Code, res.Body.String())
	}
	after := decodePlan(t, res.Body.Bytes())

	var got *struct{ ax, ay, bx, by float64 }
	for _, w := range after.Walls {
		if w.ID == wallID {
			got = &struct{ ax, ay, bx, by float64 }{w.AX, w.AY, w.BX, w.BY}
		}
	}
	if got == nil {
		t.Fatalf("墙 %s 改完消失了", wallID)
	}
	if got.ax != 3000 || got.ay != 3100 || got.bx != 3400 || got.by != 3000 {
		t.Fatalf("端点没改对：%+v", got)
	}

	// 旧端点 (3000,3000) 已经没人引用，不该再出现在户型里
	if _, ok := after.cornerAt(3000, 3000); ok {
		t.Fatalf("旧端点 (3000,3000) 应被清理，实际还在")
	}
	if _, ok := after.cornerAt(3000, 3100); !ok {
		t.Fatalf("新端点 (3000,3100) 应该存在")
	}
}

func TestUpdateWallRejectsCollisionWithOtherWall(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	a := do(t, h, http.MethodPost, "/api/walls", `{"ax":3000,"ay":3200,"bx":3400,"by":3200}`, cookie)
	b := do(t, h, http.MethodPost, "/api/walls", `{"ax":3000,"ay":3300,"bx":3400,"by":3300}`, cookie)
	if a.Code != http.StatusCreated || b.Code != http.StatusCreated {
		t.Fatalf("准备两面墙失败：%d %d", a.Code, b.Code)
	}
	idA := wallInRect(t, decodePlan(t, a.Body.Bytes()), 3200)
	idB := wallInRect(t, decodePlan(t, b.Body.Bytes()), 3300)
	t.Cleanup(func() {
		do(t, h, http.MethodDelete, "/api/walls/"+idA, "", cookie)
		do(t, h, http.MethodDelete, "/api/walls/"+idB, "", cookie)
	})

	// 把第二面墙挪成和第一面完全重合 → 409，不许悄悄合并
	res := do(t, h, http.MethodPatch, "/api/walls/"+idB, `{"ax":3000,"ay":3200,"bx":3400,"by":3200}`, cookie)
	if res.Code != http.StatusConflict {
		t.Fatalf("重合应 409，实际 %d：%s", res.Code, res.Body.String())
	}
}

func TestUpdateWallNotFound(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	res := do(t, h, http.MethodPatch, "/api/walls/00000000-0000-4000-8000-00000000dead",
		`{"ax":0,"ay":0,"bx":100,"by":0}`, cookie)
	if res.Code != http.StatusNotFound {
		t.Fatalf("不存在的墙应 404，实际 %d：%s", res.Code, res.Body.String())
	}
}

// wallInRect 取 y 落在指定水平线上、且起点 x=3000 的那面墙。
func wallInRect(t *testing.T, p planBody, y float64) string {
	t.Helper()
	for _, w := range p.Walls {
		if w.AY == y && w.BY == y && w.AX == 3000 {
			return w.ID
		}
	}
	t.Fatalf("没找到 y=%v 的墙", y)
	return ""
}

// 撤销/重做走「整份替换 + 版本号」，下面三条钉住它的关键语义。
func TestReplacePlanRejectsStaleVersion(t *testing.T) {
	h := newTestServer(t)
	cookie := sessionCookie(t, do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil))

	plan := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())
	body := fmt.Sprintf(`{"version":%d,"walls":[]}`, plan.Version+5)
	rec := do(t, h, http.MethodPut, "/api/plan", body, cookie)
	if rec.Code != http.StatusConflict {
		t.Fatalf("版本号不符应 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "CONFLICT" {
		t.Fatalf("错误码应为 CONFLICT，实际 %s", code)
	}
}

func TestReplacePlanRestoresDeletedWallWithSameID(t *testing.T) {
	h := newTestServer(t)
	cookie := sessionCookie(t, do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil))

	// 建一面墙，记下 id 与几何
	created := do(t, h, http.MethodPost, "/api/walls",
		`{"ax":3300,"ay":3300,"bx":3700,"by":3300}`, cookie)
	if created.Code != http.StatusCreated {
		t.Fatalf("建墙失败 %d：%s", created.Code, created.Body.String())
	}
	plan := decodePlan(t, created.Body.Bytes())
	var target struct {
		id           string
		ax, ay, bx   float64
		by           float64
	}
	for _, w := range plan.Walls {
		if w.AY == 3300 && w.BY == 3300 && w.AX == 3300 {
			target.id, target.ax, target.ay, target.bx, target.by = w.ID, w.AX, w.AY, w.BX, w.BY
		}
	}
	if target.id == "" {
		t.Fatal("没找到刚建的墙")
	}
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/walls/"+target.id, "", cookie) })

	// 删掉它 —— 相当于用户按了 Delete
	if del := do(t, h, http.MethodDelete, "/api/walls/"+target.id, "", cookie); del.Code != http.StatusNoContent {
		t.Fatalf("删墙失败 %d", del.Code)
	}

	// 撤销：把整份列表写回，带原来的 id
	current := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())
	restore := fmt.Sprintf(
		`{"version":%d,"walls":[
			{"id":%q,"ax":%v,"ay":%v,"bx":%v,"by":%v}
		]}`, current.Version, target.id, target.ax, target.ay, target.bx, target.by)
	rec := do(t, h, http.MethodPut, "/api/plan", restore, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("撤销应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	after := decodePlan(t, rec.Body.Bytes())
	found := false
	for _, w := range after.Walls {
		if w.ID == target.id {
			found = true
		}
	}
	if !found {
		t.Fatalf("撤销应恢复同一面墙（id 必须是 %s），实际 %+v", target.id, after.Walls)
	}
	if after.Version <= current.Version {
		t.Fatalf("替换成功后版本号应递增：%d → %d", current.Version, after.Version)
	}
}

func TestReplacePlanDeletesWallsMissingFromList(t *testing.T) {
	h := newTestServer(t)
	cookie := sessionCookie(t, do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil))

	before := decodePlan(t, do(t, h, http.MethodGet, "/api/plan", "", cookie).Body.Bytes())
	var keep []string
	for _, w := range before.Walls {
		keep = append(keep, fmt.Sprintf(`{"id":%q,"ax":%v,"ay":%v,"bx":%v,"by":%v}`,
			w.ID, w.AX, w.AY, w.BX, w.BY))
	}
	// 只写回前一半的墙：剩下的应该被删掉
	half := keep[:len(keep)/2]
	body := fmt.Sprintf(`{"version":%d,"walls":[%s]}`, before.Version, strings.Join(half, ","))
	rec := do(t, h, http.MethodPut, "/api/plan", body, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("整份替换应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	after := decodePlan(t, rec.Body.Bytes())
	if len(after.Walls) != len(half) {
		t.Fatalf("墙数应变成 %d，实际 %d", len(half), len(after.Walls))
	}
	// 还原，别影响其他用例共享的数据库
	current := after.Version
	var full []string
	for _, w := range before.Walls {
		full = append(full, fmt.Sprintf(`{"id":%q,"ax":%v,"ay":%v,"bx":%v,"by":%v}`,
			w.ID, w.AX, w.AY, w.BX, w.BY))
	}
	do(t, h, http.MethodPut, "/api/plan",
		fmt.Sprintf(`{"version":%d,"walls":[%s]}`, current, strings.Join(full, ",")), cookie)
}
