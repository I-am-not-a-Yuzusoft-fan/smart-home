package api

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
)

type accountKey struct{}

// AccountFrom 从请求上下文取出已鉴权的账号。
func AccountFrom(ctx context.Context) (Account, bool) {
	a, ok := ctx.Value(accountKey{}).(Account)
	return a, ok
}

// Router 组装路由。
//
// 路由本身来自生成的代码（契约是唯一来源），鉴权在这里统一挂一次：
// 命中公开路径的白名单就放行，其余一律要求有效会话。公开路径只写在下面这一处，
// 新增接口默认受保护，避免"忘了加鉴权"。
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	// 临时：把 /api 的写请求与失败请求打一行日志，排查"点了没反应"。
	r.Use(logRequests(s.log))

	// 先挡跨站写请求，再谈有没有会话：否则攻击者能从 401/403 的差异里
	// 探出"这个会话是不是还有效"。
	r.Use(requireCsrfHeader)
	r.Use(s.requireSession)

	// 默认的 ErrorHandlerFunc 会 http.Error 出纯文本，前端 JSON.parse 直接抛
	// SyntaxError（例如 /api/walls/not-a-uuid）。统一成契约里的错误封套。
	gen.HandlerWithOptions(s, gen.ChiServerOptions{
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求参数不合法")
		},
	})

	// 底图字节不走生成代码：一个 200 响应可能带三种 content type，
	// 契约里写出来只会让 codegen 生成难用的 union。鉴权链是同一个。
	r.Get("/api/plan/underlay/image", s.ServeUnderlayImage)
	r.Get("/ws/state", s.requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.ws.ServeWS(w, r)
	})).ServeHTTP)
	return r
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack 透传底层连接：/ws/state 靠 http.Hijacker 完成协议升级，
// 外层记录状态的结构体不实现它会让握手直接 501。
func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// Flush 同理，透传流式响应能力。
func (w *statusRecorder) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// logRequests 只记录写请求与 4xx/5xx，避免刷屏。
func logRequests(log interface {
	InfoContext(context.Context, string, ...any)
	WarnContext(context.Context, string, ...any)
}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start)
			write := r.Method != http.MethodGet && r.Method != http.MethodHead
			if !write && rec.status < 400 {
				return
			}
			if rec.status >= 400 {
				log.WarnContext(r.Context(), "请求失败", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", elapsed.Milliseconds())
				return
			}
			log.InfoContext(r.Context(), "请求已处理", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", elapsed.Milliseconds())
		})
	}
}

var publicPaths = map[string]bool{
	"/api/healthz":     true,
	"/api/auth/login":  true,
	"/api/auth/logout": true,
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(SessionCookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
			return
		}
		uid, err := s.session.Verify(c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "会话已失效")
			return
		}
		acct, err := s.accounts.AccountByID(r.Context(), uid)
		if err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "账号不存在")
				return
			}
			s.log.ErrorContext(r.Context(), "按 id 查询账号失败", "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "服务内部错误")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accountKey{}, acct)))
	})
}
