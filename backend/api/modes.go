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

func (s *Server) CreateMode(w http.ResponseWriter, r *http.Request) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if account.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "只有管理员可以新建场景")
		return
	}
	var body gen.ModeInput
	if !decodeJSONBody(w, r, &body) {
		return
	}
	defRaw, err := json.Marshal(body.Definition)
	if err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "场景定义序列化失败")
		return
	}
	runMode := "restart"
	if body.RunMode != nil {
		runMode = string(*body.RunMode)
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	accountID, err := uuid.Parse(account.ID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "会话用户数据无效")
		return
	}
	created, err := s.modes.CreateMode(r.Context(), control.ModeWriteInput{
		Name:       body.Name,
		Definition: defRaw,
		RunMode:    runMode,
		Enabled:    enabled,
		CreatedBy:  accountID,
	})
	if err != nil {
		s.writeModeWriteError(w, r, err, "创建场景失败")
		return
	}
	out, err := modeToGen(created)
	if err != nil {
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "解析场景定义失败")
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) UpdateMode(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if account.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "只有管理员可以修改场景")
		return
	}
	var body gen.ModeInput
	if !decodeJSONBody(w, r, &body) {
		return
	}
	defRaw, err := json.Marshal(body.Definition)
	if err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "场景定义序列化失败")
		return
	}
	runMode := "restart"
	if body.RunMode != nil {
		runMode = string(*body.RunMode)
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	updated, err := s.modes.UpdateMode(r.Context(), uuid.UUID(id), control.ModeWriteInput{
		Name:       body.Name,
		Definition: defRaw,
		RunMode:    runMode,
		Enabled:    enabled,
	})
	if err != nil {
		s.writeModeWriteError(w, r, err, "更新场景失败")
		return
	}
	out, err := modeToGen(updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "解析场景定义失败")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) DeleteMode(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if s.modes == nil {
		writeError(w, http.StatusServiceUnavailable, gen.INTERNAL, "场景执行器未就绪")
		return
	}
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if account.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "只有管理员可以删除场景")
		return
	}
	if err := s.modes.DeleteMode(r.Context(), uuid.UUID(id)); err != nil {
		if errors.Is(err, control.ErrModeNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "场景不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "删除场景失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除场景失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeModeWriteError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	switch {
	case errors.Is(err, control.ErrModeValidation):
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, err.Error())
	case errors.Is(err, control.ErrModeNameConflict):
		writeError(w, http.StatusConflict, gen.CONFLICT, err.Error())
	case errors.Is(err, control.ErrModeNotFound):
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "场景不存在")
	default:
		s.log.ErrorContext(r.Context(), fallback, "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, fallback)
	}
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
		if errors.Is(err, control.ErrModeAlreadyRunning) {
			writeError(w, http.StatusConflict, gen.CONFLICT, err.Error())
			return
		}
		if errors.Is(err, control.ErrRunModeUnsupported) {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, err.Error())
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
		if st.Note.Valid {
			note := st.Note.String
			row.Note = &note
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
	if d.Reason != "" {
		reason := d.Reason
		out.Reason = &reason
	}
	return out
}
