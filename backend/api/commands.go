package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

type CommandHandler interface {
	Handle(context.Context, control.Command) (control.CommandResult, error)
	ListLogs(context.Context, int32) ([]storegen.ListCommandLogsRow, error)
	ListStates(context.Context, uuid.UUID) ([]storegen.CapabilityState, error)
	ListHistory(context.Context, uuid.UUID, *string, int32) ([]storegen.StateHistory, error)
}

func (s *Server) SendItemCommand(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	var body gen.CommandInput
	if !decodeJSONBody(w, r, &body) {
		return
	}
	account, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	accountID, err := uuid.Parse(account.ID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "会话用户数据无效")
		return
	}
	result, err := s.commands.Handle(r.Context(), control.Command{
		RequestID:     uuid.UUID(body.RequestId),
		ItemID:        uuid.UUID(id),
		CapabilityKey: body.CapabilityKey,
		Value:         body.Value,
		CreatedBy:     accountID,
	})
	if err != nil {
		var commandErr *control.CommandError
		if errors.As(err, &commandErr) {
			s.log.InfoContext(r.Context(), "命令校验错误", "code", commandErr.Code, "message", commandErr.Message)
			status := http.StatusBadRequest
			code := gen.VALIDATIONFAILED
			switch commandErr.Code {
			case "NOT_FOUND":
				status, code = http.StatusNotFound, gen.NOTFOUND
			case "RANGE_OUT_OF_BOUNDS":
				status, code = http.StatusBadRequest, gen.RANGEOUTOFBOUNDS
			case "VALIDATION_FAILED":
				status, code = http.StatusBadRequest, gen.VALIDATIONFAILED
			case "DEVICE_UNAVAILABLE":
				status, code = http.StatusServiceUnavailable, gen.DEVICEUNAVAILABLE
			case "DEVICE_TIMEOUT":
				status, code = http.StatusServiceUnavailable, gen.DEVICETIMEOUT
			}
			writeError(w, status, code, commandErr.Message)
			return
		}
		s.log.ErrorContext(r.Context(), "下发设备命令失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "命令执行失败")
		return
	}
	writeJSON(w, http.StatusAccepted, gen.CommandResult{
		RequestId: openapi_types.UUID(result.RequestID),
		Status:    gen.CommandResultStatusOk,
		Value:     result.Value,
	})
}

func (s *Server) ListCommandLogs(w http.ResponseWriter, r *http.Request, params gen.ListCommandLogsParams) {
	limit := int32(50)
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 200 {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "limit 必须在 1–200")
			return
		}
		limit = int32(*params.Limit)
	}
	logs, err := s.commands.ListLogs(r.Context(), limit)
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取操作记录失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取操作记录失败")
		return
	}
	out := make([]gen.CommandLog, 0, len(logs))
	for _, log := range logs {
		row := gen.CommandLog{
			Id:        log.ID,
			RequestId: openapi_types.UUID(log.RequestID.Bytes),
			CreatedAt: log.CreatedAt.Time,
			Status:    gen.CommandLogStatus(log.Status),
			Source:    gen.CommandLogSource(log.Source),
		}
		if log.ItemID.Valid {
			id := openapi_types.UUID(log.ItemID.Bytes)
			row.ItemId = &id
		}
		if log.CapabilityKey.Valid {
			row.CapabilityKey = &log.CapabilityKey.String
		}
		if len(log.Target) > 0 {
			row.Target = log.Target
		}
		if log.ErrorCode.Valid {
			code := log.ErrorCode.String
			row.ErrorCode = &code
		}
		if log.Error.Valid {
			row.Error = &log.Error.String
		}
		if log.CompletedAt.Valid {
			row.CompletedAt = &log.CompletedAt.Time
		}
		if log.DurationMs.Valid {
			durationMs := int(log.DurationMs.Int32)
			row.DurationMs = &durationMs
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ListItemHistory(w http.ResponseWriter, r *http.Request, id openapi_types.UUID, params gen.ListItemHistoryParams) {
	if _, err := s.items.GetItem(r.Context(), uuid.UUID(id)); err != nil {
		if errors.Is(err, itemstore.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "物品不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取物品失败")
		return
	}
	limit := int32(100)
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 500 {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "limit 必须在 1–500")
			return
		}
		limit = int32(*params.Limit)
	}
	history, err := s.commands.ListHistory(r.Context(), uuid.UUID(id), params.Key, limit)
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取状态历史失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取状态历史失败")
		return
	}
	out := make([]gen.StateHistory, 0, len(history))
	for _, row := range history {
		var value any
		if err := json.Unmarshal(row.Value, &value); err != nil {
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取状态历史失败")
			return
		}
		out = append(out, gen.StateHistory{
			Id:            int64(row.ID),
			ItemId:        openapi_types.UUID(row.ItemID.Bytes),
			CapabilityKey: row.CapabilityKey,
			Value:         value,
			Source:        gen.StateHistorySource(row.Source),
			RecordedAt:    row.RecordedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ListItemStates(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	if _, err := s.items.GetItem(r.Context(), uuid.UUID(id)); err != nil {
		if errors.Is(err, itemstore.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "物品不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取物品失败")
		return
	}
	states, err := s.commands.ListStates(r.Context(), uuid.UUID(id))
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取设备状态失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取设备状态失败")
		return
	}
	out := make([]gen.CapabilityState, 0, len(states))
	for _, state := range states {
		var value any
		if err := json.Unmarshal(state.Value, &value); err != nil {
			s.log.ErrorContext(r.Context(), "设备状态值不是合法 JSON", "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取设备状态失败")
			return
		}
		out = append(out, gen.CapabilityState{
			ItemId:        openapi_types.UUID(state.ItemID.Bytes),
			CapabilityKey: state.CapabilityKey,
			Value:         value,
			Source:        gen.CapabilityStateSource(state.Source),
			UpdatedAt:     state.UpdatedAt.Time,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
