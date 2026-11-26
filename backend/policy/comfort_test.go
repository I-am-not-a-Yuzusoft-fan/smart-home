package policy_test

import (
	"strings"
	"testing"
	"time"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/policy"
)

func f64(v float64) *float64 { return &v }

func TestEvaluateComfortSeasonsAndRanges(t *testing.T) {
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		input          policy.ComfortInput
		wantAction     policy.ComfortAction
		wantMinTemp    float64
		wantMaxTemp    float64
		wantReasonPart string
	}{
		{
			name: "夏季高温高湿动态算温并在24-28区间内",
			input: policy.ComfortInput{
				Season:         policy.SeasonSummer,
				IndoorTemp:     f64(32.0),
				IndoorHumidity: f64(75.0),
				CurrentOn:      false,
				Now:            now,
			},
			wantAction:     policy.ComfortActionCool,
			wantMinTemp:    24,
			wantMaxTemp:    28,
			wantReasonPart: "夏季",
		},
		{
			name: "冬季低温动态制热并在18-22区间内",
			input: policy.ComfortInput{
				Season:         policy.SeasonWinter,
				IndoorTemp:     f64(13.5),
				IndoorHumidity: f64(55.0),
				CurrentOn:      false,
				Now:            now,
			},
			wantAction:     policy.ComfortActionHeat,
			wantMinTemp:    18,
			wantMaxTemp:    22,
			wantReasonPart: "冬季",
		},
		{
			name: "春秋无室外传感器时建议自然通风不启动压缩机",
			input: policy.ComfortInput{
				Season:         policy.SeasonSpring,
				IndoorTemp:     f64(24.0),
				IndoorHumidity: f64(55.0),
				CurrentOn:      false,
				Now:            now,
			},
			wantAction:     policy.ComfortActionNatural,
			wantReasonPart: "自然通风",
		},
		{
			name: "春秋室外22℃在18-26区间内建议自然通风",
			input: policy.ComfortInput{
				Season:         policy.SeasonAutumn,
				IndoorTemp:     f64(25.5),
				IndoorHumidity: f64(50.0),
				OutdoorTemp:    f64(22.0),
				CurrentOn:      false,
				Now:            now,
			},
			wantAction:     policy.ComfortActionNatural,
			wantReasonPart: "自然通风",
		},
		{
			name: "春秋室外酷热33℃且室温29℃兜底制冷",
			input: policy.ComfortInput{
				Season:         policy.SeasonAutumn,
				IndoorTemp:     f64(29.0),
				IndoorHumidity: f64(60.0),
				OutdoorTemp:    f64(33.0),
				CurrentOn:      false,
				Now:            now,
			},
			wantAction:     policy.ComfortActionCool,
			wantMinTemp:    24,
			wantMaxTemp:    26,
			wantReasonPart: "兜底",
		},
		{
			name: "读数不可用时拒绝决策并提示等待数据",
			input: policy.ComfortInput{
				Season:         policy.SeasonSummer,
				IndoorTemp:     nil,
				IndoorHumidity: f64(60.0),
				Now:            now,
			},
			wantAction:     policy.ComfortActionWait,
			wantReasonPart: "等待",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := policy.EvaluateComfort(tc.input)
			if got.Action != tc.wantAction {
				t.Fatalf("Action = %q, want %q (reason=%q)", got.Action, tc.wantAction, got.Reason)
			}
			if tc.wantAction == policy.ComfortActionCool || tc.wantAction == policy.ComfortActionHeat {
				if got.TargetTemp == nil {
					t.Fatal("制冷/制热决策必须给出 TargetTemp")
				}
				if *got.TargetTemp < tc.wantMinTemp || *got.TargetTemp > tc.wantMaxTemp {
					t.Fatalf("TargetTemp = %.1f 超出区间 [%.1f, %.1f]", *got.TargetTemp, tc.wantMinTemp, tc.wantMaxTemp)
				}
			}
			if !strings.Contains(got.Reason, tc.wantReasonPart) {
				t.Fatalf("Reason %q 应包含 %q", got.Reason, tc.wantReasonPart)
			}
		})
	}
}

func TestEvaluateComfortHysteresisPreventsChatter(t *testing.T) {
	now := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)

	// 夏季：室温在目标温度 26℃ 附近 ±0.8℃ 抖动（26.5 -> 25.4 -> 26.8 -> 25.3），空调保持关闭不应反复启停
	jitterTemps := []float64{26.5, 25.4, 26.8, 25.3, 26.9, 25.5}
	on := false
	switches := 0
	for i, temp := range jitterTemps {
		dec := policy.EvaluateComfort(policy.ComfortInput{
			Season:         policy.SeasonSummer,
			IndoorTemp:     f64(temp),
			IndoorHumidity: f64(55.0),
			CurrentOn:      on,
			CurrentMode:    "cool",
			CurrentTarget:  f64(26.0),
			Now:            now.Add(time.Duration(i*5) * time.Minute),
		})
		if dec.ShouldTurnOn != on && (dec.Action == policy.ComfortActionCool || dec.Action == policy.ComfortActionNatural) {
			on = dec.ShouldTurnOn
			switches++
		}
	}
	if switches != 0 {
		t.Fatalf("在 1℃ 滞回带内抖动不应启停，实际切换 %d 次", switches)
	}

	// 升到 28.5℃ (> 26+1) 才开启；降到 24.5℃ (<= 26-1) 才关闭
	openDec := policy.EvaluateComfort(policy.ComfortInput{
		Season:         policy.SeasonSummer,
		IndoorTemp:     f64(28.5),
		IndoorHumidity: f64(55.0),
		CurrentOn:      false,
		Now:            now,
	})
	if openDec.Action != policy.ComfortActionCool || !openDec.ShouldTurnOn {
		t.Fatalf("高于目标+1℃应开启制冷，实际 %+v", openDec)
	}

	closeDec := policy.EvaluateComfort(policy.ComfortInput{
		Season:         policy.SeasonSummer,
		IndoorTemp:     f64(24.5),
		IndoorHumidity: f64(55.0),
		CurrentOn:      true,
		CurrentMode:    "cool",
		CurrentTarget:  f64(26.0),
		Now:            now.Add(5 * time.Minute),
	})
	if closeDec.ShouldTurnOn {
		t.Fatalf("低于等于目标-1℃应关闭压缩机，实际 %+v", closeDec)
	}
}

func TestEvaluateComfortMinSwitchInterval60s(t *testing.T) {
	now := time.Date(2026, 7, 15, 14, 0, 30, 0, time.UTC)
	last := now.Add(-30 * time.Second) // 距上次切换仅 30 秒 (< 60s)

	dec := policy.EvaluateComfort(policy.ComfortInput{
		Season:         policy.SeasonSummer,
		IndoorTemp:     f64(31.0),
		IndoorHumidity: f64(65.0),
		CurrentOn:      false,
		LastSwitchAt:   &last,
		Now:            now,
	})
	if dec.Action != policy.ComfortActionHold {
		t.Fatalf("60 秒最小切换间隔内应 Hold，实际 %q (%q)", dec.Action, dec.Reason)
	}
	if !strings.Contains(dec.Reason, "60") {
		t.Fatalf("原因应说明 60 秒间隔保护，实际 %q", dec.Reason)
	}
}

func TestResolveSeason(t *testing.T) {
	if got := policy.ResolveSeason("auto", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); got != policy.SeasonSummer {
		t.Fatalf("7 月应为 summer，实际 %q", got)
	}
	if got := policy.ResolveSeason("auto", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)); got != policy.SeasonWinter {
		t.Fatalf("1 月应为 winter，实际 %q", got)
	}
	if got := policy.ResolveSeason("winter", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); got != policy.SeasonWinter {
		t.Fatalf("显式 winter 应覆盖当前月份，实际 %q", got)
	}
}
