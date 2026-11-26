package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestModesListGetAndPermissions(t *testing.T) {
	h := newTestServer(t)

	// 未登录拒绝
	if w := do(t, h, http.MethodGet, "/api/modes", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 GET /api/modes 应 401，实际 %d", w.Code)
	}

	// 普通成员可读列表与详情
	memberCookie := loginAs(t, h, "li")
	w := do(t, h, http.MethodGet, "/api/modes", "", memberCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/modes 应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var modes []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &modes); err != nil {
		t.Fatalf("解析模式列表失败: %v", err)
	}
	if len(modes) < 5 {
		t.Fatalf("seed 应至少包含 5 个内置模式，实际 %d", len(modes))
	}

	w = do(t, h, http.MethodGet, "/api/modes/00000000-0000-4000-8000-000000000502", "", memberCookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/modes/{id} 应 200，实际 %d", w.Code)
	}
	w = do(t, h, http.MethodGet, "/api/modes/"+uuid.NewString(), "", memberCookie)
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在的模式应 404，实际 %d", w.Code)
	}
}

func TestModeRunStreamsRunStepsOverWSAndCompletes(t *testing.T) {
	h := newTestServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	cookie := loginAs(t, h, "li")

	conn := dialWS(t, srv, cookie)
	snapshot := readEnvelope(t, conn)
	if snapshot.Type != "snapshot" {
		t.Fatalf("首帧应为 snapshot，实际 %q", snapshot.Type)
	}
	lastSeq := snapshot.Seq

	// 触发「离家」模式（3 action + 1 delay）
	w := do(t, h, http.MethodPost, "/api/modes/00000000-0000-4000-8000-000000000502/run",
		`{"requestId":"`+uuid.NewString()+`"}`, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST /api/modes/{id}/run 应 202，实际 %d：%s", w.Code, w.Body.String())
	}
	var started struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Steps  []struct {
			NodeID string `json:"nodeId"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatalf("解析 Run 失败: %v", err)
	}
	if started.ID == "" || len(started.Steps) != 4 {
		t.Fatalf("离家执行应返回 4 步，实际 %+v", started)
	}

	// 读取 WS 事件直到看到最后一步 l4 完成，并校验 seq 严格连续无跳号
	seenRunStep := 0
	seenState := 0
	finished := false
	for !finished {
		env := readEnvelope(t, conn)
		if env.Seq != lastSeq+1 {
			t.Fatalf("WS seq 出现跳号：期望 %d，实际 %d (type=%s)", lastSeq+1, env.Seq, env.Type)
		}
		lastSeq = env.Seq
		if env.Type == "state" {
			seenState++
			continue
		}
		if env.Type == "run_step" {
			seenRunStep++
			var p struct {
				RunID     string `json:"runId"`
				NodeID    string `json:"nodeId"`
				Status    string `json:"status"`
				RunStatus string `json:"runStatus"`
			}
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				t.Fatalf("解析 run_step 失败: %v", err)
			}
			if p.RunID == started.ID && p.NodeID == "l4" && p.Status == "ok" && p.RunStatus == "ok" {
				finished = true
			}
		}
	}
	if seenRunStep < 8 || seenState < 3 {
		t.Fatalf("期望至少 8 条 run_step 与 3 条 state 广播，实际 run_step=%d state=%d", seenRunStep, seenState)
	}

	// GET /api/runs/{id} 应返回完整已完成状态（供刷新恢复）
	w = do(t, h, http.MethodGet, "/api/runs/"+started.ID, "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/runs/{id} 应 200，实际 %d", w.Code)
	}
	var detail struct {
		Status string `json:"status"`
		Steps  []struct {
			NodeID string `json:"nodeId"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("解析 Run 详情失败: %v", err)
	}
	if detail.Status != "ok" {
		t.Fatalf("最终状态应为 ok，实际 %q", detail.Status)
	}
	for _, st := range detail.Steps {
		if st.Status != "ok" {
			t.Fatalf("步骤 %s 应为 ok，实际 %q", st.NodeID, st.Status)
		}
	}
}

func TestModeRunCancelEndpoint(t *testing.T) {
	h := newTestServer(t)
	cookie := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodPost, "/api/modes/00000000-0000-4000-8000-000000000502/run", "", cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("启动执行应 202，实际 %d", w.Code)
	}
	var started struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &started)

	time.Sleep(100 * time.Millisecond)
	w = do(t, h, http.MethodPost, "/api/runs/"+started.ID+"/cancel", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("取消执行应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var cancelled struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("取消后状态应为 cancelled，实际 %q", cancelled.Status)
	}
}

func TestModeRunGuardRejectedReturnsCancelledAndReason(t *testing.T) {
	h := newTestServer(t)
	cookie := loginAs(t, h, "zhang")

	// 显式把客厅光照（305）置为 500 lux，避免受前序仿真时间推进用例影响
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer pool.Close()
	luxID := uuid.MustParse("00000000-0000-4000-8000-000000000305")
	var oldLux []byte
	_ = pool.QueryRow(context.Background(),
		`SELECT value FROM capability_state WHERE item_id = $1 AND capability_key = 'illuminance'`, luxID).Scan(&oldLux)
	_, err = pool.Exec(context.Background(),
		`UPDATE capability_state SET value = '500'::jsonb, updated_at = now() WHERE item_id = $1 AND capability_key = 'illuminance'`, luxID)
	if err != nil {
		t.Fatalf("设置光照状态失败: %v", err)
	}
	t.Cleanup(func() {
		if len(oldLux) > 0 {
			_, _ = pool.Exec(context.Background(),
				`UPDATE capability_state SET value = $2, updated_at = now() WHERE item_id = $1 AND capability_key = 'illuminance'`, luxID, oldLux)
		}
	})

	// seed 中「夜起」(0504) 守卫要求照度 < 50；当前 500 lux 应不执行并返回 cancelled + reason
	w := do(t, h, http.MethodPost, "/api/modes/00000000-0000-4000-8000-000000000504/run", "", cookie)
	if w.Code != http.StatusAccepted {
		t.Fatalf("守卫拒绝应返回 202 + cancelled Run，实际 %d：%s", w.Code, w.Body.String())
	}
	var run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatalf("解析 Run 失败: %v", err)
	}
	if run.Status != "cancelled" || run.Reason == "" {
		t.Fatalf("期望 status=cancelled 且带 reason，实际 %+v", run)
	}

	// 刷新/回查 GET /api/runs/{id} 也能还原 reason
	w2 := do(t, h, http.MethodGet, "/api/runs/"+run.ID, "", cookie)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET /api/runs/{id} 应 200，实际 %d", w2.Code)
	}
	var fetched struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &fetched)
	if fetched.Status != "cancelled" || fetched.Reason == "" {
		t.Fatalf("回查 Run 应保留 reason，实际 %+v", fetched)
	}
}
