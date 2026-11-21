package api

import (
	"net/http"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
)

const (
	// CSRFHeaderName 是写请求必须携带的标记头。
	//
	// 用自定义头而不是 token，因为浏览器对跨站表单提交不允许加自定义头：
	// 攻击者没法在自己的页面里伪造这个头，同源的 fetch 则可以一字不差地带上。
	CSRFHeaderName = "X-Requested-With"
	// CSRFHeaderValue 是前端 svc 固定发送的值。只认这一个值，避免"随便填点啥"也算过。
	CSRFHeaderValue = "fetch"
)

// requireCsrfHeader 挡住跨站写请求。
//
// 会话 Cookie 已经是 SameSite=Lax，跨站表单 POST 本来就不带 Cookie；这道检查是
// 第二层保险，好处是能给出明确的 403 而不是让用户看到一个像"没登录"的 401。
// 读请求（GET/HEAD/OPTIONS）不改变状态，不检查——否则每个 GET 都要加头，纯属负担。
func requireCsrfHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(CSRFHeaderName) != CSRFHeaderValue {
			writeError(w, http.StatusForbidden, gen.FORBIDDEN,
				"写操作需要 "+CSRFHeaderName+": "+CSRFHeaderValue+" 请求头")
			return
		}
		next.ServeHTTP(w, r)
	})
}
