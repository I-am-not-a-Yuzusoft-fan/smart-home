package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// csrfHeader 是契约里约定的 CSRF 标记头：同源 fetch 会带，
// 跨站表单提交带不了自定义头，所以"缺这个头"就是跨站写请求的信号。
const csrfHeader = "X-Requested-With"

// doH 与 do 相同，只是能额外指定请求头——用来分别构造"带 CSRF 头"与"不带"两种情况。
func doH(t *testing.T, h http.Handler, method, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// loginWithCsrf 走"正常浏览器"的路径：登录本身也是写请求，必须带头。
func loginWithCsrf(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	res := doH(t, h, http.MethodPost, "/api/auth/login",
		`{"username":"zhang","password":"demo1234"}`, nil,
		map[string]string{csrfHeader: "fetch"})
	if res.Code != http.StatusOK {
		t.Fatalf("带 CSRF 头的登录应 200，实际 %d：%s", res.Code, res.Body.String())
	}
	return sessionCookie(t, res)
}

// 写请求缺 CSRF 头必须被拒——这是 ADR 0002 §2.8 与详细设计 §3.1 写下的防线，
// 之前只有文档没有实现。
func TestWriteRequiresCsrfHeader(t *testing.T) {
	h := newTestServer(t)
	cookie := loginWithCsrf(t, h)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"画墙", http.MethodPost, "/api/walls", `{"ax":4100,"ay":4100,"bx":4700,"by":4100}`},
		{"退出", http.MethodPost, "/api/auth/logout", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := doH(t, h, tc.method, tc.path, tc.body, cookie, nil)
			if res.Code != http.StatusForbidden {
				t.Fatalf("缺 CSRF 头的写请求应 403，实际 %d：%s", res.Code, res.Body.String())
			}
			if code := errorCode(t, res); code != "FORBIDDEN" {
				t.Errorf("错误码应为 FORBIDDEN，实际 %s", code)
			}
		})
	}

	// 删墙是 DELETE，同样算写
	created := doH(t, h, http.MethodPost, "/api/walls",
		`{"ax":4200,"ay":4200,"bx":4800,"by":4200}`, cookie,
		map[string]string{csrfHeader: "fetch"})
	if created.Code != http.StatusCreated {
		t.Fatalf("带头的画墙应 201，实际 %d：%s", created.Code, created.Body.String())
	}
	id := sizeofWallID(t, created.Body.Bytes(), 4200, 4200, 4800, 4200)
	t.Cleanup(func() {
		doH(t, h, http.MethodDelete, "/api/walls/"+id, "", cookie, map[string]string{csrfHeader: "fetch"})
	})

	noHeader := doH(t, h, http.MethodDelete, "/api/walls/"+id, "", cookie, nil)
	if noHeader.Code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 头的 DELETE 应 403，实际 %d：%s", noHeader.Code, noHeader.Body.String())
	}

	// 带上头就照常工作（确认这道门没有把正常路径也挡了）
	withHeader := doH(t, h, http.MethodDelete, "/api/walls/"+id, "", cookie,
		map[string]string{csrfHeader: "fetch"})
	if withHeader.Code != http.StatusNoContent {
		t.Fatalf("带 CSRF 头的删除应 204，实际 %d：%s", withHeader.Code, withHeader.Body.String())
	}
}

// 登录也不能例外：否则攻击者用跨站表单先登进自己的账号，后续操作就都带上了合法会话。
func TestLoginRequiresCsrfHeader(t *testing.T) {
	h := newTestServer(t)

	res := doH(t, h, http.MethodPost, "/api/auth/login",
		`{"username":"zhang","password":"demo1234"}`, nil, nil)
	if res.Code != http.StatusForbidden {
		t.Fatalf("缺 CSRF 头的登录应 403，实际 %d：%s", res.Code, res.Body.String())
	}
	if code := errorCode(t, res); code != "FORBIDDEN" {
		t.Errorf("错误码应为 FORBIDDEN，实际 %s", code)
	}
}

// 读请求不受影响：GET 不会改变状态，套 CSRF 只会平白增加客户端负担。
func TestReadDoesNotRequireCsrfHeader(t *testing.T) {
	h := newTestServer(t)
	cookie := loginWithCsrf(t, h)

	res := doH(t, h, http.MethodGet, "/api/plan", "", cookie, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET 不带 CSRF 头应 200，实际 %d：%s", res.Code, res.Body.String())
	}
}

// sizeofWallID 从户型响应里找指定位形的墙——用于清理与后续 DELETE。
func sizeofWallID(t *testing.T, body []byte, ax, ay, bx, by float64) string {
	t.Helper()
	plan := decodePlan(t, body)
	for _, w := range plan.Walls {
		if (w.AX == ax && w.AY == ay && w.BX == bx && w.BY == by) ||
			(w.AX == bx && w.AY == by && w.BX == ax && w.BY == ay) {
			return w.ID
		}
	}
	t.Fatalf("响应里没找到墙 (%v,%v)-(%v,%v)", ax, ay, bx, by)
	return ""
}
