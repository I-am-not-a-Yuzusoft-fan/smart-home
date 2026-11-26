package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

func (s *Server) ListModes(w http.ResponseWriter, r *http.Request) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	modes, err := s.modes.ListModes(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取场景列表失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取场景列表失败")
		return
	}
	out := make([]gen.Mode, 0, len(modes))
	for _, m := range modes {
		item, err := modeToGen(m)
		if err != nil {
			s.log.ErrorContext(r.Context(), "解析场景定义失败", "modeId", uuid.UUID(m.ID.Bytes).String(), "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "解析场景定义失败")
			return
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetMode(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	m, err := s.modes.GetMode(r.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, control.ErrModeNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "场景不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "读取场景详情失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取场景详情失败")
		return
	}
	item, err := modeToGen(m)
	if err != nil {
		s.log.ErrorContext(r.Context(), "解析场景定义失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "解析场景定义失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) RunMode(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	// 请求体可选（可传 {requestId} 或空体）
	if r.Body != nil {
		var body gen.RunModeRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体解析失败")
			return
		}
	}
	accountID, err := uuid.Parse(account.ID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "会话用户数据无效")
		return
	}
	detail, err := s.modes.Start(r.Context(), uuid.UUID(id), accountID, "manual")
	if err != nil {
		if errors.Is(err, control.ErrModeNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "场景不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "触发场景执行失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "触发场景执行失败")
		return
	}
	writeJSON(w, http.StatusAccepted, runDetailToGen(detail))
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	detail, err := s.modes.GetRun(r.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, control.ErrRunNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "执行记录不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "查询执行记录失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "查询执行记录失败")
		return
	}
	writeJSON(w, http.StatusOK, runDetailToGen(detail))
}

func (s *Server) CancelRun(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	detail, err := s.modes.Cancel(r.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, control.ErrRunNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "执行记录不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "取消场景执行失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "取消场景执行失败")
		return
	}
	writeJSON(w, http.StatusOK, runDetailToGen(detail))
}

func modeToGen(m storegen.Mode) (gen.Mode, error) {
	var def gen.ModeDefinition
	if err := json.Unmarshal(m.Definition, &def); err != nil {
		return gen.Mode{}, err
	}
	out := gen.Mode{
		Id:         openapi_types.UUID(m.ID.Bytes),
		Name:       m.Name,
		Definition: def,
		RunMode:    gen.ModeRunMode(m.RunMode),
		Enabled:    m.Enabled,
		CreatedAt:  m.CreatedAt.Time,
		UpdatedAt:  m.UpdatedAt.Time,
	}
	if m.CreatedBy.Valid {
		uid := openapi_types.UUID(m.CreatedBy.Bytes)
		out.CreatedBy = &uid
	}
	return out, nil
}

func runDetailToGen(d control.RunDetail) gen.Run {
	steps := make([]gen.RunStep, 0, len(d.Steps))
	for _, st := range d.Steps {
		row := gen.RunStep{
			RunId:  openapi_types.UUID(st.RunID.Bytes),
			NodeId: st.NodeID,
			Path:   st.Path,
			Status: gen.RunStepStatus(st.Status),
		}
		if st.Alias.Valid {
			alias := st.Alias.String
			row.Alias = &alias
		}
		if st.Error.Valid {
			msg := st.Error.String
			row.Error = &msg
		}
		if st.StartedAt.Valid {
			t := st.StartedAt.Time
			row.StartedAt = &t
		}
		if st.FinishedAt.Valid {
			t := st.FinishedAt.Time
			row.FinishedAt = &t
		}
		steps = append(steps, row)
	}
	out := gen.Run{
		Id:        openapi_types.UUID(d.Run.ID.Bytes),
		Status:    gen.RunStatus(d.Run.Status),
		Source:    gen.RunSource(d.Run.Source),
		StartedAt: d.Run.StartedAt.Time,
		Steps:     steps,
	}
	if d.Run.ModeID.Valid {
		mid := openapi_types.UUID(d.Run.ModeID.Bytes)
		out.ModeId = &mid
	}
	if d.Run.FinishedAt.Valid {
		t := d.Run.FinishedAt.Time
		out.FinishedAt = &t
	}
	return out
}
