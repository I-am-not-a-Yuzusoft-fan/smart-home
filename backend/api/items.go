package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
)

// ItemStore 是物品的持久化接缝。
type ItemStore interface {
	ListItems(ctx context.Context, filter itemstore.ItemFilter) ([]itemstore.Item, error)
	GetItem(ctx context.Context, id uuid.UUID) (itemstore.Item, error)
	CreateItem(ctx context.Context, in itemstore.ItemCreate) (itemstore.Item, error)
	UpdateItem(ctx context.Context, in itemstore.ItemUpdate) (itemstore.Item, error)
	DeleteItem(ctx context.Context, id uuid.UUID) (int64, error)
}

// ListItems 按房间、类别与关键字返回物品。
func (s *Server) ListItems(w http.ResponseWriter, r *http.Request, params gen.ListItemsParams) {
	if params.Kind != nil {
		kind := string(*params.Kind)
		if kind != "furniture" && kind != "device" {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "kind 只能是 furniture 或 device")
			return
		}
	}
	var query *string
	if params.Q != nil {
		q := strings.TrimSpace(*params.Q)
		if utf8.RuneCountInString(q) > 64 {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "搜索关键字不能超过 64 个字符")
			return
		}
		if q != "" {
			query = &q
		}
	}
	items, err := s.items.ListItems(r.Context(), itemstore.ItemFilter{
		RoomHash: params.Room,
		Kind:     kindPtr(params.Kind),
		Query:    query,
	})
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取物品失败")
		return
	}
	out := make([]gen.Item, 0, len(items))
	for _, item := range items {
		converted, err := toGenItem(item)
		if err != nil {
			s.log.ErrorContext(r.Context(), "物品数据无法投影", "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取物品失败")
			return
		}
		out = append(out, converted)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateItem 创建物品；设备按类型元数据初始化 capability_state。
func (s *Server) CreateItem(w http.ResponseWriter, r *http.Request) {
	var body gen.ItemInput
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if msg, ok := validateItemCreate(body); !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}
	typ, err := s.itemTypes.GetItemType(r.Context(), body.TypeKey)
	if err != nil {
		if errors.Is(err, ErrItemTypeNotFound) {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "类型不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "读取类型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "创建物品失败")
		return
	}
	roomHash, msg, ok := s.normalizeRoomHash(r.Context(), body.RoomHash)
	if !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}
	attrs, err := marshalAttrs(body.Attrs)
	if err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "attrs 必须是 JSON 对象")
		return
	}
	rotation := 0.0
	if body.Rotation != nil {
		rotation = float64(*body.Rotation)
	}
	item, err := s.items.CreateItem(r.Context(), itemstore.ItemCreate{
		TypeKey:  typ.Key,
		Name:     strings.TrimSpace(body.Name),
		RoomHash: roomHash,
		X:        float64(body.X),
		Y:        float64(body.Y),
		Rotation: rotation,
		Status:   itemStatusValue(body.Status),
		Attrs:    attrs,
		States:   initialStates(typ.Capabilities),
	})
	if err != nil {
		s.log.ErrorContext(r.Context(), "创建物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "创建物品失败")
		return
	}
	converted, err := toGenItem(item)
	if err != nil {
		s.log.ErrorContext(r.Context(), "物品数据无法投影", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "创建物品失败")
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

// UpdateItem 修改物品的字段；未出现的字段保持原值。
func (s *Server) UpdateItem(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	var body gen.ItemUpdate
	if !decodeJSONBody(w, r, &body) {
		return
	}
	itemID := uuid.UUID(id)
	existing, err := s.items.GetItem(r.Context(), itemID)
	if err != nil {
		if errors.Is(err, itemstore.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "物品不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新物品失败")
		return
	}

	update := itemstore.ItemUpdate{ID: itemID}
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" || utf8.RuneCountInString(name) > 64 {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "物品名称必须在 1–64 个字符之间")
			return
		}
		update.NameSet = true
		update.Name = name
	}
	if body.RoomHash != nil {
		roomHash, msg, ok := s.normalizeRoomHash(r.Context(), body.RoomHash)
		if !ok {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
			return
		}
		update.RoomSet = true
		if roomHash != nil {
			update.RoomHash = *roomHash
		}
	}
	if body.X != nil || body.Y != nil || body.Rotation != nil {
		x, y, rotation := existing.X, existing.Y, existing.Rotation
		if body.X != nil {
			x = float64(*body.X)
		}
		if body.Y != nil {
			y = float64(*body.Y)
		}
		if body.Rotation != nil {
			rotation = float64(*body.Rotation)
		}
		if msg, ok := validatePosition(x, y, rotation); !ok {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
			return
		}
		update.PositionSet = true
		update.X, update.Y, update.Rotation = x, y, rotation
	}
	if body.Status != nil {
		status := string(*body.Status)
		if status != "" && status != "normal" && status != "idle" && status != "maintenance" {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "status 不合法")
			return
		}
		update.StatusSet = true
		update.Status = status
	}
	if body.Attrs != nil {
		attrs, err := marshalAttrs(body.Attrs)
		if err != nil {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "attrs 必须是 JSON 对象")
			return
		}
		update.AttrsSet = true
		update.Attrs = attrs
	}

	updated, err := s.items.UpdateItem(r.Context(), update)
	if err != nil {
		if errors.Is(err, itemstore.ErrItemNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "物品不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "更新物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新物品失败")
		return
	}
	converted, err := toGenItem(updated)
	if err != nil {
		s.log.ErrorContext(r.Context(), "物品数据无法投影", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新物品失败")
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

// DeleteItem 删除物品；capability_state 随外键级联删除，state_history 保留。
func (s *Server) DeleteItem(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	deleted, err := s.items.DeleteItem(r.Context(), uuid.UUID(id))
	if err != nil {
		s.log.ErrorContext(r.Context(), "删除物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除物品失败")
		return
	}
	if deleted == 0 {
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "物品不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func kindPtr(kind *gen.ListItemsParamsKind) *string {
	if kind == nil {
		return nil
	}
	value := string(*kind)
	return &value
}

// normalizeRoomHash 只允许已命名的房间：item.room_hash 指向 room_name。
func (s *Server) normalizeRoomHash(ctx context.Context, raw *string) (*string, string, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, "", true
	}
	hash := strings.TrimSpace(*raw)
	names, err := s.plan.LoadRoomNames(ctx)
	if err != nil {
		return nil, "读取房间失败", false
	}
	for _, name := range names {
		if name.Hash == hash {
			return &hash, "", true
		}
	}
	return nil, "房间不存在或还没有命名", false
}

func validateItemCreate(body gen.ItemInput) (string, bool) {
	if strings.TrimSpace(body.TypeKey) == "" {
		return "typeKey 不能为空", false
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "物品名称必须在 1–64 个字符之间", false
	}
	rotation := 0.0
	if body.Rotation != nil {
		rotation = float64(*body.Rotation)
	}
	if msg, ok := validatePosition(float64(body.X), float64(body.Y), rotation); !ok {
		return msg, false
	}
	if body.Status != nil && !validStatus(string(*body.Status)) {
		return "status 不合法", false
	}
	return "", true
}

func validatePosition(x, y, rotation float64) (string, bool) {
	for _, value := range []float64{x, y} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 5000 {
			return "坐标必须在 0–5000 厘米之间", false
		}
	}
	if math.IsNaN(rotation) || math.IsInf(rotation, 0) || rotation < 0 || rotation > 360 {
		return "旋转角度必须在 0–360 度之间", false
	}
	return "", true
}

func validStatus(status string) bool {
	return status == "normal" || status == "idle" || status == "maintenance"
}

func itemStatusValue(status *gen.ItemInputStatus) *string {
	if status == nil || *status == "" {
		return nil
	}
	value := string(*status)
	return &value
}

func marshalAttrs(attrs *map[string]interface{}) (json.RawMessage, error) {
	if attrs == nil {
		return json.RawMessage(`{}`), nil
	}
	raw, err := json.Marshal(*attrs)
	if err != nil {
		return nil, err
	}
	var check map[string]interface{}
	if err := json.Unmarshal(raw, &check); err != nil {
		return nil, err
	}
	return raw, nil
}

// initialStates 从类型元数据推导设备的初始 capability_state。
func initialStates(capabilities []Capability) []itemstore.InitialState {
	states := make([]itemstore.InitialState, 0, len(capabilities))
	for _, capability := range capabilities {
		value := capability.Default
		if value == nil {
			switch capability.Type {
			case "bool":
				value = false
			case "number":
				if capability.Min != nil {
					value = *capability.Min
				} else {
					value = 0
				}
			case "enum":
				if len(capability.Options) > 0 {
					value = capability.Options[0].Value
				} else {
					value = ""
				}
			case "string":
				value = ""
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			continue
		}
		states = append(states, itemstore.InitialState{Key: capability.Key, Value: raw})
	}
	return states
}

func toGenItem(item itemstore.Item) (gen.Item, error) {
	var attrs *map[string]interface{}
	if len(item.Attrs) > 0 && string(item.Attrs) != "null" {
		var value map[string]interface{}
		if err := json.Unmarshal(item.Attrs, &value); err != nil {
			return gen.Item{}, err
		}
		attrs = &value
	}
	var status *gen.ItemStatus
	if item.Status != nil {
		value := gen.ItemStatus(*item.Status)
		status = &value
	}
	return gen.Item{
		Id:       openapi_types.UUID(item.ID),
		TypeKey:  item.TypeKey,
		Kind:     gen.ItemKind(item.Kind),
		Name:     item.Name,
		RoomHash: item.RoomHash,
		RoomName: item.RoomName,
		X:        item.X,
		Y:        item.Y,
		Rotation: item.Rotation,
		Status:   status,
		Attrs:    attrs,
	}, nil
}
