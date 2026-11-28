package api

import (
	"encoding/json"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
)

type wallSnapshot struct {
	Plan       gen.Plan              `json:"plan"`
	Items      []gen.Item            `json:"items"`
	Types      []gen.ItemType        `json:"types"`
	States     []gen.CapabilityState `json:"states"`
	SimEnv     *gen.SimEnv           `json:"simEnv,omitempty"`
	ServerTime time.Time             `json:"serverTime"`
}

// GetWallSnapshot 免登录只读挂墙屏聚合快照（FR-4.7）。
// 仅返回户型、物品、类型、设备当前状态与仿真季节，不返回账号、操作日志或写接口。
func (s *Server) GetWallSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	plan, err := s.readPlan(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "挂墙屏读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	rawItems, err := s.items.ListItems(ctx, itemstore.ItemFilter{})
	if err != nil {
		s.log.ErrorContext(ctx, "挂墙屏读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取物品失败")
		return
	}
	items := make([]gen.Item, 0, len(rawItems))
	for _, it := range rawItems {
		if converted, err := toGenItem(it); err == nil {
			items = append(items, converted)
		}
	}
	rawTypes, err := s.itemTypes.ListItemTypes(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "挂墙屏读取类型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取类型失败")
		return
	}
	types := make([]gen.ItemType, 0, len(rawTypes))
	for _, typ := range rawTypes {
		types = append(types, toGenItemType(typ))
	}

	states := make([]gen.CapabilityState, 0)
	if s.commands != nil {
		if rawStates, err := s.commands.ListAllStates(ctx); err == nil {
			for _, st := range rawStates {
				var val any
				if json.Unmarshal(st.Value, &val) == nil {
					states = append(states, gen.CapabilityState{
						ItemId:        openapi_types.UUID(st.ItemID.Bytes),
						CapabilityKey: st.CapabilityKey,
						Value:         val,
						Source:        gen.CapabilityStateSource(st.Source),
						UpdatedAt:     st.UpdatedAt.Time,
					})
				}
			}
		}
	}

	var simEnv *gen.SimEnv
	if s.simEnv != nil {
		env := simEnvToGen(s.simEnv.Get(time.Now()))
		simEnv = &env
	}

	writeJSON(w, http.StatusOK, wallSnapshot{
		Plan:       plan,
		Items:      items,
		Types:      types,
		States:     states,
		SimEnv:     simEnv,
		ServerTime: time.Now(),
	})
}
