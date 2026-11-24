package api

import (
	"net/http"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
)

func (s *Server) AdvanceSimulation(w http.ResponseWriter, r *http.Request) {
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if account.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "只有管理员可以推进模拟时间")
		return
	}
	var body gen.AdvanceSimulationJSONRequestBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.Minutes < 1 || body.Minutes > 1440 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "minutes 必须在 1–1440")
		return
	}
	simulatedAt, err := s.sensors.Advance(r.Context(), body.Minutes)
	if err != nil {
		s.log.ErrorContext(r.Context(), "推进模拟时间失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "推进模拟时间失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"minutes":     body.Minutes,
		"simulatedAt": simulatedAt,
	})
}
