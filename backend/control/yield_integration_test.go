package control_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/device"
)

func TestManualYieldWindowEnvJumpAndSceneClear(t *testing.T) {
	pool := openTestPool(t)
	lightID := uuid.MustParse("00000000-0000-4000-8000-000000000301")
	curtainID := uuid.MustParse("00000000-0000-4000-8000-000000000303")
	thID := uuid.MustParse("00000000-0000-4000-8000-000000000304")
	leaveModeID := uuid.MustParse("00000000-0000-4000-8000-000000000502")
	adminID := uuid.MustParse("00000000-0000-4000-8000-000000000001")

	setStateForTest(t, pool, thID, "temperature", 25.0)
	setStateForTest(t, pool, thID, "humidity", 50.0)

	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	cmdHandler := control.NewCommandHandler(pool, device.NewSimulatorAdapter(5*time.Millisecond, 0), time.Second)
	cmdHandler.Overrides().SetNowFunc(func() time.Time { return now })

	runner := control.NewModeRunner(pool, cmdHandler, nil)
	runner.SetOverrides(cmdHandler.Overrides())

	// 1. 用户手动开客厅主灯 → 记录 (lightID, "on") 的手动时刻与房间基线（25℃, 50%RH）
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "on", Value: true, Source: "manual", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("手动开灯失败: %v", err)
	}

	// 2. 10 分钟后自动策略尝试关同一盏灯的 on → 必须被抑制且返回「手动接管中」
	now = now.Add(10 * time.Minute)
	_, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "on", Value: false, Source: "automation", CreatedBy: adminID,
	})
	var ce *control.CommandError
	if !errors.As(err, &ce) || ce.Code != "CONFLICT" || !strings.Contains(ce.Message, "手动接管中") {
		t.Fatalf("窗口内自动策略应被抑制并返回 CONFLICT + 手动接管中，实际 %v", err)
	}

	// 3. 同一设备的其他能力（brightness）与其他设备（curtain）不受影响
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "brightness", Value: float64(60), Source: "automation", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("同设备其他能力 brightness 不应受让位抑制: %v", err)
	}
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: curtainID, CapabilityKey: "position", Value: float64(20), Source: "automation", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("其他设备窗帘不应受让位抑制: %v", err)
	}

	// 4. 环境温度突变 +3.5℃（25.0 -> 28.5）→ 立即解除让位，自动策略成功放行
	setStateForTest(t, pool, thID, "temperature", 28.5)
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "on", Value: false, Source: "automation", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("温度突变超过 ±3℃ 后让位应立即解除，实际失败: %v", err)
	}

	// 5. 再次手动开灯并推进 30 分钟 → 窗口过期解除让位
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "on", Value: true, Source: "manual", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("再次手动开灯失败: %v", err)
	}
	now = now.Add(30 * time.Minute)
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "on", Value: false, Source: "automation", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("满 30 分钟后让位应过期放行，实际失败: %v", err)
	}

	// 6. 再次手动开灯 → 5 分钟内触发「离家」场景 → 场景启动先清让位再下发，成功关灯！
	if _, err := cmdHandler.Handle(context.Background(), control.Command{
		RequestID: uuid.New(), ItemID: lightID, CapabilityKey: "on", Value: true, Source: "manual", CreatedBy: adminID,
	}); err != nil {
		t.Fatalf("手动开灯失败: %v", err)
	}
	now = now.Add(5 * time.Minute)
	detail, err := runner.Start(context.Background(), leaveModeID, adminID, "manual")
	if err != nil {
		t.Fatalf("触发离家场景失败: %v", err)
	}
	final := waitRunDone(t, runner, uuid.UUID(detail.Run.ID.Bytes), 5*time.Second)
	if final.Run.Status != control.RunStatusOK {
		t.Fatalf("场景启动应先清让位并全部 ok，实际 %q", final.Run.Status)
	}
	if _, stillExists := cmdHandler.Overrides().Get(lightID, "on"); stillExists {
		t.Fatal("场景执行后不应残留 (lightID, on) 的手动让位标记")
	}
}
