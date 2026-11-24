package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/device"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

var (
	ErrItemNotFound = errors.New("物品不存在")
	ErrTypeNotFound = errors.New("类型不存在")
)

type Command struct {
	RequestID     uuid.UUID
	ItemID        uuid.UUID
	CapabilityKey string
	Value         any
	CreatedBy     uuid.UUID
}

type CommandResult struct {
	RequestID uuid.UUID
	Status    string
	Value     any
}

type CommandHandler interface {
	Handle(ctx context.Context, command Command) (CommandResult, error)
	ListLogs(ctx context.Context, limit int32) ([]storegen.ListCommandLogsRow, error)
	ListStates(ctx context.Context, itemID uuid.UUID) ([]storegen.CapabilityState, error)
	ListHistory(ctx context.Context, itemID uuid.UUID, capabilityKey *string, limit int32) ([]storegen.StateHistory, error)
}

type Handler struct {
	pool        *pgxpool.Pool
	adapter     device.Adapter
	timeout     time.Duration
	broadcaster Broadcaster
}

type Broadcaster interface {
	Broadcast(StateMessage)
}

type StateMessage struct {
	ItemID        uuid.UUID
	CapabilityKey string
	Value         any
	Source        string
	UpdatedAt     time.Time
}

func NewCommandHandler(pool *pgxpool.Pool, adapter device.Adapter, timeout time.Duration) *Handler {
	return &Handler{pool: pool, adapter: adapter, timeout: timeout}
}

func (h *Handler) SetBroadcaster(broadcaster Broadcaster) {
	h.broadcaster = broadcaster
}

func (h *Handler) ListLogs(ctx context.Context, limit int32) ([]storegen.ListCommandLogsRow, error) {
	return storegen.New(h.pool).ListCommandLogs(ctx, limit)
}

func (h *Handler) ListStates(ctx context.Context, itemID uuid.UUID) ([]storegen.CapabilityState, error) {
	return storegen.New(h.pool).ListStatesByItem(ctx, pgUUID(itemID))
}

func (h *Handler) ListHistory(ctx context.Context, itemID uuid.UUID, capabilityKey *string, limit int32) ([]storegen.StateHistory, error) {
	key := pgTextPtr(capabilityKey)
	return storegen.New(h.pool).ListStateHistory(ctx, storegen.ListStateHistoryParams{
		ItemID:        pgUUID(itemID),
		CapabilityKey: key,
		Limit:         limit,
	})
}

func (h *Handler) Handle(ctx context.Context, command Command) (CommandResult, error) {
	q := storegen.New(h.pool)
	requestID := pgUUID(command.RequestID)
	if existing, err := q.GetCommandByRequestID(ctx, requestID); err == nil {
		if existing.Status == "ok" {
			var value any
			_ = json.Unmarshal(existing.Target, &value)
			return CommandResult{RequestID: command.RequestID, Status: "ok", Value: value}, nil
		}
		return CommandResult{}, &CommandError{Code: replayErrorCode(existing), Message: text(existing.Error)}
	}

	item, err := q.GetItem(ctx, pgUUID(command.ItemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandResult{}, &CommandError{Code: "NOT_FOUND", Message: "物品不存在"}
	}
	if err != nil {
		return CommandResult{}, err
	}
	typ, err := q.GetItemType(ctx, item.TypeKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandResult{}, &CommandError{Code: "NOT_FOUND", Message: "类型不存在"}
	}
	if err != nil {
		return CommandResult{}, err
	}
	if typ.Kind != "device" {
		return CommandResult{}, &CommandError{Code: "VALIDATION_FAILED", Message: "家具没有设备能力"}
	}

	var capabilities []struct {
		Key     string   `json:"key"`
		Type    string   `json:"type"`
		Min     *float64 `json:"min"`
		Max     *float64 `json:"max"`
		Step    *float64 `json:"step"`
		Options []struct {
			Value string `json:"value"`
		} `json:"options"`
		Writable bool `json:"writable"`
	}
	if err := json.Unmarshal(typ.Capabilities, &capabilities); err != nil {
		return CommandResult{}, err
	}
	var capability *struct {
		Key     string   `json:"key"`
		Type    string   `json:"type"`
		Min     *float64 `json:"min"`
		Max     *float64 `json:"max"`
		Step    *float64 `json:"step"`
		Options []struct {
			Value string `json:"value"`
		} `json:"options"`
		Writable bool `json:"writable"`
	}
	for i := range capabilities {
		if capabilities[i].Key == command.CapabilityKey {
			capability = &capabilities[i]
			break
		}
	}
	if capability == nil {
		return CommandResult{}, &CommandError{Code: "VALIDATION_FAILED", Message: "该物品没有能力项 " + command.CapabilityKey}
	}
	if !capability.Writable {
		return CommandResult{}, &CommandError{Code: "VALIDATION_FAILED", Message: "能力项 " + command.CapabilityKey + " 不可写"}
	}
	if msg, code, ok := validateValue(command.Value, capability); !ok {
		target, _ := json.Marshal(command.Value)
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			return CommandResult{}, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		txq := q.WithTx(tx)
		now := time.Now()
		if _, err := txq.InsertCommand(ctx, storegen.InsertCommandParams{
			RequestID:     pgUUID(command.RequestID),
			ItemID:        pgUUID(command.ItemID),
			CapabilityKey: pgText(command.CapabilityKey),
			Target:        target,
			Status:        "rejected",
			Source:        "manual",
			CreatedBy:     pgUUID(command.CreatedBy),
		}); err != nil {
			return CommandResult{}, err
		}
		if err := txq.CompleteCommand(ctx, storegen.CompleteCommandParams{
			RequestID:   pgUUID(command.RequestID),
			Status:      "rejected",
			Error:       pgText(msg),
			ErrorCode:   pgText(code),
			CompletedAt: pgTime(now),
			DurationMs:  pgInt32(0),
		}); err != nil {
			return CommandResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, &CommandError{Code: code, Message: msg}
	}

	target, _ := json.Marshal(command.Value)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return CommandResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txq := q.WithTx(tx)
	startedAt := time.Now()
	if _, err := txq.InsertCommand(ctx, storegen.InsertCommandParams{
		RequestID:     requestID,
		ItemID:        pgUUID(command.ItemID),
		CapabilityKey: pgText(command.CapabilityKey),
		Target:        target,
		Status:        "failed",
		Source:        "manual",
		CreatedBy:     pgUUID(command.CreatedBy),
	}); err != nil {
		return CommandResult{}, err
	}

	execCtx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	value, dispatchErr := h.adapter.Dispatch(execCtx, device.Command{
		RequestID: command.RequestID, ItemID: command.ItemID,
		CapabilityKey: command.CapabilityKey, Value: command.Value, Source: "manual",
		Driver: typ.Driver.String,
	})
	if dispatchErr != nil {
		status := "failed"
		code := "DEVICE_UNAVAILABLE"
		message := "设备不可用"
		if errors.Is(dispatchErr, device.ErrTimeout) {
			status = "timeout"
			code = "DEVICE_TIMEOUT"
			message = "设备没有响应，请稍后重试"
		}
		completedAt := time.Now()
		if err := txq.CompleteCommand(ctx, storegen.CompleteCommandParams{
			RequestID: requestID, Status: status, Error: pgText(message), ErrorCode: pgText(code),
			CompletedAt: pgTime(completedAt), DurationMs: pgInt32(int32(completedAt.Sub(startedAt).Milliseconds())),
		}); err != nil {
			return CommandResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, &CommandError{Code: code, Message: message}
	}

	valueJSON, _ := json.Marshal(value)
	completedAt := time.Now()
	if err := txq.UpsertState(ctx, storegen.UpsertStateParams{
		ItemID: pgUUID(command.ItemID), CapabilityKey: command.CapabilityKey,
		Value: valueJSON, Source: "manual", UpdatedAt: pgTime(completedAt),
	}); err != nil {
		return CommandResult{}, err
	}
	if err := txq.InsertHistory(ctx, storegen.InsertHistoryParams{
		ItemID: pgUUID(command.ItemID), CapabilityKey: command.CapabilityKey,
		Value: valueJSON, Source: "manual", RecordedAt: pgTime(completedAt),
	}); err != nil {
		return CommandResult{}, err
	}
	if err := txq.CompleteCommand(ctx, storegen.CompleteCommandParams{
		RequestID: requestID, Status: "ok", Error: pgtype.Text{}, ErrorCode: pgtype.Text{},
		CompletedAt: pgTime(completedAt), DurationMs: pgInt32(int32(completedAt.Sub(startedAt).Milliseconds())),
	}); err != nil {
		return CommandResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommandResult{}, err
	}
	if h.broadcaster != nil {
		h.broadcaster.Broadcast(StateMessage{
			ItemID: command.ItemID, CapabilityKey: command.CapabilityKey,
			Value: value, Source: "manual", UpdatedAt: completedAt,
		})
	}
	return CommandResult{RequestID: command.RequestID, Status: "ok", Value: value}, nil
}

type CommandError struct {
	Code    string
	Message string
}

func (e *CommandError) Error() string { return e.Message }

// replayErrorCode 返回首次执行时的错误码。
// 老数据没有 error_code 列，退回按 status 推一个最接近的码。
func replayErrorCode(existing storegen.GetCommandByRequestIDRow) string {
	if existing.ErrorCode.Valid && existing.ErrorCode.String != "" {
		return existing.ErrorCode.String
	}
	return statusErrorCode(existing.Status)
}

func statusErrorCode(status string) string {
	if status == "rejected" {
		return "RANGE_OUT_OF_BOUNDS"
	}
	if status == "timeout" {
		return "DEVICE_TIMEOUT"
	}
	return "DEVICE_UNAVAILABLE"
}
func text(v pgtype.Text) string {
	if v.Valid {
		return v.String
	}
	return ""
}
func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
func pgText(v string) pgtype.Text     { return pgtype.Text{String: v, Valid: v != ""} }
func pgTextPtr(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}
func pgTime(v time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: v, Valid: true} }
func pgInt32(v int32) pgtype.Int4           { return pgtype.Int4{Int32: v, Valid: true} }

func validateValue(value any, capability *struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
	Step    *float64 `json:"step"`
	Options []struct {
		Value string `json:"value"`
	} `json:"options"`
	Writable bool `json:"writable"`
}) (string, string, bool) {
	switch capability.Type {
	case "bool":
		_, ok := value.(bool)
		if !ok {
			return fmt.Sprintf("「%s」必须是布尔值", capability.Key), "VALIDATION_FAILED", false
		}
	case "number":
		n, ok := value.(float64)
		if !ok {
			return fmt.Sprintf("「%s」必须是数字", capability.Key), "VALIDATION_FAILED", false
		}
		if capability.Min != nil && n < *capability.Min {
			return fmt.Sprintf("「%s」不能小于 %v", capability.Key, *capability.Min), "RANGE_OUT_OF_BOUNDS", false
		}
		if capability.Max != nil && n > *capability.Max {
			return fmt.Sprintf("「%s」不能大于 %v", capability.Key, *capability.Max), "RANGE_OUT_OF_BOUNDS", false
		}
	case "enum":
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("「%s」必须是字符串枚举值", capability.Key), "VALIDATION_FAILED", false
		}
		for _, option := range capability.Options {
			if option.Value == s {
				return "", "", true
			}
		}
		return fmt.Sprintf("「%s」的值不在允许选项中", capability.Key), "VALIDATION_FAILED", false
	case "string":
		_, ok := value.(string)
		if !ok {
			return fmt.Sprintf("「%s」必须是字符串", capability.Key), "VALIDATION_FAILED", false
		}
	default:
		return "能力类型不受支持", "VALIDATION_FAILED", false
	}
	return "", "", true
}
