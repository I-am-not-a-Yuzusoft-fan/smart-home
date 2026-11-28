package api

import (
	"net/http"
	"time"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/device"
)

// GetSimConfig 返回当前模拟器运行参数（延迟与失败率）。
func (s *Server) GetSimConfig(w http.ResponseWriter, _ *http.Request) {
	if s.simAdapter == nil {
		writeJSON(w, http.StatusOK, device.SimulatorConfig{DelayMs: 0, FailureRate: 0})
		return
	}
	writeJSON(w, http.StatusOK, s.simAdapter.Config())
}

// UpdateSimConfig 动态调整模拟器延迟与失败率（仅管理员可用）。
func (s *Server) UpdateSimConfig(w http.ResponseWriter, r *http.Request) {
	acct, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if acct.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "仅管理员可调整模拟器参数")
		return
	}
	if s.simAdapter == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "模拟器未初始化")
		return
	}
	var body device.SimulatorConfig
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.DelayMs < 0 || body.DelayMs > 5000 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "delayMs 必须在 0–5000 之间")
		return
	}
	if body.FailureRate < 0 || body.FailureRate > 1 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "failureRate 必须在 0–1 之间")
		return
	}
	updated := s.simAdapter.UpdateConfig(time.Duration(body.DelayMs)*time.Millisecond, body.FailureRate)
	writeJSON(w, http.StatusOK, updated)
}
