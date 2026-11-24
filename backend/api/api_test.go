package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/device"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// newTestServer 连到真实的 PostgreSQL（与 seed 数据同一份），返回它的路由。
//
// 这是 spec 里约定的 api 接缝：httptest 端到端，不打桩数据库——
// 登录要校验的正是 seed 里的 bcrypt 哈希，打桩就等于没验。
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	return newTestServerWith(t, device.NewSimulatorAdapter(10*time.Millisecond, 0), time.Second)
}

// newTestServerWith 允许替换设备适配器与命令超时，用来覆盖失败/超时路径。
func newTestServerWith(t *testing.T, adapter device.Adapter, timeout time.Duration) http.Handler {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI 上必须设置 TEST_DATABASE_URL：静默跳过会让数据库接缝假装通过")
		}
		t.Skip("未设置 TEST_DATABASE_URL，跳过需要数据库的测试")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	t.Cleanup(pool.Close)

	commandHandler := control.NewCommandHandler(pool, adapter, timeout)
	hub := api.NewHub(storegen.New(pool))
	commandHandler.SetBroadcaster(hub)

	srv := api.New(
		api.NewPGAccounts(storegen.New(pool)),
		api.NewPGPlan(pool),
		api.NewPGItemTypes(pool),
		itemstore.NewItems(pool),
		commandHandler,
		api.NewSessionCodec("test-secret", time.Hour),
		false,
		"test",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	srv.SetHub(hub)
	srv.SetSensorReporter(control.NewSensorReporter(pool, hub))
	return srv.Router()
}

// commandRow 直接按 requestId 查 command_log：断言不依赖「最近 N 条」，
// 也不会因为重放到旧记录而误判（每个用例用新生成的 requestId）。
type commandRow struct {
	Status    string
	Error     *string
	ErrorCode *string
}

func commandLogOf(t *testing.T, requestID string) commandRow {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()
	var row commandRow
	err = pool.QueryRow(context.Background(),
		`SELECT status, error, error_code FROM command_log WHERE request_id = $1`, requestID).
		Scan(&row.Status, &row.Error, &row.ErrorCode)
	if err != nil {
		t.Fatalf("查询命令 %s 失败: %v", requestID, err)
	}
	return row
}

func do(t *testing.T, h http.Handler, method, path string, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
	}
	// 写方法默认带 CSRF 头：绝大多数用例关心的是业务语义，不是这道门。
	// 专门验这道门的用例用 doH 显式构造"不带"的情况。
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		r.Header.Set("X-Requested-With", "fetch")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == api.SessionCookieName && c.Value != "" {
			return c
		}
	}
	t.Fatalf("响应里没有会话 Cookie，状态码 %d，体 %s", w.Code, w.Body.String())
	return nil
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("错误响应不是 JSON: %v (%s)", err, w.Body.String())
	}
	return body.Error.Code
}

func TestHealthzIsPublic(t *testing.T) {
	h := newTestServer(t)
	w := do(t, h, http.MethodGet, "/api/healthz", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("健康检查应 200，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("健康检查体不含 status=ok：%s", w.Body.String())
	}
}

func TestProtectedRouteRequiresSession(t *testing.T) {
	h := newTestServer(t)
	w := do(t, h, http.MethodGet, "/api/auth/me", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无会话访问 /api/auth/me 应 401，实际 %d", w.Code)
	}
	if code := errorCode(t, w); code != "UNAUTHENTICATED" {
		t.Fatalf("错误码应为 UNAUTHENTICATED，实际 %q", code)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h := newTestServer(t)
	w := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"wrong"}`, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("口令错误应 401，实际 %d", w.Code)
	}
	if code := errorCode(t, w); code != "UNAUTHENTICATED" {
		t.Fatalf("错误码应为 UNAUTHENTICATED，实际 %q", code)
	}
}

func TestLoginRejectsUnknownUserWithSameError(t *testing.T) {
	h := newTestServer(t)
	w := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"nobody","password":"demo1234"}`, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("账号不存在应 401，实际 %d", w.Code)
	}
	if code := errorCode(t, w); code != "UNAUTHENTICATED" {
		t.Fatalf("错误码应为 UNAUTHENTICATED，实际 %q", code)
	}
}

func TestLoginThenMe(t *testing.T) {
	h := newTestServer(t)
	w := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("登录应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	cookie := sessionCookie(t, w)
	if !cookie.HttpOnly {
		t.Error("会话 Cookie 必须是 HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("会话 Cookie 应为 SameSite=Lax，实际 %v", cookie.SameSite)
	}

	me := do(t, h, http.MethodGet, "/api/auth/me", "", cookie)
	if me.Code != http.StatusOK {
		t.Fatalf("带会话访问 /api/auth/me 应 200，实际 %d：%s", me.Code, me.Body.String())
	}
	var user struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &user); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if user.Username != "zhang" || user.Role != "admin" {
		t.Fatalf("应返回 zhang/admin，实际 %+v", user)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"li","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	out := do(t, h, http.MethodPost, "/api/auth/logout", "", cookie)
	if out.Code != http.StatusNoContent {
		t.Fatalf("退出应 204，实际 %d", out.Code)
	}
	cleared := false
	for _, c := range out.Result().Cookies() {
		if c.Name == api.SessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("退出时应下发 MaxAge<0 的 Cookie 以清除会话")
	}
}

func TestTamperedCookieIsRejected(t *testing.T) {
	h := newTestServer(t)
	bad := &http.Cookie{Name: api.SessionCookieName, Value: "forged.value.sig"}
	w := do(t, h, http.MethodGet, "/api/auth/me", "", bad)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("伪造会话应 401，实际 %d", w.Code)
	}
}

func TestSendLightCommandAndRangeReject(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)
	itemID := "00000000-0000-4000-8000-000000000301"

	onID := uuid.NewString()
	on := do(t, h, http.MethodPost, "/api/items/"+itemID+"/commands",
		`{"capabilityKey":"on","value":true,"requestId":"`+onID+`"}`, cookie)
	if on.Code != http.StatusAccepted {
		t.Fatalf("灯开关命令应 202，实际 %d：%s", on.Code, on.Body.String())
	}

	states := do(t, h, http.MethodGet, "/api/items/"+itemID+"/states", "", cookie)
	if states.Code != http.StatusOK {
		t.Fatalf("读取灯状态应 200，实际 %d", states.Code)
	}
	if !strings.Contains(states.Body.String(), `"capabilityKey":"on","value":true`) &&
		!strings.Contains(states.Body.String(), `"capabilityKey":"on"`) {
		t.Fatalf("灯状态未落库：%s", states.Body.String())
	}

	rejectID := uuid.NewString()
	rejected := do(t, h, http.MethodPost, "/api/items/"+itemID+"/commands",
		`{"capabilityKey":"brightness","value":101,"requestId":"`+rejectID+`"}`, cookie)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("亮度越界应 400，实际 %d：%s", rejected.Code, rejected.Body.String())
	}
	if code := errorCode(t, rejected); code != "RANGE_OUT_OF_BOUNDS" {
		t.Fatalf("越界错误码应为 RANGE_OUT_OF_BOUNDS，实际 %q", code)
	}

	// 操作记录列表也要带上错误码，前端才能显示「为什么失败」。
	logs := do(t, h, http.MethodGet, "/api/command-logs?limit=20", "", cookie)
	if logs.Code != http.StatusOK || !strings.Contains(logs.Body.String(), `"errorCode":"RANGE_OUT_OF_BOUNDS"`) {
		t.Fatalf("操作记录应带 errorCode，实际 %d %s", logs.Code, logs.Body.String())
	}

	row := commandLogOf(t, rejectID)
	if row.Status != "rejected" || row.ErrorCode == nil || *row.ErrorCode != "RANGE_OUT_OF_BOUNDS" {
		t.Fatalf("越界应写 rejected + RANGE_OUT_OF_BOUNDS，实际 %+v", row)
	}

	// 重放同一条被拒命令必须返回与首次一致的结果（幂等），而不是重新跑一遍。
	replay := do(t, h, http.MethodPost, "/api/items/"+itemID+"/commands",
		`{"capabilityKey":"brightness","value":101,"requestId":"`+rejectID+`"}`, cookie)
	if replay.Code != http.StatusBadRequest || errorCode(t, replay) != "RANGE_OUT_OF_BOUNDS" {
		t.Fatalf("重放越界命令应仍 400 RANGE_OUT_OF_BOUNDS，实际 %d %s", replay.Code, replay.Body.String())

	}
}

func TestSendAirConditionerCommand(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"li","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)
	itemID := "00000000-0000-4000-8000-000000000302"

	for i, body := range []string{
		`{"capabilityKey":"mode","value":"cool","requestId":"` + uuid.NewString() + `"}`,
		`{"capabilityKey":"target_temp","value":28,"requestId":"` + uuid.NewString() + `"}`,
	} {
		w := do(t, h, http.MethodPost, "/api/items/"+itemID+"/commands", body, cookie)
		if w.Code != http.StatusAccepted {
			t.Fatalf("空调命令 %d 应 202，实际 %d：%s", i, w.Code, w.Body.String())
		}
	}

	rangeReject := do(t, h, http.MethodPost, "/api/items/"+itemID+"/commands",
		`{"capabilityKey":"target_temp","value":31,"requestId":"`+uuid.NewString()+`"}`, cookie)
	if rangeReject.Code != http.StatusBadRequest {
		t.Fatalf("空调温度越界应 400，实际 %d", rangeReject.Code)
	}
}

func TestStatesOfMissingItemReturns404(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)
	w := do(t, h, http.MethodGet,
		"/api/items/99999999-0000-4000-8000-000000000999/states", "", cookie)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在物品的状态应 404，实际 %d：%s", w.Code, w.Body.String())
	}
}

func TestWrongCapabilityTypeIsValidationFailed(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)
	requestID := uuid.NewString()
	w := do(t, h, http.MethodPost,
		"/api/items/00000000-0000-4000-8000-000000000301/commands",
		`{"capabilityKey":"brightness","value":"hot","requestId":"`+requestID+`"}`, cookie)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("类型错误应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "VALIDATION_FAILED" {
		t.Fatalf("类型错误应 VALIDATION_FAILED，实际 %q", code)
	}
	// 重放时必须返回同一个码（曾经因为 status 只记 rejected 而变成 RANGE_OUT_OF_BOUNDS）。
	replay := do(t, h, http.MethodPost,
		"/api/items/00000000-0000-4000-8000-000000000301/commands",
		`{"capabilityKey":"brightness","value":"hot","requestId":"`+requestID+`"}`, cookie)
	if code := errorCode(t, replay); code != "VALIDATION_FAILED" {
		t.Fatalf("重放类型错误应仍为 VALIDATION_FAILED，实际 %q（%s）", code, replay.Body.String())
	}
}

// TestCommandFailureAndTimeoutAreRecorded 覆盖两条以前「只在发布配置下不可达」的路径：
// 设备不可用、设备超时都要落 command_log，并且重放时返回同一个错误码。
func TestCommandFailureAndTimeoutAreRecorded(t *testing.T) {
	cases := []struct {
		name    string
		adapter device.Adapter
		timeout time.Duration
		code    string
		status  string
	}{
		{"unavailable", device.NewSimulatorAdapter(0, 1), time.Second, "DEVICE_UNAVAILABLE", "failed"},
		{"timeout", device.NewSimulatorAdapter(200*time.Millisecond, 0), 20 * time.Millisecond, "DEVICE_TIMEOUT", "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestServerWith(t, tc.adapter, tc.timeout)
			login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
			cookie := sessionCookie(t, login)
			requestID := uuid.NewString()
			body := `{"capabilityKey":"on","value":true,"requestId":"` + requestID + `"}`
			w := do(t, h, http.MethodPost, "/api/items/00000000-0000-4000-8000-000000000301/commands", body, cookie)
			if w.Code != http.StatusServiceUnavailable || errorCode(t, w) != tc.code {
				t.Fatalf("应 503 %s，实际 %d %s", tc.code, w.Code, w.Body.String())
			}
			row := commandLogOf(t, requestID)
			if row.Status != tc.status || row.ErrorCode == nil || *row.ErrorCode != tc.code {
				t.Fatalf("命令日志应为 %s/%s，实际 %+v", tc.status, tc.code, row)
			}
			replay := do(t, h, http.MethodPost, "/api/items/00000000-0000-4000-8000-000000000301/commands", body, cookie)
			if replay.Code != http.StatusServiceUnavailable || errorCode(t, replay) != tc.code {
				t.Fatalf("重放应仍 503 %s，实际 %d %s", tc.code, replay.Code, replay.Body.String())
			}
		})

	}
}

func TestSimulationTickDoesNotCreateHistory(t *testing.T) {
	h := newTestServer(t)
	login := do(t, h, http.MethodPost, "/api/auth/login", `{"username":"zhang","password":"demo1234"}`, nil)
	cookie := sessionCookie(t, login)

	dsn := os.Getenv("TEST_DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()
	before := 0
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM state_history").Scan(&before); err != nil {
		t.Fatalf("读取历史数量失败: %v", err)
	}

	tick := do(t, h, http.MethodPost, "/api/sim/tick", `{"minutes":60}`, cookie)
	if tick.Code != http.StatusOK {
		t.Fatalf("推进模拟时间应 200，实际 %d：%s", tick.Code, tick.Body.String())
	}

	after := 0
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM state_history").Scan(&after); err != nil {
		t.Fatalf("读取历史数量失败: %v", err)
	}
	if after < before {
		t.Fatalf("状态历史不应减少：推进前 %d，推进后 %d", before, after)
	}

	state := do(t, h, http.MethodGet,
		"/api/items/00000000-0000-4000-8000-000000000304/states", "", cookie)
	if state.Code != http.StatusOK || !strings.Contains(state.Body.String(), `"capabilityKey":"temperature"`) {
		t.Fatalf("tick 后传感器状态应可读，实际 %d：%s", state.Code, state.Body.String())
	}
}
