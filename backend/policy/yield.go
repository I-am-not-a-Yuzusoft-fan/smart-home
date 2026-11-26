package policy

import (
	"fmt"
	"math"
	"time"
)

const (
	DefaultYieldWindow      = 30 * time.Minute
	YieldTempDeltaThreshold = 3.0   // ±3℃
	YieldRHDeltaThreshold   = 10.0  // ±10%RH
	YieldLuxDeltaThreshold  = 200.0 // ±200 lux
)

// EnvReadings 表示一次环境快照（温度/湿度/照度均可缺失）。
type EnvReadings struct {
	Temperature *float64
	Humidity    *float64
	Illuminance *float64
}

// YieldInput 注入手动接管时刻、当前时间、窗口时长与基线/当前环境读数。
type YieldInput struct {
	ManualAt    time.Time
	Now         time.Time
	Window      time.Duration
	HasBaseline bool
	Baseline    EnvReadings
	Current     EnvReadings
}

// YieldDecision 表示自动策略是否应当让位以及可读原因。
type YieldDecision struct {
	Suppressed bool
	Reason     string
}

// EvaluateYield 按 FR-5.9 判断某设备某能力是否处于手动接管让位窗口内。
func EvaluateYield(in YieldInput) YieldDecision {
	if in.ManualAt.IsZero() {
		return YieldDecision{Suppressed: false}
	}
	window := in.Window
	if window <= 0 {
		window = DefaultYieldWindow
	}
	elapsed := in.Now.Sub(in.ManualAt)
	if elapsed >= window {
		return YieldDecision{
			Suppressed: false,
			Reason:     "手动接管让位窗口已过期",
		}
	}
	remaining := window - elapsed
	if remaining < 0 {
		remaining = 0
	}

	// 重启或无基线场景下保守处理：窗口内不放行，保护用户手动意图（CONTEXT.md 状态过期哲学）。
	if !in.HasBaseline {
		return YieldDecision{
			Suppressed: true,
			Reason:     fmt.Sprintf("手动接管中（无环境基线，窗口内保守让位，剩余 %.0f 分钟）", math.Ceil(remaining.Minutes())),
		}
	}

	if in.Baseline.Temperature != nil && in.Current.Temperature != nil {
		if delta := math.Abs(*in.Current.Temperature - *in.Baseline.Temperature); delta >= YieldTempDeltaThreshold {
			return YieldDecision{
				Suppressed: false,
				Reason:     fmt.Sprintf("环境温度变化 %.1f℃（达到 ±%.0f℃ 阈值），手动接管让位已解除", delta, YieldTempDeltaThreshold),
			}
		}
	}
	if in.Baseline.Humidity != nil && in.Current.Humidity != nil {
		if delta := math.Abs(*in.Current.Humidity - *in.Baseline.Humidity); delta >= YieldRHDeltaThreshold {
			return YieldDecision{
				Suppressed: false,
				Reason:     fmt.Sprintf("环境湿度变化 %.1f%%RH（达到 ±%.0f%%RH 阈值），手动接管让位已解除", delta, YieldRHDeltaThreshold),
			}
		}
	}
	if in.Baseline.Illuminance != nil && in.Current.Illuminance != nil {
		if delta := math.Abs(*in.Current.Illuminance - *in.Baseline.Illuminance); delta >= YieldLuxDeltaThreshold {
			return YieldDecision{
				Suppressed: false,
				Reason:     fmt.Sprintf("环境照度变化 %.0f lux（达到 ±%.0f lux 阈值），手动接管让位已解除", delta, YieldLuxDeltaThreshold),
			}
		}
	}

	return YieldDecision{
		Suppressed: true,
		Reason:     fmt.Sprintf("手动接管中（30 分钟让位窗口剩余 %.0f 分钟，自动策略让位）", math.Ceil(remaining.Minutes())),
	}
}
