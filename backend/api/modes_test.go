package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestModeRunSingleConflictReturns409(t *testing.T) {
	h := newTestServer(t)
	cookie := loginAs(t, h, "zhang")

	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer pool.Close()

	modeID := uuid.New()
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	_, err = pool.Exec(context.Background(),
		`INSERT INTO mode (id, name, definition, run_mode, created_by) VALUES ($1, $2, '{"version":1,"steps":[{"nodeId":"d1","type":"delay","ms":500}]}'::jsonb, 'single', $3)`,
		modeID, "single-conflict-"+modeID.String()[:8], adminID)
	if err != nil {
		t.Fatalf("插入测试模式失败: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mode WHERE id = $1`, modeID)
	})

	w1 := do(t, h, http.MethodPost, "/api/modes/"+modeID.String()+"/run", "", cookie)
	if w1.Code != http.StatusAccepted {
		t.Fatalf("首次触发应 202，实际 %d：%s", w1.Code, w1.Body.String())
	}
	var started struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w1.Body.Bytes(), &started)

	w2 := do(t, h, http.MethodPost, "/api/modes/"+modeID.String()+"/run", "", cookie)
	if w2.Code != http.StatusConflict {
		t.Fatalf("single 模式运行中再次触发应 409，实际 %d：%s", w2.Code, w2.Body.String())
	}
	_ = do(t, h, http.MethodPost, "/api/runs/"+started.ID+"/cancel", "", cookie)
}

func TestSimEnvPermissionsAndComfortSeasonSwitch(t *testing.T) {
	h := newTestServer(t)
	adminCookie := loginAs(t, h, "zhang")
	memberCookie := loginAs(t, h, "li")

	// 成员可读 /api/sim/env，不可写（403）
	if w := do(t, h, http.MethodGet, "/api/sim/env", "", memberCookie); w.Code != http.StatusOK {
		t.Fatalf("member GET /api/sim/env 应 200，实际 %d", w.Code)
	}
	if w := do(t, h, http.MethodPut, "/api/sim/env", `{"season":"summer"}`, memberCookie); w.Code != http.StatusForbidden {
		t.Fatalf("member PUT /api/sim/env 应 403，实际 %d", w.Code)
	}

	// 确保室内温湿度（304）高温高湿、客厅有人（306=true）、空调（302）初始关闭且已过最小间隔
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer pool.Close()
	_, _ = pool.Exec(context.Background(),
		`UPDATE capability_state SET value = '32'::jsonb, updated_at = now() - interval '5 minutes' WHERE item_id = '00000000-0000-4000-8000-000000000304' AND capability_key = 'temperature'`)
	_, _ = pool.Exec(context.Background(),
		`UPDATE capability_state SET value = '75'::jsonb, updated_at = now() - interval '5 minutes' WHERE item_id = '00000000-0000-4000-8000-000000000304' AND capability_key = 'humidity'`)
	_, _ = pool.Exec(context.Background(),
		`UPDATE capability_state SET value = 'true'::jsonb, updated_at = now() WHERE item_id = '00000000-0000-4000-8000-000000000306' AND capability_key = 'occupied'`)
	_, _ = pool.Exec(context.Background(),
		`UPDATE capability_state SET value = 'false'::jsonb, updated_at = now() - interval '5 minutes' WHERE item_id = '00000000-0000-4000-8000-000000000302' AND capability_key = 'on'`)

	runHomeAndFindS5 := func() (status, note string) {
		w := do(t, h, http.MethodPost, "/api/modes/00000000-0000-4000-8000-000000000501/run", "", adminCookie)
		if w.Code != http.StatusAccepted {
			t.Fatalf("触发回家模式应 202，实际 %d：%s", w.Code, w.Body.String())
		}
		var started struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &started)
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			wr := do(t, h, http.MethodGet, "/api/runs/"+started.ID, "", adminCookie)
			var run struct {
				Status string `json:"status"`
				Steps  []struct {
					NodeID string `json:"nodeId"`
					Status string `json:"status"`
					Note   string `json:"note"`
				} `json:"steps"`
			}
			_ = json.Unmarshal(wr.Body.Bytes(), &run)
			if run.Status != "running" {
				for _, st := range run.Steps {
					if st.NodeID == "s5" {
						return st.Status, st.Note
					}
				}
				t.Fatalf("未找到 s5 步骤：%+v", run)
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("等待回家模式完成超时")
		return "", ""
	}

	// 1. 切到夏季 → s5 应 ok 且依据文案包含「夏季」与目标温度
	if w := do(t, h, http.MethodPut, "/api/sim/env", `{"season":"summer","outdoorTemp":35}`, adminCookie); w.Code != http.StatusOK {
		t.Fatalf("设置夏季失败: %d %s", w.Code, w.Body.String())
	}
	stSummer, noteSummer := runHomeAndFindS5()
	if stSummer != "ok" || !strings.Contains(noteSummer, "夏季") {
		t.Fatalf("夏季温控 s5 应 ok 且含「夏季」，实际 status=%q note=%q", stSummer, noteSummer)
	}

	// 2. 紧接着切到春季（室外 22℃）——无需手工回拨时间戳，切季节后立即生效并返回「自然通风」
	if w := do(t, h, http.MethodPut, "/api/sim/env", `{"season":"spring","outdoorTemp":22}`, adminCookie); w.Code != http.StatusOK {
		t.Fatalf("设置春季失败: %d %s", w.Code, w.Body.String())
	}
	stSpring, noteSpring := runHomeAndFindS5()
	if stSpring != "ok" || !strings.Contains(noteSpring, "自然通风") {
		t.Fatalf("春季温控 s5 应 ok 且含「自然通风」，实际 status=%q note=%q", stSpring, noteSpring)
	}
}

func TestModeCRUDValidationAndPermissions(t *testing.T) {
	h := newTestServer(t)
	adminCookie := loginAs(t, h, "zhang")
	memberCookie := loginAs(t, h, "li")

	validBody := `{
		"name": "测试自定义场景-` + uuid.NewString()[:8] + `",
		"runMode": "restart",
		"enabled": true,
		"definition": {
			"version": 1,
			"guard": {
				"all": [{"itemId":"00000000-0000-4000-8000-000000000306","capability":"occupied","op":"==","value":true}]
			},
			"defaults": {"continueOnError": false},
			"steps": [
				{"nodeId":"a1","type":"action","alias":"开客厅灯","itemId":"00000000-0000-4000-8000-000000000301","capability":"on","value":true},
				{"nodeId":"a2","type":"action","alias":"调亮度","itemId":"00000000-0000-4000-8000-000000000301","capability":"brightness","value":60},
				{"nodeId":"d1","type":"delay","alias":"等 50ms","ms":50},
				{"nodeId":"c1","type":"choose","alias":"照度判断",
				 "when":{"all":[{"itemId":"00000000-0000-4000-8000-000000000305","capability":"illuminance","op":">","value":10}]},
				 "then":[{"nodeId":"a3","type":"action","alias":"关窗帘","itemId":"00000000-0000-4000-8000-000000000303","capability":"position","value":0}],
				 "else":[]}
			]
		}
	}`

	// 1. 普通成员写操作一律 403
	if w := do(t, h, http.MethodPost, "/api/modes", validBody, memberCookie); w.Code != http.StatusForbidden {
		t.Fatalf("member POST /api/modes 应 403，实际 %d", w.Code)
	}
	if w := do(t, h, http.MethodPut, "/api/modes/00000000-0000-4000-8000-000000000501", validBody, memberCookie); w.Code != http.StatusForbidden {
		t.Fatalf("member PUT /api/modes 应 403，实际 %d", w.Code)
	}
	if w := do(t, h, http.MethodDelete, "/api/modes/00000000-0000-4000-8000-000000000501", "", memberCookie); w.Code != http.StatusForbidden {
		t.Fatalf("member DELETE /api/modes 应 403，实际 %d", w.Code)
	}

	// 2. JSON Schema 与语义校验拒绝非法定义（空 steps、重复 nodeId、不存在设备）
	invalidCases := []struct {
		name string
		body string
	}{
		{"空 steps", `{"name":"bad1","definition":{"version":1,"steps":[]}}`},
		{"重复 nodeId", `{"name":"bad2","definition":{"version":1,"steps":[{"nodeId":"x","type":"delay","ms":10},{"nodeId":"x","type":"delay","ms":10}]}}`},
		{"不存在设备", `{"name":"bad3","definition":{"version":1,"steps":[{"nodeId":"x","type":"action","itemId":"` + uuid.NewString() + `","capability":"on","value":true}]}}`},
	}
	for _, tc := range invalidCases {
		if w := do(t, h, http.MethodPost, "/api/modes", tc.body, adminCookie); w.Code != http.StatusBadRequest {
			t.Fatalf("非法用例 %q 应返回 400，实际 %d：%s", tc.name, w.Code, w.Body.String())
		}
	}

	// 3. 管理员创建 → 重名 409 → 更新 → 删除
	wCreate := do(t, h, http.MethodPost, "/api/modes", validBody, adminCookie)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("admin POST /api/modes 应 201，实际 %d：%s", wCreate.Code, wCreate.Body.String())
	}
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(wCreate.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("创建返回缺少 id")
	}

	// 同名冲突 409
	if wDup := do(t, h, http.MethodPost, "/api/modes", validBody, adminCookie); wDup.Code != http.StatusConflict {
		t.Fatalf("重名创建应 409，实际 %d", wDup.Code)
	}

	// 更新 200
	updatedBody := strings.Replace(validBody, created.Name, created.Name+"-已更新", 1)
	if wUpd := do(t, h, http.MethodPut, "/api/modes/"+created.ID, updatedBody, adminCookie); wUpd.Code != http.StatusOK {
		t.Fatalf("PUT /api/modes/{id} 应 200，实际 %d：%s", wUpd.Code, wUpd.Body.String())
	}

	// 触发执行该自定义模式（3 action + 1 delay + 1 choose + 顶层 guard），验证完整跑通 ok
	wRun := do(t, h, http.MethodPost, "/api/modes/"+created.ID+"/run", "", adminCookie)
	if wRun.Code != http.StatusAccepted {
		t.Fatalf("触发自定义模式应 202，实际 %d：%s", wRun.Code, wRun.Body.String())
	}
	var runResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(wRun.Body.Bytes(), &runResp)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		wr := do(t, h, http.MethodGet, "/api/runs/"+runResp.ID, "", adminCookie)
		var r struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(wr.Body.Bytes(), &r)
		if r.Status != "running" {
			if r.Status != "ok" {
				t.Fatalf("自定义模式执行应 ok，实际 %q", r.Status)
			}
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// 删除 204，再查 404
	if wDel := do(t, h, http.MethodDelete, "/api/modes/"+created.ID, "", adminCookie); wDel.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/modes/{id} 应 204，实际 %d", wDel.Code)
	}
	if wGone := do(t, h, http.MethodGet, "/api/modes/"+created.ID, "", adminCookie); wGone.Code != http.StatusNotFound {
		t.Fatalf("删除后 GET 应 404，实际 %d", wGone.Code)
	}
}
