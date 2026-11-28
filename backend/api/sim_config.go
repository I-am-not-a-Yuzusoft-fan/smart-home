package api

import (
	"net/http"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
)

type simConfigPayload struct {
	DelayMs     int     `json:"delayMs"`
	FailureRate float64 `json:"failureRate"`
}

// SimulatorController 由控制层实现，接口层不得直接依赖设备层。
type SimulatorController interface {
	GetSimulatorConfig() control.SimulatorConfig
	UpdateSimulatorConfig(delayMs int, failureRate float64) (control.SimulatorConfig, bool)
}

// GetSimConfig 返回当前模拟器运行参数（延迟与失败率）。
func (s *Server) GetSimConfig(w http.ResponseWriter, _ *http.Request) {
	ctrl, ok := s.commands.(SimulatorController)
	if !ok || ctrl == nil {
		writeJSON(w, http.StatusOK, simConfigPayload{DelayMs: 0, FailureRate: 0})
		return
	}
	cfg := ctrl.GetSimulatorConfig()
	writeJSON(w, http.StatusOK, simConfigPayload{DelayMs: cfg.DelayMs, FailureRate: cfg.FailureRate})
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
	ctrl, ok := s.commands.(SimulatorController)
	if !ok || ctrl == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "模拟器未初始化")
		return
	}
	var body simConfigPayload
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
	updated, ok := ctrl.UpdateSimulatorConfig(body.DelayMs, body.FailureRate)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "当前设备适配器不支持动态调参")
		return
	}
	writeJSON(w, http.StatusOK, simConfigPayload{DelayMs: updated.DelayMs, FailureRate: updated.FailureRate})
}
