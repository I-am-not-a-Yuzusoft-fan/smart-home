package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if typ.Kind == "device" && body.Status != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, deviceStatusMessage)
		return
	}
	// 挂墙类型（窗帘、壁挂空调）没有墙就没有位置：服务端兜底，
	// 不依赖前端一定把落点吸到墙上——换个客户端或旧版页面也不会造出悬空物品。
	if typ.Mount == "wall" && body.WallId == nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED,
			fmt.Sprintf("「%s」只能挂在墙上：请把落点放到某面墙边", typ.Name))
		return
	}
	// 挂墙物品：位置与朝向由墙决定，x/y/rotation 只是派生值。
	placeX, placeY, placeRot := float64(body.X), float64(body.Y), 0.0
	if body.Rotation != nil {
		placeRot = float64(*body.Rotation)
	}
	var wallID *uuid.UUID
	var wallOffset *float64
	if body.WallId != nil {
		wall, err := s.plan.GetWall(r.Context(), *body.WallId)
		if err != nil {
			if errors.Is(err, ErrWallNotFound) {
				writeError(w, http.StatusNotFound, gen.NOTFOUND, "没有这面墙")
				return
			}
			s.log.ErrorContext(r.Context(), "读取墙失败", "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "创建物品失败")
			return
		}
		offset := 0.0
		if body.WallOffset != nil {
			offset = *body.WallOffset
		}
		x, y, rotation, clamped := placeOnWall(wall, offset, typ.DefaultWidth)
		placeX, placeY, placeRot = x, y, rotation
		wallOffset = &clamped
		id := uuid.UUID(*body.WallId)
		wallID = &id
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
	item, err := s.items.CreateItem(r.Context(), itemstore.ItemCreate{
		TypeKey:    typ.Key,
		Name:       strings.TrimSpace(body.Name),
		RoomHash:   roomHash,
		X:          placeX,
		Y:          placeY,
		Rotation:   placeRot,
		Status:     itemStatusValue(body.Status),
		Attrs:      attrs,
		States:     initialStates(typ.Capabilities),
		WallID:     wallID,
		WallOffset: wallOffset,
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

	if existing.Kind == "device" && body.Status != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, deviceStatusMessage)
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
	// 挂墙物品的位置是墙的函数：直接改 x/y 会让 wallOffset 与坐标对不上。
	// 另外类型要求挂墙的物品即使 wall_id 为空（早期数据、或墙被删后没重挂）也不能
	// 用地板坐标摆放——否则旧客户端一次 PATCH x/y 就能把窗帘停到房间中间。
	if body.WallId == nil && (body.X != nil || body.Y != nil || body.Rotation != nil) {
		if existing.WallID != nil {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED,
				"挂墙物品的位置由墙决定：改 wallOffset 沿墙移动，或把 wallId 设为空串先脱离墙面")
			return
		}
		if typ, err := s.itemTypes.GetItemType(r.Context(), existing.TypeKey); err == nil && typ.Mount == "wall" {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED,
				fmt.Sprintf("「%s」要挂在墙上：把它拖到某面墙边（40 像素内）就会贴上去", typ.Name))
			return
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

	// 墙上物品的三种写入：给定 wallId 重新贴墙、wallId 空串脱离、只给 wallOffset 沿墙挪。
	detach := body.WallId != nil && *body.WallId == ""
	var targetWall *openapi_types.UUID
	if body.WallId != nil && !detach {
		parsed, err := uuid.Parse(*body.WallId)
		if err != nil {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "wallId 不是合法的 uuid")
			return
		}
		u := openapi_types.UUID(parsed)
		targetWall = &u
	}
	if targetWall == nil && body.WallOffset != nil && existing.WallID != nil {
		id := openapi_types.UUID(*existing.WallID)
		targetWall = &id
	}
	switch {
	case detach:
		// 挂墙类型不能脱离墙面：窗帘飘在房间中间没有意义。
		if typ, err := s.itemTypes.GetItemType(r.Context(), existing.TypeKey); err == nil && typ.Mount == "wall" {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED,
				fmt.Sprintf("「%s」不能脱离墙面：请把它挪到另一面墙上", typ.Name))
			return
		}
		// 空串 = 脱离墙面，回到自由摆放；位置保持当前值。
		update.WallSet = true
		update.WallID = nil
		update.WallOffset = nil
	case targetWall != nil:
		wall, err := s.plan.GetWall(r.Context(), *targetWall)
		if err != nil {
			if errors.Is(err, ErrWallNotFound) {
				writeError(w, http.StatusNotFound, gen.NOTFOUND, "没有这面墙")
				return
			}
			s.log.ErrorContext(r.Context(), "读取墙失败", "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新物品失败")
			return
		}
		typ, err := s.itemTypes.GetItemType(r.Context(), existing.TypeKey)
		if err != nil {
			s.log.ErrorContext(r.Context(), "读取类型失败", "err", err)
			writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新物品失败")
			return
		}
		offset := 0.0
		if body.WallOffset != nil {
			offset = *body.WallOffset
		}
		x, y, rotation, clamped := placeOnWall(wall, offset, typ.DefaultWidth)
		wallUUID := uuid.UUID(*targetWall)
		update.WallSet = true
		update.WallID = &wallUUID
		update.WallOffset = &clamped
		update.PositionSet = true
		update.X, update.Y, update.Rotation = x, y, rotation
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

// deviceStatusMessage 说明为什么设备不能写 status。
//
// 设备的状态在 capability_state（每项能力一条），item.status 是给家具用的
// 「正常/闲置/维修」。允许两边都写就会出现两份真相，读的人不知道该信哪个。
const deviceStatusMessage = "设备不使用 status 字段：设备状态在 capability_state 里（如 on、brightness），item.status 只用于家具"

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
		Id:         openapi_types.UUID(item.ID),
		TypeKey:    item.TypeKey,
		Kind:       gen.ItemKind(item.Kind),
		Name:       item.Name,
		RoomHash:   item.RoomHash,
		RoomName:   item.RoomName,
		X:          item.X,
		Y:          item.Y,
		Rotation:   item.Rotation,
		Status:     status,
		Attrs:      attrs,
		WallId:     uuidPtr(item.WallID),
		WallOffset: item.WallOffset,
	}, nil
}

// uuidPtr 把 store 层的可空 uuid 投成契约里的可空字段。
func uuidPtr(id *uuid.UUID) *openapi_types.UUID {
	if id == nil {
		return nil
	}
	v := openapi_types.UUID(*id)
	return &v
}
