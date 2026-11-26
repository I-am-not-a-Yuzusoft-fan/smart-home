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

	cases := []struct {
		name        string
		season      policy.Season
		outdoorTemp *float64
		startOn     bool
		startMode   string
		startTarget float64
		temps       []float64
	}{
		{
			name:        "夏季关机态在目标26℃±0.9℃内抖动不启停",
			season:      policy.SeasonSummer,
			startOn:     false,
			startMode:   "cool",
			startTarget: 26.0,
			temps:       []float64{26.5, 25.4, 26.8, 25.3, 26.9, 25.5},
		},
		{
			name:        "夏季运行态在目标26℃±0.9℃内抖动不反复关停",
			season:      policy.SeasonSummer,
			startOn:     true,
			startMode:   "cool",
			startTarget: 26.0,
			temps:       []float64{26.2, 25.8, 26.4, 25.2, 26.7, 25.5},
		},
		{
			name:        "冬季关机态在目标20℃±0.9℃内抖动不启停",
			season:      policy.SeasonWinter,
			startOn:     false,
			startMode:   "heat",
			startTarget: 20.0,
			temps:       []float64{19.4, 20.5, 19.2, 20.8, 19.5},
		},
		{
			name:        "冬季运行态在目标20℃±0.9℃内抖动不反复关停",
			season:      policy.SeasonWinter,
			startOn:     true,
			startMode:   "heat",
			startTarget: 20.0,
			temps:       []float64{19.8, 20.3, 19.5, 20.8, 19.2},
		},
		{
			name:        "春秋室外33℃兜底制冷运行态在25℃±0.9℃内抖动不反复关停",
			season:      policy.SeasonAutumn,
			outdoorTemp: f64(33.0),
			startOn:     true,
			startMode:   "cool",
			startTarget: 25.0,
			temps:       []float64{25.8, 24.5, 25.9, 24.2, 25.6},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			on := tc.startOn
			switches := 0
			for i, temp := range tc.temps {
				dec := policy.EvaluateComfort(policy.ComfortInput{
					Season:         tc.season,
					IndoorTemp:     f64(temp),
					IndoorHumidity: f64(55.0),
					OutdoorTemp:    tc.outdoorTemp,
					CurrentOn:      on,
					CurrentMode:    tc.startMode,
					CurrentTarget:  f64(tc.startTarget),
					Now:            now.Add(time.Duration(i*5) * time.Minute),
				})
				if dec.ShouldTurnOn != on {
					on = dec.ShouldTurnOn
					switches++
				}
			}
			if switches != 0 {
				t.Fatalf("在 1℃ 滞回带内抖动不应启停，实际切换 %d 次", switches)
			}
		})
	}

	// 越过 1℃ 滞回边界才真正开启/关闭
	openDec := policy.EvaluateComfort(policy.ComfortInput{
		Season: policy.SeasonSummer, IndoorTemp: f64(28.5), IndoorHumidity: f64(55.0), CurrentOn: false, Now: now,
	})
	if openDec.Action != policy.ComfortActionCool || !openDec.ShouldTurnOn {
		t.Fatalf("高于目标+1℃应开启制冷，实际 %+v", openDec)
	}
	closeDec := policy.EvaluateComfort(policy.ComfortInput{
		Season: policy.SeasonSummer, IndoorTemp: f64(24.5), IndoorHumidity: f64(55.0),
		CurrentOn: true, CurrentMode: "cool", CurrentTarget: f64(26.0), Now: now.Add(5 * time.Minute),
	})
	if closeDec.ShouldTurnOn {
		t.Fatalf("低于等于目标-1℃应关闭压缩机，实际 %+v", closeDec)
	}
}

func TestEvaluateComfortMinSwitchInterval60s(t *testing.T) {
	now := time.Date(2026, 7, 15, 14, 1, 0, 0, time.UTC)

	tests := []struct {
		name       string
		elapsed    time.Duration
		wantAction policy.ComfortAction
	}{
		{name: "30秒内被最小间隔拦截", elapsed: 30 * time.Second, wantAction: policy.ComfortActionHold},
		{name: "满60秒放行切换", elapsed: 60 * time.Second, wantAction: policy.ComfortActionCool},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			last := now.Add(-tc.elapsed)
			dec := policy.EvaluateComfort(policy.ComfortInput{
				Season: policy.SeasonSummer, IndoorTemp: f64(31.0), IndoorHumidity: f64(65.0),
				CurrentOn: false, LastSwitchAt: &last, Now: now,
			})
			if dec.Action != tc.wantAction {
				t.Fatalf("elapsed=%v: Action = %q, want %q (%q)", tc.elapsed, dec.Action, tc.wantAction, dec.Reason)
			}
		})
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
