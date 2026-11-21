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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// newTestServer 连到真实的 PostgreSQL（与 seed 数据同一份），返回它的路由。
//
// 这是 spec 里约定的 api 接缝：httptest 端到端，不打桩数据库——
// 登录要校验的正是 seed 里的 bcrypt 哈希，打桩就等于没验。
func newTestServer(t *testing.T) http.Handler {
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

	srv := api.New(
		api.NewPGAccounts(storegen.New(pool)),
		api.NewPGPlan(pool),
		api.NewSessionCodec("test-secret", time.Hour),
		false,
		"test",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return srv.Router()
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
