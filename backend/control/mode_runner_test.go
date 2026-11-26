package control_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/device"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

type spyRunStepBroadcaster struct {
	mu     sync.Mutex
	events []control.RunStepMessage
}

func (s *spyRunStepBroadcaster) BroadcastRunStep(msg control.RunStepMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, msg)
}

func (s *spyRunStepBroadcaster) snapshot() []control.RunStepMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]control.RunStepMessage, len(s.events))
	copy(out, s.events)
	return out
}

type countingCommandHandler struct {
	inner control.CommandHandler
	mu    sync.Mutex
	calls []control.Command
}

func (c *countingCommandHandler) Handle(ctx context.Context, cmd control.Command) (control.CommandResult, error) {
	c.mu.Lock()
	c.calls = append(c.calls, cmd)
	c.mu.Unlock()
	return c.inner.Handle(ctx, cmd)
}

func (c *countingCommandHandler) ListLogs(ctx context.Context, limit int32) ([]storegen.ListCommandLogsRow, error) {
	return c.inner.ListLogs(ctx, limit)
}

func (c *countingCommandHandler) ListStates(ctx context.Context, itemID uuid.UUID) ([]storegen.CapabilityState, error) {
	return c.inner.ListStates(ctx, itemID)
}

func (c *countingCommandHandler) ListHistory(ctx context.Context, itemID uuid.UUID, capabilityKey *string, limit int32) ([]storegen.StateHistory, error) {
	return c.inner.ListHistory(ctx, itemID, capabilityKey, limit)
}

func (c *countingCommandHandler) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI 上必须设置 TEST_DATABASE_URL")
		}
		t.Skip("未设置 TEST_DATABASE_URL，跳过需要数据库的测试")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestModeRunnerActionDelayAndIdempotentAdvance(t *testing.T) {
	pool := openTestPool(t)
	cmdInner := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	cmdSpy := &countingCommandHandler{inner: cmdInner}
	broadcaster := &spyRunStepBroadcaster{}

	runner := control.NewModeRunner(pool, cmdSpy, broadcaster)
	// seed 中的「离家」模式：3 action + 1 delay（1000ms）
	leaveModeID := uuid.MustParse("00000000-0000-4000-8000-000000000502")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")

	detail, err := runner.Start(context.Background(), leaveModeID, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if len(detail.Steps) != 4 {
		t.Fatalf("离家模式应预建 4 步，实际 %d", len(detail.Steps))
	}

	// 等待异步执行完成
	deadline := time.Now().Add(5 * time.Second)
	var final control.RunDetail
	for time.Now().Before(deadline) {
		final, err = runner.GetRun(context.Background(), uuid.UUID(detail.Run.ID.Bytes))
		if err != nil {
			t.Fatalf("GetRun 失败: %v", err)
		}
		if final.Run.Status != "running" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if final.Run.Status != "ok" {
		t.Fatalf("执行最终状态应为 ok，实际 %q", final.Run.Status)
	}
	for _, st := range final.Steps {
		if st.Status != "ok" {
			t.Fatalf("步骤 %s 应为 ok，实际 %q (err=%v)", st.NodeID, st.Status, st.Error)
		}
	}
	if cmdSpy.count() != 3 {
		t.Fatalf("离家模式应下发 3 条设备命令，实际 %d", cmdSpy.count())
	}
	if len(broadcaster.snapshot()) < 8 {
		t.Fatalf("4 步至少应广播 8 条 running/ok 事件，实际 %d", len(broadcaster.snapshot()))
	}

	// 幂等性：对同一 runID 重复调用 Advance 不得重复下发命令
	if err := runner.Advance(context.Background(), uuid.UUID(detail.Run.ID.Bytes)); err != nil {
		t.Fatalf("重复 Advance 失败: %v", err)
	}
	if cmdSpy.count() != 3 {
		t.Fatalf("重复 Advance 不应再次下发命令，期望 3，实际 %d", cmdSpy.count())
	}
}

func TestModeRunnerCancelDuringDelay(t *testing.T) {
	pool := openTestPool(t)
	cmdInner := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	runner := control.NewModeRunner(pool, cmdInner, nil)

	leaveModeID := uuid.MustParse("00000000-0000-4000-8000-000000000502")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")

	detail, err := runner.Start(context.Background(), leaveModeID, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	runID := uuid.UUID(detail.Run.ID.Bytes)

	// 等前两步执行完进入 1s delay 后立即取消
	time.Sleep(100 * time.Millisecond)
	cancelled, err := runner.Cancel(context.Background(), runID)
	if err != nil {
		t.Fatalf("Cancel 失败: %v", err)
	}
	if cancelled.Run.Status != "cancelled" {
		t.Fatalf("取消后状态应为 cancelled，实际 %q", cancelled.Run.Status)
	}
	var skipped int
	for _, st := range cancelled.Steps {
		if st.Status == "skipped" {
			skipped++
		}
	}
	if skipped == 0 {
		t.Fatalf("取消后剩余步骤应至少有一条 skipped，实际步骤: %+v", cancelled.Steps)
	}
}

// 直接构造一条 running 且首步已 ok 的执行记录，验证 Advance 按 nodeId 判重不会重复下发已完成节点。
func TestModeRunnerAdvanceSkipsAlreadyCompletedNode(t *testing.T) {
	pool := openTestPool(t)
	cmdInner := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	cmdSpy := &countingCommandHandler{inner: cmdInner}
	runner := control.NewModeRunner(pool, cmdSpy, nil)

	q := storegen.New(pool)
	leaveModeID := uuid.MustParse("00000000-0000-4000-8000-000000000502")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	mode, err := runner.GetMode(context.Background(), leaveModeID)
	if err != nil {
		t.Fatalf("GetMode 失败: %v", err)
	}

	runID := uuid.New()
	now := time.Now()
	_, err = q.InsertRun(context.Background(), storegen.InsertRunParams{
		ID:                 pgtype.UUID{Bytes: runID, Valid: true},
		ModeID:             pgtype.UUID{Bytes: leaveModeID, Valid: true},
		Status:             control.RunStatusRunning,
		DefinitionSnapshot: mode.Definition,
		TriggeredBy:        pgtype.UUID{Bytes: adminID, Valid: true},
		Source:             "manual",
		StartedAt:          pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("InsertRun 失败: %v", err)
	}
	// 预先将第 1 步 l1 标记为 ok
	_, err = q.UpsertRunStep(context.Background(), storegen.UpsertRunStepParams{
		RunID:      pgtype.UUID{Bytes: runID, Valid: true},
		NodeID:     "l1",
		Path:       "000",
		Alias:      pgtype.Text{String: "关客厅主灯", Valid: true},
		Status:     control.StepStatusOK,
		StartedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		FinishedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertRunStep 失败: %v", err)
	}

	if err := runner.Advance(context.Background(), runID); err != nil {
		t.Fatalf("Advance 失败: %v", err)
	}
	// 离家共 3 个 action（l1, l2, l4），l1 已完成故只应下发 2 条命令
	if cmdSpy.count() != 2 {
		t.Fatalf("已完成节点 l1 应被跳过，仅下发剩余 2 条命令，实际 %d", cmdSpy.count())
	}
	final, err := runner.GetRun(context.Background(), runID)
	if err != nil || final.Run.Status != control.RunStatusOK {
		t.Fatalf("Advance 完成后状态应为 ok，实际 %+v (err=%v)", final.Run.Status, err)
	}
}

func insertTempMode(t *testing.T, pool *pgxpool.Pool, runMode string, def control.ModeDefinition) uuid.UUID {
	t.Helper()
	id := uuid.New()
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal def: %v", err)
	}
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	_, err = pool.Exec(context.Background(),
		`INSERT INTO mode (id, name, definition, run_mode, created_by) VALUES ($1, $2, $3, $4, $5)`,
		id, "test-mode-"+id.String()[:8], raw, runMode, adminID)
	if err != nil {
		t.Fatalf("insert temp mode: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mode WHERE id = $1`, id)
	})
	return id
}

func waitRunDone(t *testing.T, runner *control.ModeRunner, runID uuid.UUID, maxWait time.Duration) control.RunDetail {
	t.Helper()
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		d, err := runner.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun 失败: %v", err)
		}
		if d.Run.Status != control.RunStatusRunning {
			return d
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待执行 %s 完成超时", runID)
	return control.RunDetail{}
}

type failingItemAdapter struct {
	failItem uuid.UUID
	inner    device.Adapter
}

func (f *failingItemAdapter) Dispatch(ctx context.Context, cmd device.Command) (any, error) {
	if cmd.ItemID == f.failItem {
		return nil, device.ErrUnavailable
	}
	return f.inner.Dispatch(ctx, cmd)
}

func setStateForTest(t *testing.T, pool *pgxpool.Pool, itemID uuid.UUID, key string, val any) {
	t.Helper()
	raw, err := json.Marshal(val)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	var oldRaw []byte
	hadOld := false
	err = pool.QueryRow(context.Background(),
		`SELECT value FROM capability_state WHERE item_id = $1 AND capability_key = $2`, itemID, key).Scan(&oldRaw)
	if err == nil {
		hadOld = true
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO capability_state (item_id, capability_key, value, source, updated_at)
		 VALUES ($1, $2, $3, 'device', now())
		 ON CONFLICT (item_id, capability_key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		itemID, key, raw)
	if err != nil {
		t.Fatalf("upsert state: %v", err)
	}
	t.Cleanup(func() {
		if hadOld {
			_, _ = pool.Exec(context.Background(),
				`UPDATE capability_state SET value = $3, updated_at = now() WHERE item_id = $1 AND capability_key = $2`,
				itemID, key, oldRaw)
		} else {
			_, _ = pool.Exec(context.Background(),
				`DELETE FROM capability_state WHERE item_id = $1 AND capability_key = $2`, itemID, key)
		}
	})
}

func TestModeRunnerTopLevelGuardRejectedAndMissingReading(t *testing.T) {
	pool := openTestPool(t)
	cmdInner := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	cmdSpy := &countingCommandHandler{inner: cmdInner}
	broadcaster := &spyRunStepBroadcaster{}
	runner := control.NewModeRunner(pool, cmdSpy, broadcaster)

	pirID := uuid.MustParse("00000000-0000-4000-8000-000000000306")
	lightID := uuid.MustParse("00000000-0000-4000-8000-000000000301")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	setStateForTest(t, pool, pirID, "occupied", true)

	// seed 中 306 的 occupied 为 true，构造一个要求 occupied == false 的顶层守卫（必然不满足）
	modeID := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Guard: &control.ModeGuard{
			All: []control.ModeCondition{{ItemID: pirID, Capability: "occupied", Op: "==", Value: false}},
		},
		Steps: []control.ModeNode{
			{NodeID: "a1", Type: "action", Alias: "开灯", ItemID: &lightID, Capability: "on", Value: true},
		},
	})

	detail, err := runner.Start(context.Background(), modeID, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 不应报错: %v", err)
	}
	if detail.Run.Status != control.RunStatusCancelled {
		t.Fatalf("守卫不满足时状态应为 cancelled，实际 %q", detail.Run.Status)
	}
	if detail.Reason == "" {
		t.Fatal("守卫不满足时必须返回具体原因")
	}
	if cmdSpy.count() != 0 {
		t.Fatalf("守卫不满足时不得执行任何动作，实际下发 %d 条", cmdSpy.count())
	}

	// 依赖的传感器读数缺失时按不成立处理并说明原因
	missingID := uuid.New()
	modeMissing := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Guard: &control.ModeGuard{
			All: []control.ModeCondition{{ItemID: missingID, Capability: "temperature", Op: ">", Value: 20}},
		},
		Steps: []control.ModeNode{
			{NodeID: "a1", Type: "action", Alias: "开灯", ItemID: &lightID, Capability: "on", Value: true},
		},
	})
	detailMissing, err := runner.Start(context.Background(), modeMissing, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if detailMissing.Run.Status != control.RunStatusCancelled || !strings.Contains(detailMissing.Reason, "缺失") {
		t.Fatalf("读数缺失应 cancelled 且原因含「缺失」，实际 status=%q reason=%q", detailMissing.Run.Status, detailMissing.Reason)
	}

	// guard.Any：任意一条满足即放行，全部不满足则拒绝
	modeAnyPass := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Guard: &control.ModeGuard{
			Any: []control.ModeCondition{
				{ItemID: pirID, Capability: "occupied", Op: "==", Value: false},
				{ItemID: pirID, Capability: "occupied", Op: "==", Value: true},
			},
		},
		Steps: []control.ModeNode{
			{NodeID: "a1", Type: "action", Alias: "开灯", ItemID: &lightID, Capability: "on", Value: true},
		},
	})
	dPass, err := runner.Start(context.Background(), modeAnyPass, adminID, "manual")
	if err != nil {
		t.Fatalf("guard.Any 满足时 Start 失败: %v", err)
	}
	fPass := waitRunDone(t, runner, uuid.UUID(dPass.Run.ID.Bytes), 5*time.Second)
	if fPass.Run.Status != control.RunStatusOK {
		t.Fatalf("guard.Any 满足应 ok，实际 %q", fPass.Run.Status)
	}

	modeAnyFail := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Guard: &control.ModeGuard{
			Any: []control.ModeCondition{
				{ItemID: pirID, Capability: "occupied", Op: "==", Value: false},
				{ItemID: missingID, Capability: "temperature", Op: ">", Value: 20},
			},
		},
		Steps: []control.ModeNode{
			{NodeID: "a1", Type: "action", Alias: "开灯", ItemID: &lightID, Capability: "on", Value: true},
		},
	})
	dFail, err := runner.Start(context.Background(), modeAnyFail, adminID, "manual")
	if err != nil {
		t.Fatalf("guard.Any 拒绝时 Start 失败: %v", err)
	}
	if dFail.Run.Status != control.RunStatusCancelled || dFail.Reason == "" {
		t.Fatalf("guard.Any 全不满足应 cancelled 且带原因，实际 status=%q reason=%q", dFail.Run.Status, dFail.Reason)
	}
}

func TestModeRunnerChooseNestedBranches(t *testing.T) {
	pool := openTestPool(t)
	cmdInner := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	cmdSpy := &countingCommandHandler{inner: cmdInner}
	runner := control.NewModeRunner(pool, cmdSpy, nil)

	pirID := uuid.MustParse("00000000-0000-4000-8000-000000000306")
	luxID := uuid.MustParse("00000000-0000-4000-8000-000000000305")
	lightID := uuid.MustParse("00000000-0000-4000-8000-000000000301")
	curtainID := uuid.MustParse("00000000-0000-4000-8000-000000000303")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	setStateForTest(t, pool, pirID, "occupied", true)
	setStateForTest(t, pool, luxID, "illuminance", 500)

	// 外层 occupied==true 命中 then，内层 illuminance<10 不成立命中 else（只执行 else 的关窗帘，不执行未命中的分支）
	modeID := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Steps: []control.ModeNode{
			{
				NodeID: "c1", Type: "choose", Alias: "是否有人",
				When: &control.ModeGuard{All: []control.ModeCondition{{ItemID: pirID, Capability: "occupied", Op: "==", Value: true}}},
				Then: []control.ModeNode{
					{
						NodeID: "c2", Type: "choose", Alias: "照度检查",
						When: &control.ModeGuard{All: []control.ModeCondition{{ItemID: luxID, Capability: "illuminance", Op: "<", Value: 10}}},
						Then: []control.ModeNode{
							{NodeID: "t1", Type: "action", Alias: "不应执行", ItemID: &lightID, Capability: "on", Value: true},
						},
						Else: []control.ModeNode{
							{NodeID: "e1", Type: "action", Alias: "关窗帘", ItemID: &curtainID, Capability: "position", Value: 0},
						},
					},
				},
				Else: []control.ModeNode{
					{NodeID: "e0", Type: "action", Alias: "外层未命中", ItemID: &lightID, Capability: "on", Value: false},
				},
			},
		},
	})

	detail, err := runner.Start(context.Background(), modeID, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	final := waitRunDone(t, runner, uuid.UUID(detail.Run.ID.Bytes), 5*time.Second)
	if final.Run.Status != control.RunStatusOK {
		t.Fatalf("嵌套 choose 应 ok，实际 %q", final.Run.Status)
	}
	if cmdSpy.count() != 1 {
		t.Fatalf("应只执行命中的 1 条动作（e1），实际下发 %d 条", cmdSpy.count())
	}
	if cmdSpy.calls[0].ItemID != curtainID {
		t.Fatalf("命中的动作应为窗帘，实际 %s", cmdSpy.calls[0].ItemID)
	}
}

func TestModeRunnerOfflineContinueAbortAndDeletedItem(t *testing.T) {
	pool := openTestPool(t)
	lightID := uuid.MustParse("00000000-0000-4000-8000-000000000301")
	curtainID := uuid.MustParse("00000000-0000-4000-8000-000000000303")
	deletedID := uuid.New()
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")

	adapter := &failingItemAdapter{failItem: lightID, inner: device.NewSimulatorAdapter(5*time.Millisecond, 0)}
	cmdHandler := control.NewCommandHandler(pool, adapter, time.Second)
	runner := control.NewModeRunner(pool, cmdHandler, nil)

	// 1. continueOnError=false：失败即中止，后续步骤 skipped
	falseVal := false
	modeAbort := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Steps: []control.ModeNode{
			{NodeID: "s1", Type: "action", Alias: "离线灯", ItemID: &lightID, Capability: "on", Value: true, ContinueOnError: &falseVal},
			{NodeID: "s2", Type: "action", Alias: "后续窗帘", ItemID: &curtainID, Capability: "position", Value: 0},
		},
	})
	d1, err := runner.Start(context.Background(), modeAbort, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	f1 := waitRunDone(t, runner, uuid.UUID(d1.Run.ID.Bytes), 5*time.Second)
	if f1.Run.Status != control.RunStatusFailed {
		t.Fatalf("离线中止应 failed，实际 %q", f1.Run.Status)
	}
	if f1.Steps[0].Status != control.StepStatusFailed || f1.Steps[1].Status != control.StepStatusSkipped {
		t.Fatalf("步骤状态应为 failed + skipped，实际 %q + %q", f1.Steps[0].Status, f1.Steps[1].Status)
	}

	// 2. 模式默认 defaults.continueOnError=true（节点本身不显式覆盖）+ 被删设备：离线记 failed 但继续，被删设备记 skipped 并继续
	modeContinue := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version:  1,
		Defaults: &control.ModeDefaults{ContinueOnError: true},
		Steps: []control.ModeNode{
			{NodeID: "s1", Type: "action", Alias: "离线灯", ItemID: &lightID, Capability: "on", Value: true},
			{NodeID: "s2", Type: "action", Alias: "已删设备", ItemID: &deletedID, Capability: "on", Value: true},
			{NodeID: "s3", Type: "action", Alias: "正常窗帘", ItemID: &curtainID, Capability: "position", Value: 50},
		},
	})
	d2, err := runner.Start(context.Background(), modeContinue, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	f2 := waitRunDone(t, runner, uuid.UUID(d2.Run.ID.Bytes), 5*time.Second)
	if f2.Run.Status != control.RunStatusOK {
		t.Fatalf("continueOnError=true 跑完应 ok，实际 %q", f2.Run.Status)
	}
	if f2.Steps[0].Status != control.StepStatusFailed || f2.Steps[1].Status != control.StepStatusSkipped || f2.Steps[2].Status != control.StepStatusOK {
		t.Fatalf("步骤状态应为 failed/skipped/ok，实际 %q/%q/%q", f2.Steps[0].Status, f2.Steps[1].Status, f2.Steps[2].Status)
	}
}

func TestModeRunnerTimeoutTerminatesRemainingSteps(t *testing.T) {
	pool := openTestPool(t)
	cmdHandler := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	runner := control.NewModeRunner(pool, cmdHandler, nil)
	runner.SetMaxRunDuration(120 * time.Millisecond)

	lightID := uuid.MustParse("00000000-0000-4000-8000-000000000301")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")

	modeID := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Steps: []control.ModeNode{
			{NodeID: "d1", Type: "delay", Alias: "长延时", Ms: 2000},
			{NodeID: "a1", Type: "action", Alias: "超时后不该执行", ItemID: &lightID, Capability: "on", Value: true},
		},
	})

	detail, err := runner.Start(context.Background(), modeID, adminID, "manual")
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	final := waitRunDone(t, runner, uuid.UUID(detail.Run.ID.Bytes), 5*time.Second)
	if final.Run.Status != control.RunStatusFailed {
		t.Fatalf("超时后 run.status 应为 failed，实际 %q", final.Run.Status)
	}
	if final.Steps[0].Status != control.StepStatusFailed || !strings.Contains(final.Steps[0].Error.String, "timeout") {
		t.Fatalf("超时当前步应 failed 且含 timeout，实际 status=%q err=%q", final.Steps[0].Status, final.Steps[0].Error.String)
	}
	if final.Steps[1].Status != control.StepStatusSkipped {
		t.Fatalf("超时后剩余步骤应 skipped，实际 %q", final.Steps[1].Status)
	}
}

func TestModeRunnerDuplicateTriggerRestartAndSingle(t *testing.T) {
	pool := openTestPool(t)
	cmdHandler := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	runner := control.NewModeRunner(pool, cmdHandler, nil)
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")

	// 1. restart 模式：第二次触发取消旧执行并完成新执行
	restartModeID := insertTempMode(t, pool, "restart", control.ModeDefinition{
		Version: 1,
		Steps:   []control.ModeNode{{NodeID: "d1", Type: "delay", Alias: "等 300ms", Ms: 300}},
	})
	first, err := runner.Start(context.Background(), restartModeID, adminID, "manual")
	if err != nil {
		t.Fatalf("第一次 Start 失败: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	second, err := runner.Start(context.Background(), restartModeID, adminID, "manual")
	if err != nil {
		t.Fatalf("restart 第二次 Start 失败: %v", err)
	}
	f1 := waitRunDone(t, runner, uuid.UUID(first.Run.ID.Bytes), 5*time.Second)
	f2 := waitRunDone(t, runner, uuid.UUID(second.Run.ID.Bytes), 5*time.Second)
	if f1.Run.Status != control.RunStatusCancelled || f2.Run.Status != control.RunStatusOK {
		t.Fatalf("restart 应取消旧 run 并完成新 run，实际 first=%q second=%q", f1.Run.Status, f2.Run.Status)
	}

	// 2. single 模式：运行中再次触发返回 ErrModeAlreadyRunning
	singleModeID := insertTempMode(t, pool, "single", control.ModeDefinition{
		Version: 1,
		Steps:   []control.ModeNode{{NodeID: "d1", Type: "delay", Alias: "等 300ms", Ms: 300}},
	})
	s1, err := runner.Start(context.Background(), singleModeID, adminID, "manual")
	if err != nil {
		t.Fatalf("single 首次 Start 失败: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	_, err = runner.Start(context.Background(), singleModeID, adminID, "manual")
	if !errors.Is(err, control.ErrModeAlreadyRunning) {
		t.Fatalf("single 模式重复触发应返回 ErrModeAlreadyRunning，实际 %v", err)
	}
	_ = waitRunDone(t, runner, uuid.UUID(s1.Run.ID.Bytes), 5*time.Second)
}
