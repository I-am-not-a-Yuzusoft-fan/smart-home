package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// planBody 只取测试要用的字段，避免和生成的模型耦合。
type planBody struct {
	Corners []struct {
		ID string  `json:"id"`
		X  float64 `json:"x"`
		Y  float64 `json:"y"`
	} `json:"corners"`
	Walls []struct {
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
