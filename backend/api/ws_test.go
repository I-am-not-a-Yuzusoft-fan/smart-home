package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type wsEnvelope struct {
	V       int             `json:"v"`
	Seq     uint64          `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type wsState struct {
	ItemID        string          `json:"itemId"`
	CapabilityKey string          `json:"capabilityKey"`
	Value         json.RawMessage `json:"value"`
	Source        string          `json:"source"`
	TS            time.Time       `json:"ts"`
}

type wsSnapshot struct {
	States []wsState `json:"states"`
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/state"
}

func dialWS(t *testing.T, srv *httptest.Server, cookie *http.Cookie) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header := http.Header{}
	if cookie != nil {
		header.Set("Cookie", cookie.String())
	}
	conn, _, err := websocket.Dial(ctx, wsURL(srv), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("连接 /ws/state 失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func readEnvelope(t *testing.T, conn *websocket.Conn) wsEnvelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("读取 WS 消息失败: %v", err)
	}
	var envelope wsEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("WS 消息不是合法 JSON: %v (%s)", err, data)
	}
	return envelope
}

// /ws/state 不认没有会话的握手：这条断言同时证明中间件套在升级之前跑。
func TestWebSocketRequiresSession(t *testing.T) {
	h := newTestServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv), nil)
	if err == nil {
		t.Fatal("未登录的握手应当失败")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("未登录握手应 401，实际 %+v", resp)
	}
}

// 连上先收全量快照，之后每次落库只收一条增量；快照与增量必须是同一种状态形状。
func TestWebSocketSnapshotThenIncrement(t *testing.T) {
	h := newTestServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	cookie := loginAs(t, h, "zhang")

	conn := dialWS(t, srv, cookie)
	snapshot := readEnvelope(t, conn)
	if snapshot.Type != "snapshot" {
		t.Fatalf("第一条消息应是 snapshot，实际 %q", snapshot.Type)
	}
	var payload wsSnapshot
	if err := json.Unmarshal(snapshot.Payload, &payload); err != nil {
		t.Fatalf("快照 payload 应是 {states: []}：%v", err)
	}
	if len(payload.States) == 0 {
		t.Fatal("快照里没有任何状态，seed 数据可能没灌")
	}
	for _, state := range payload.States {
		if _, err := uuid.Parse(state.ItemID); err != nil {
			t.Fatalf("快照的 itemId 必须是 UUID 字符串，实际 %q", state.ItemID)
		}
		if state.TS.IsZero() {
			t.Fatalf("快照状态缺少 ts：%+v", state)
		}
	}

	// 下发一条命令，广播的 seq 必须接着快照往下走。
	w := do(t, h, http.MethodPost, "/api/items/00000000-0000-4000-8000-000000000301/commands",
		`{"capabilityKey":"on","value":false,"requestId":"`+uuid.NewString()+`"}`, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("下发命令应 202，实际 %d：%s", w.Code, w.Body.String())
	}

	increment := readEnvelope(t, conn)
	if increment.Type != "state" {
		t.Fatalf("增量消息类型应是 state，实际 %q", increment.Type)
	}
	if increment.Seq != snapshot.Seq+1 {
		t.Fatalf("增量 seq 应接在快照后面（%d+1），实际 %d", snapshot.Seq, increment.Seq)
	}
	var state wsState
	if err := json.Unmarshal(increment.Payload, &state); err != nil {
		t.Fatalf("增量 payload 解析失败: %v", err)
	}
	if _, err := uuid.Parse(state.ItemID); err != nil {
		t.Fatalf("增量的 itemId 必须与快照同为 UUID 字符串，实际 %q", state.ItemID)
	}
	if state.CapabilityKey != "on" {
		t.Fatalf("增量应是对 on 的更新，实际 %q", state.CapabilityKey)
	}
}

// 第二个标签页连上来不能占掉一个 seq：否则第一个标签页的下一条广播就变成"跳号"，
// 两个标签页会互相把对方踢下线。
func TestWebSocketSnapshotDoesNotConsumeSeq(t *testing.T) {
	h := newTestServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	cookie := loginAs(t, h, "zhang")

	first := dialWS(t, srv, cookie)
	firstSnapshot := readEnvelope(t, first)

	second := dialWS(t, srv, cookie)
	secondSnapshot := readEnvelope(t, second)
	if secondSnapshot.Seq != firstSnapshot.Seq {
		t.Fatalf("新连接不应改变 seq：首个快照 %d，第二个快照 %d", firstSnapshot.Seq, secondSnapshot.Seq)
	}

	w := do(t, h, http.MethodPost, "/api/items/00000000-0000-4000-8000-000000000301/commands",
		`{"capabilityKey":"on","value":true,"requestId":"`+uuid.NewString()+`"}`, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("下发命令应 202，实际 %d：%s", w.Code, w.Body.String())
	}

	for name, conn := range map[string]*websocket.Conn{"第一个": first, "第二个": second} {
		increment := readEnvelope(t, conn)
		if increment.Seq != secondSnapshot.Seq+1 {
			t.Fatalf("%s连接收到的 seq 应为 %d，实际 %d", name, secondSnapshot.Seq+1, increment.Seq)
		}
	}
}
