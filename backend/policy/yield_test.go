package policy_test

import (
	"strings"
	"testing"
	"time"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/policy"
)

func TestEvaluateYieldWindowAndThresholds(t *testing.T) {
	manualAt := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	base := policy.EnvReadings{
		Temperature: f64(25.0),
		Humidity:    f64(50.0),
		Illuminance: f64(300.0),
	}

	tests := []struct {
		name           string
		input          policy.YieldInput
		wantSuppressed bool
		wantReasonPart string
	}{
		{
			name: "无手动记录时不抑制",
			input: policy.YieldInput{
				Now: manualAt.Add(5 * time.Minute),
			},
			wantSuppressed: false,
		},
		{
			name: "30分钟窗口内且环境未突变时抑制自动策略",
			input: policy.YieldInput{
				ManualAt:    manualAt,
				Now:         manualAt.Add(15 * time.Minute),
				HasBaseline: true,
				Baseline:    base,
				Current: policy.EnvReadings{
					Temperature: f64(26.5), // +1.5℃ < 3℃
					Humidity:    f64(55.0), // +5% < 10%
					Illuminance: f64(420.0), // +120lux < 200lux
				},
			},
			wantSuppressed: true,
			wantReasonPart: "手动接管中",
		},
		{
			name: "满30分钟窗口过期后自动放行",
			input: policy.YieldInput{
				ManualAt:    manualAt,
				Now:         manualAt.Add(30 * time.Minute),
				HasBaseline: true,
				Baseline:    base,
				Current:     base,
			},
			wantSuppressed: false,
			wantReasonPart: "过期",
		},
		{
			name: "温度变化达到±3℃立即解除让位",
			input: policy.YieldInput{
				ManualAt:    manualAt,
				Now:         manualAt.Add(5 * time.Minute),
				HasBaseline: true,
				Baseline:    base,
				Current: policy.EnvReadings{
					Temperature: f64(28.0), // +3.0℃
					Humidity:    f64(50.0),
					Illuminance: f64(300.0),
				},
			},
			wantSuppressed: false,
			wantReasonPart: "温度",
		},
		{
			name: "湿度变化达到±10%RH立即解除让位",
			input: policy.YieldInput{
				ManualAt:    manualAt,
				Now:         manualAt.Add(5 * time.Minute),
				HasBaseline: true,
				Baseline:    base,
				Current: policy.EnvReadings{
					Temperature: f64(25.0),
					Humidity:    f64(39.5), // -10.5%RH
					Illuminance: f64(300.0),
				},
			},
			wantSuppressed: false,
			wantReasonPart: "湿度",
		},
		{
			name: "照度变化达到±200lux立即解除让位",
			input: policy.YieldInput{
				ManualAt:    manualAt,
				Now:         manualAt.Add(5 * time.Minute),
				HasBaseline: true,
				Baseline:    base,
				Current: policy.EnvReadings{
					Temperature: f64(25.0),
					Humidity:    f64(50.0),
					Illuminance: f64(505.0), // +205lux
				},
			},
			wantSuppressed: false,
			wantReasonPart: "照度",
		},
		{
			name: "无基线读数时窗口内保守让位不放行",
			input: policy.YieldInput{
				ManualAt:    manualAt,
				Now:         manualAt.Add(10 * time.Minute),
				HasBaseline: false,
				Current:     base,
			},
			wantSuppressed: true,
			wantReasonPart: "手动接管中",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := policy.EvaluateYield(tc.input)
			if got.Suppressed != tc.wantSuppressed {
				t.Fatalf("Suppressed = %v, want %v (reason=%q)", got.Suppressed, tc.wantSuppressed, got.Reason)
			}
			if tc.wantReasonPart != "" && !strings.Contains(got.Reason, tc.wantReasonPart) {
				t.Fatalf("Reason %q 应包含 %q", got.Reason, tc.wantReasonPart)
			}
		})
	}
}
