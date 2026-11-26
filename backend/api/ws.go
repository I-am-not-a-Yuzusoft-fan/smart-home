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

// client 包一层写锁与初始化缓冲：ByteStream 的 WebSocket 不允许并发写，
// 且连接在读库发送 snapshot 期间收到的增量必须先缓存在 pending 里，
// 等 snapshot 写出后再按序补发，避免漏消息或误触发 seq 跳号。
type client struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	ready   bool
	pending []StateEnvelope
}

func (c *client) writeLocked(env StateEnvelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	// 单个慢连接不能拖住命令接口：每次写都有自己的截止时间。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *client) send(env StateEnvelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready {
		c.pending = append(c.pending, env)
		return nil
	}
	return c.writeLocked(env)
}

func (c *client) sendSnapshotAndFlush(seq uint64, views []StateView) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.writeLocked(StateEnvelope{V: 1, Seq: seq, Type: "snapshot", Payload: snapshotPayload{States: views}}); err != nil {
		return err
	}
	for _, env := range c.pending {
		if env.Seq <= seq {
			continue
		}
		if err := c.writeLocked(env); err != nil {
			return err
		}
	}
	c.pending = nil
	c.ready = true
	return nil
}

func (c *client) sendError(seq uint64, code, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.writeLocked(StateEnvelope{
		V:       1,
		Seq:     seq,
		Type:    "error",
		Payload: map[string]string{"code": code, "message": message},
	})
}

type Hub struct {
	queries *storegen.Queries
	conns   sync.Map // *client -> struct{}
	seq     atomic.Uint64

	// broadcastMu 保证「取号 → 写连接/入缓冲」与「新连接取 seq 并注册」有序，
	// 避免并发落库交错乱序，或新连接在读快照期间漏掉广播导致假跳号。
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

	// 在 broadcastMu 下先取 seq 并注册连接（此时 c.ready=false，新广播会进入 c.pending），
	// 再读库拼快照：这样读库期间产生的 seq > snapshot.seq 增量绝不会丢失。
	h.broadcastMu.Lock()
	seq := h.seq.Load()
	h.conns.Store(c, struct{}{})
	h.broadcastMu.Unlock()
	defer h.conns.Delete(c)

	states, err := h.queries.ListAllStates(r.Context())
	if err != nil {
		c.sendError(seq, "INTERNAL", "读取快照失败")
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
	if err := c.sendSnapshotAndFlush(seq, views); err != nil {
		return
	}

	for {
		// 只读不处理：这条通道是单向广播，读循环负责发现对端已经断开。
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
	}
}

// RunStepView 是 WS 上 type="run_step" 的负载形状，驱动前端执行时间线实时刷新。
type RunStepView struct {
	RunID      string     `json:"runId"`
	NodeID     string     `json:"nodeId"`
	Path       string     `json:"path"`
	Alias      string     `json:"alias,omitempty"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	RunStatus  string     `json:"runStatus,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// BroadcastRunStep 把场景步骤状态变化推给所有连接，与 state 共用同一单调 seq。
func (h *Hub) BroadcastRunStep(message control.RunStepMessage) {
	view := RunStepView{
		RunID:      message.RunID.String(),
		NodeID:     message.NodeID,
		Path:       message.Path,
		Alias:      message.Alias,
		Status:     message.Status,
		Error:      message.Error,
		RunStatus:  message.RunStatus,
		StartedAt:  message.StartedAt,
		FinishedAt: message.FinishedAt,
	}
	h.broadcastMu.Lock()
	defer h.broadcastMu.Unlock()
	seq := h.seq.Add(1)
	env := StateEnvelope{V: 1, Seq: seq, Type: "run_step", Payload: view}
	h.conns.Range(func(key, _ any) bool {
		c := key.(*client)
		if err := c.send(env); err != nil {
			h.conns.Delete(c)
			_ = c.conn.Close(websocket.StatusNormalClosure, "")
		}
		return true
	})
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
	env := StateEnvelope{V: 1, Seq: seq, Type: "state", Payload: view}
	h.conns.Range(func(key, _ any) bool {
		c := key.(*client)
		if err := c.send(env); err != nil {
			h.conns.Delete(c)
			_ = c.conn.Close(websocket.StatusNormalClosure, "")
		}
		return true
	})
}
