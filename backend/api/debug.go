package api

import (
	"encoding/json"
	"net/http"
)

type debugEvent struct {
	Tag  string         `json:"tag"`
	Data map[string]any `json:"data"`
}

// DebugEvent 临时接口：把前端交互写进服务端日志，便于排查"点了没反应"。
func (s *Server) DebugEvent(w http.ResponseWriter, r *http.Request) {
	var ev debugEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "请求体不是合法的 JSON")
		return
	}
	s.log.InfoContext(r.Context(), "前端事件", "tag", ev.Tag, "data", ev.Data)
	w.WriteHeader(http.StatusNoContent)
}
