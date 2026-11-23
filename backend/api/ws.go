package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

// StateEnvelope 是 WS 上唯一的消息形状：v 版本、seq 单调递增、type 与 payload 决定语义。
// 契约见 docs/deliverables/04-详细设计说明书.md §3.2。
type StateEnvelope struct {
	V       int    `json:"v"`
	Seq     uint64 `json:"seq"`
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// StateView 是快照与增量共用的状态形状。itemId 一律用 UUID 字符串：
// 快照里给字节数组、增量里给字符串，前端按 `itemId:capabilityKey` 建键就会对不上。
type StateView struct {
	ItemID        string    `json:"itemId"`
	CapabilityKey string    `json:"capabilityKey"`
	Value         any       `json:"value"`
	Source        string    `json:"source"`
	TS            time.Time `json:"ts"`
}

type snapshotPayload struct {
	States []StateView `json:"states"`
}

// client 包一层写锁：ByteStream 的 WebSocket 不允许并发写，
// 而广播、快照、错误帧都可能来自不同 goroutine。
type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *client) write(seq uint64, typ string, payload any) error {
	data, err := json.Marshal(StateEnvelope{V: 1, Seq: seq, Type: typ, Payload: payload})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// 单个慢连接不能拖住命令接口：每次写都有自己的截止时间。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

type Hub struct {
	queries *storegen.Queries
	conns   sync.Map // *client -> struct{}
	seq     atomic.Uint64

	// broadcastMu 保证「取号 → 写连接」是一个整体：并发的两次落库如果交错，
	// 客户端会先收到 seq 更大的那条，把小的当成过期消息。
	broadcastMu sync.Mutex
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
	c := &client{conn: conn}

	// 先取 seq 再读快照：读的过程中落了库的变化，seq 一定更大、会在广播里补发；
	// 已经含进快照的变化再收到一次也无害（同值覆盖）。反过来先读后取号会漏消息。
	seq := h.seq.Load()
	states, err := h.queries.ListAllStates(r.Context())
	if err != nil {
		_ = c.write(seq, "error", map[string]string{"code": "INTERNAL", "message": "读取快照失败"})
		return
	}
	views := make([]StateView, 0, len(states))
	for _, state := range states {
		var value any
		_ = json.Unmarshal(state.Value, &value)
		views = append(views, StateView{
			ItemID:        uuid.UUID(state.ItemID.Bytes).String(),
			CapabilityKey: state.CapabilityKey,
			Value:         value,
			Source:        state.Source,
			TS:            state.UpdatedAt.Time,
		})
	}
	if err := c.write(seq, "snapshot", snapshotPayload{States: views}); err != nil {
		return
	}

	h.conns.Store(c, struct{}{})
	defer h.conns.Delete(c)
	for {
		// 只读不处理：这条通道是单向广播，读循环负责发现对端已经断开。
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}

// Broadcast 把一次落库的结果推给所有连接。seq 在这里取号，保证与写出的顺序一致。
func (h *Hub) Broadcast(message control.StateMessage) {
	view := StateView{
		ItemID:        message.ItemID.String(),
		CapabilityKey: message.CapabilityKey,
		Value:         message.Value,
		Source:        message.Source,
		TS:            message.UpdatedAt,
	}
	h.broadcastMu.Lock()
	defer h.broadcastMu.Unlock()
	seq := h.seq.Add(1)
	h.conns.Range(func(key, _ any) bool {
		c := key.(*client)
		if err := c.write(seq, "state", view); err != nil {
			h.conns.Delete(c)
			_ = c.conn.Close(websocket.StatusNormalClosure, "")
		}
		return true
	})
}
