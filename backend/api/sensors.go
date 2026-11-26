package api

import (
	"net/http"
	"time"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
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

func (s *Server) GetSimEnv(w http.ResponseWriter, _ *http.Request) {
	if s.simEnv == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "仿真环境未就绪")
		return
	}
	writeJSON(w, http.StatusOK, simEnvToGen(s.simEnv.Get(time.Now())))
}

func (s *Server) UpdateSimEnv(w http.ResponseWriter, r *http.Request) {
	if s.simEnv == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "仿真环境未就绪")
		return
	}
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if account.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "只有管理员可以修改仿真环境")
		return
	}
	var body gen.SimEnvInput
	if !decodeJSONBody(w, r, &body) {
		return
	}
	switch body.Season {
	case gen.SimEnvInputSeasonAuto, gen.SimEnvInputSeasonSpring, gen.SimEnvInputSeasonSummer, gen.SimEnvInputSeasonAutumn, gen.SimEnvInputSeasonWinter:
	default:
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "season 必须是 auto/spring/summer/autumn/winter")
		return
	}
	if body.OutdoorTemp != nil && (*body.OutdoorTemp < -40 || *body.OutdoorTemp > 60) {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "outdoorTemp 必须在 -40 到 60 之间")
		return
	}
	if body.OutdoorHumidity != nil && (*body.OutdoorHumidity < 0 || *body.OutdoorHumidity > 100) {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "outdoorHumidity 必须在 0 到 100 之间")
		return
	}
	snap := s.simEnv.Set(string(body.Season), body.OutdoorTemp, body.OutdoorHumidity, time.Now())
	writeJSON(w, http.StatusOK, simEnvToGen(snap))
}

func simEnvToGen(snap control.SimEnvSnapshot) gen.SimEnv {
	return gen.SimEnv{
		Season:          gen.SimEnvSeason(snap.Season),
		EffectiveSeason: gen.SimEnvEffectiveSeason(snap.EffectiveSeason),
		OutdoorTemp:     snap.OutdoorTemp,
		OutdoorHumidity: snap.OutdoorHumidity,
	}
}
