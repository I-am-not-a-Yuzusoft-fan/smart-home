package control_test

import (
	"context"
	"os"
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
