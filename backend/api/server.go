package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"golang.org/x/crypto/bcrypt"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
)

// SessionCookieName 是会话 Cookie 的名字，与契约里的 securityScheme 一致。
const SessionCookieName = "session"

// Username 是登录时提交的用户名。
type Username = string

// Account 是鉴权需要的最小用户投影，避免接口层依赖生成代码的行结构。
type Account struct {
	ID           openapi_types.UUID
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
}

// AccountStore 读取账号。接口层只依赖这一个方法，方便换成任意实现。
type AccountStore interface {
	AccountByUsername(ctx context.Context, username Username) (Account, error)
	AccountByID(ctx context.Context, id openapi_types.UUID) (Account, error)
}

// ErrAccountNotFound 表示账号不存在。
var ErrAccountNotFound = errors.New("账号不存在")

// Server 实现契约里的全部 handler。
type Server struct {
	gen.Unimplemented

	accounts  AccountStore
	plan      PlanStore
	itemTypes ItemTypeStore
	items     ItemStore
	commands  CommandHandler
	session   *SessionCodec
	secure    bool
	log       *slog.Logger
	version   string
}

// New 构造接口层。secure 为 true 时 Cookie 带 Secure（生产用）。
func New(accounts AccountStore, plan PlanStore, itemTypes ItemTypeStore, items ItemStore, commands CommandHandler, session *SessionCodec, secure bool, version string, log *slog.Logger) *Server {
	return &Server{accounts: accounts, plan: plan, itemTypes: itemTypes, items: items, commands: commands, session: session, secure: secure, version: version, log: log}
}

// dummyHash 是固定口令 "dummy-password" 的 bcrypt 结果，只用来抹平时间差。
var dummyHash = []byte("$2a$12$7rJGAyaJnlHTZ9qVnGoJ9ekDVRyCFU0PeLS4uHMNhhny5gT9R8ukK")

// Healthz 不鉴权、不读库，只证明进程在跑。
func (s *Server) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

// Login 校验口令并设置会话 Cookie。用户不存在与口令错误返回同一个 401，避免枚举账号。
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req gen.LoginRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体解析失败")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "用户名与口令不能为空")
		return
	}
	acct, err := s.accounts.AccountByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			// 账号不存在时也做一次等价开销的哈希比对：否则响应时间
			// 会明显短于「账号存在但口令错」，等于把账号是否存在说出去。
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
			writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "用户名或口令错误")
			return
		}
		s.log.ErrorContext(r.Context(), "查询账号失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "服务内部错误")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.PasswordHash), []byte(req.Password)) != nil {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "用户名或口令错误")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    s.session.Sign(acct.ID),
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.session.TTL() / time.Second),
	})
	writeJSON(w, http.StatusOK, sessionUser(acct))
}

// Logout 让浏览器丢弃会话 Cookie；重复调用无副作用。
func (s *Server) Logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// Me 返回当前会话的用户，供前端启动时判断登录状态。
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	acct, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	writeJSON(w, http.StatusOK, sessionUser(acct))
}

func sessionUser(a Account) gen.SessionUser {
	return gen.SessionUser{
		Id:          a.ID,
		Username:    a.Username,
		DisplayName: a.DisplayName,
		Role:        gen.SessionUserRole(a.Role),
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code gen.ErrorErrorCode, message string) {
	writeJSON(w, status, gen.Error{Error: struct {
		Code    gen.ErrorErrorCode `json:"code"`
		Message string             `json:"message"`
	}{Code: code, Message: message}})
}
