package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

type StateEnvelope struct {
	V       int    `json:"v"`
	Seq     uint64 `json:"seq"`
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Hub struct {
	queries *storegen.Queries
	conns   sync.Map
	seq     atomic.Uint64
}

func NewHub(queries *storegen.Queries) *Hub {
	return &Hub{queries: queries}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	states, err := h.queries.ListAllStates(r.Context())
	if err != nil {
		h.writeEnvelope(r.Context(), conn, "error", map[string]string{"code": "INTERNAL", "message": "读取快照失败"})
		return
	}
	payload := make([]map[string]any, 0, len(states))
	for _, state := range states {
		var value any
		_ = json.Unmarshal(state.Value, &value)
		payload = append(payload, map[string]any{
			"itemId": state.ItemID.Bytes, "capabilityKey": state.CapabilityKey,
			"value": value, "source": state.Source, "updatedAt": state.UpdatedAt.Time,
		})
	}
	h.seq.Add(1)
	if err := h.writeEnvelope(r.Context(), conn, "snapshot", payload); err != nil {
		return
	}

	h.conns.Store(conn, struct{}{})
	defer h.conns.Delete(conn)
	for {
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}

func (h *Hub) writeEnvelope(ctx context.Context, conn *websocket.Conn, typ string, payload any) error {
	data, _ := json.Marshal(StateEnvelope{V: 1, Seq: h.seq.Load(), Type: typ, Payload: payload})
	return conn.Write(ctx, websocket.MessageText, data)
}

func (h *Hub) Broadcast(message control.StateMessage) {
	h.seq.Add(1)
	payload := map[string]any{
		"itemId": message.ItemID.String(), "capabilityKey": message.CapabilityKey,
		"value": message.Value, "source": message.Source, "updatedAt": message.UpdatedAt,
	}
	data, _ := json.Marshal(StateEnvelope{V: 1, Seq: h.seq.Load(), Type: "state", Payload: payload})
	h.conns.Range(func(key, _ any) bool {
		_ = key.(*websocket.Conn).Write(context.Background(), websocket.MessageText, data)
		return true
	})
}
