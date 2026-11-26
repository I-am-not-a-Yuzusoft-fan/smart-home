// Package policy 实现五层架构中的策略层（纯函数、无 IO、无时钟依赖）。
// 所有环境读数、当前状态与时间均由调用方注入，便于表驱动单测与独立验证。
package policy

import (
	"fmt"
	"math"
	"time"
)

type Season string

const (
	SeasonSpring Season = "spring"
	SeasonSummer Season = "summer"
	SeasonAutumn Season = "autumn"
	SeasonWinter Season = "winter"
)

const (
	DefaultHysteresisC       = 1.0
	DefaultMinSwitchInterval = 60 * time.Second
)

// ResolveSeason 根据配置值与当前时间推导实际生效季节。
func ResolveSeason(configured string, now time.Time) Season {
	switch Season(configured) {
	case SeasonSpring, SeasonSummer, SeasonAutumn, SeasonWinter:
		return Season(configured)
	default:
		switch now.Month() {
		case time.March, time.April, time.May:
			return SeasonSpring
		case time.June, time.July, time.August:
			return SeasonSummer
		case time.September, time.October, time.November:
			return SeasonAutumn
		default:
			return SeasonWinter
		}
	}
}

type ComfortAction string

const (
	ComfortActionCool    ComfortAction = "cool"
	ComfortActionHeat    ComfortAction = "heat"
	ComfortActionNatural ComfortAction = "natural" // 建议自然通风 / 关闭压缩机
	ComfortActionHold    ComfortAction = "hold"    // 滞回死区或最小切换间隔内，维持现状
	ComfortActionWait    ComfortAction = "wait"    // 传感器读数不可用，等待数据
)

type ComfortInput struct {
	Season            Season
	IndoorTemp        *float64
	IndoorHumidity    *float64
	OutdoorTemp       *float64
	OutdoorHumidity   *float64
	CurrentOn         bool
	CurrentMode       string
	CurrentTarget     *float64
	LastSwitchAt      *time.Time
	Now               time.Time
	MinSwitchInterval time.Duration
}

type ComfortDecision struct {
	Action       ComfortAction
	TargetTemp   *float64
	ACMode       string
	ShouldTurnOn bool
	Reason       string
}

// EvaluateComfort 按季节、室内外温湿度动态计算舒适目标温度，应用 1℃ 滞回与最小切换间隔，并生成可读调节依据。
func EvaluateComfort(in ComfortInput) ComfortDecision {
	if in.IndoorTemp == nil || in.IndoorHumidity == nil {
		return ComfortDecision{
			Action:       ComfortActionWait,
			ShouldTurnOn: in.CurrentOn,
			Reason:       "室内温湿度读数不可用，等待传感器数据",
		}
	}

	temp := *in.IndoorTemp
	rh := *in.IndoorHumidity
	minInterval := in.MinSwitchInterval
	if minInterval <= 0 {
		minInterval = DefaultMinSwitchInterval
	}

	var decision ComfortDecision
	switch in.Season {
	case SeasonSummer:
		decision = evalSummer(temp, rh, in)
	case SeasonWinter:
		decision = evalWinter(temp, rh, in)
	case SeasonSpring, SeasonAutumn:
		decision = evalTransitional(temp, rh, in)
	default:
		decision = evalTransitional(temp, rh, in)
	}

	// 最小切换间隔保护：若本次决策会翻转压缩机开关，且距上次切换不足 minInterval，则维持现状。
	if decision.ShouldTurnOn != in.CurrentOn && in.LastSwitchAt != nil && !in.Now.IsZero() {
		if elapsed := in.Now.Sub(*in.LastSwitchAt); elapsed >= 0 && elapsed < minInterval {
			return ComfortDecision{
				Action:       ComfortActionHold,
				ShouldTurnOn: in.CurrentOn,
				Reason: fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，距上次切换仅 %.0f 秒（小于 %.0f 秒最小切换间隔），维持现状",
					temp, rh, elapsed.Seconds(), minInterval.Seconds()),
			}
		}
	}
	return decision
}

func evalSummer(temp, rh float64, in ComfortInput) ComfortDecision {
	// 夏季设定值约束区间 24–28℃（GB 50736-2012 表 3.0.2 供冷工况区间转用）
	raw := 26.0
	if rh >= 70 {
		raw -= 1.0
	} else if rh <= 40 {
		raw += 0.5
	}
	if temp >= 32 {
		raw -= 0.5
	} else if temp <= 26 {
		raw += 1.0
	}
	target := clamp(math.Round(raw), 24, 28)

	if !in.CurrentOn {
		if temp > target+DefaultHysteresisC {
			return ComfortDecision{
				Action:       ComfortActionCool,
				TargetTemp:   &target,
				ACMode:       "cool",
				ShouldTurnOn: true,
				Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，按照夏季温控策略（24–28℃），开启制冷并设定目标温度 %.0f℃", temp, rh, target),
			}
		}
		return ComfortDecision{
			Action:       ComfortActionHold,
			ShouldTurnOn: false,
			Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，处于夏季目标 %.0f℃ ±1℃ 滞回区间内，维持关闭", temp, rh, target),
		}
	}

	// 当前已开机
	if in.CurrentMode == "cool" && temp <= target-DefaultHysteresisC {
		return ComfortDecision{
			Action:       ComfortActionNatural,
			ShouldTurnOn: false,
			Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，已降至夏季目标 %.0f℃−1℃ 滞回下限，关闭空调压缩机", temp, rh, target),
		}
	}
	if in.CurrentMode == "cool" && math.Abs(temp-target) <= DefaultHysteresisC && in.CurrentTarget != nil && *in.CurrentTarget == target {
		return ComfortDecision{
			Action:       ComfortActionHold,
			TargetTemp:   &target,
			ACMode:       "cool",
			ShouldTurnOn: true,
			Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，处于夏季目标 %.0f℃ ±1℃ 滞回区间内，维持运行", temp, rh, target),
		}
	}
	return ComfortDecision{
		Action:       ComfortActionCool,
		TargetTemp:   &target,
		ACMode:       "cool",
		ShouldTurnOn: true,
		Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，按照夏季温控策略（24–28℃），设定制冷目标温度 %.0f℃", temp, rh, target),
	}
}

func evalWinter(temp, rh float64, in ComfortInput) ComfortDecision {
	// 冬季设定值约束区间 18–22℃（GB 50736-2012 表 3.0.2 供热工况区间转用）
	raw := 20.0
	if temp <= 14 {
		raw += 1.0
	} else if temp >= 19 {
		raw -= 1.0
	}
	if rh >= 70 {
		raw += 1.0
	}
	target := clamp(math.Round(raw), 18, 22)

	if !in.CurrentOn {
		if temp < target-DefaultHysteresisC {
			return ComfortDecision{
				Action:       ComfortActionHeat,
				TargetTemp:   &target,
				ACMode:       "heat",
				ShouldTurnOn: true,
				Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，按照冬季温控策略（18–22℃），开启制热并设定目标温度 %.0f℃", temp, rh, target),
			}
		}
		return ComfortDecision{
			Action:       ComfortActionHold,
			ShouldTurnOn: false,
			Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，处于冬季目标 %.0f℃ ±1℃ 滞回区间内，维持关闭", temp, rh, target),
		}
	}

	if in.CurrentMode == "heat" && temp >= target+DefaultHysteresisC {
		return ComfortDecision{
			Action:       ComfortActionNatural,
			ShouldTurnOn: false,
			Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，已达冬季目标 %.0f℃+1℃ 滞回上限，关闭空调压缩机", temp, rh, target),
		}
	}
	if in.CurrentMode == "heat" && math.Abs(temp-target) <= DefaultHysteresisC && in.CurrentTarget != nil && *in.CurrentTarget == target {
		return ComfortDecision{
			Action:       ComfortActionHold,
			TargetTemp:   &target,
			ACMode:       "heat",
			ShouldTurnOn: true,
			Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，处于冬季目标 %.0f℃ ±1℃ 滞回区间内，维持运行", temp, rh, target),
		}
	}
	return ComfortDecision{
		Action:       ComfortActionHeat,
		TargetTemp:   &target,
		ACMode:       "heat",
		ShouldTurnOn: true,
		Reason:       fmt.Sprintf("当前室温 %.1f℃、湿度 %.0f%%，按照冬季温控策略（18–22℃），设定制热目标温度 %.0f℃", temp, rh, target),
	}
}

func evalTransitional(temp, rh float64, in ComfortInput) ComfortDecision {
	seasonName := "春季"
	if in.Season == SeasonAutumn {
		seasonName = "秋季"
	}

	// 春秋自然通风判据：未配置室外传感器，或室外温度在 18–26℃ 区间内，优先建议自然通风、不启动压缩机。
	if in.OutdoorTemp == nil || (*in.OutdoorTemp >= 18 && *in.OutdoorTemp <= 26) {
		outdoorDesc := "未配置室外传感器"
		if in.OutdoorTemp != nil {
			outdoorDesc = fmt.Sprintf("室外 %.1f℃", *in.OutdoorTemp)
		}
		return ComfortDecision{
			Action:       ComfortActionNatural,
			ShouldTurnOn: false,
			Reason:       fmt.Sprintf("当前%s，室温 %.1f℃、湿度 %.0f%%（%s），按照春秋温控策略优先建议自然通风，不启动空调", seasonName, temp, rh, outdoorDesc),
		}
	}

	// 室外温度超出 18–26℃ 时的兜底调温（带 1℃ 滞回）
	outdoor := *in.OutdoorTemp
	if temp > 26+DefaultHysteresisC {
		target := 25.0
		return ComfortDecision{
			Action:       ComfortActionCool,
			TargetTemp:   &target,
			ACMode:       "cool",
			ShouldTurnOn: true,
			Reason:       fmt.Sprintf("当前%s，室温 %.1f℃、湿度 %.0f%%、室外 %.1f℃ 超出自然通风区间，启动兜底制冷并设定目标温度 25℃", seasonName, temp, rh, outdoor),
		}
	}
	if temp < 18-DefaultHysteresisC {
		target := 20.0
		return ComfortDecision{
			Action:       ComfortActionHeat,
			TargetTemp:   &target,
			ACMode:       "heat",
			ShouldTurnOn: true,
			Reason:       fmt.Sprintf("当前%s，室温 %.1f℃、湿度 %.0f%%、室外 %.1f℃ 超出自然通风区间，启动兜底制热并设定目标温度 20℃", seasonName, temp, rh, outdoor),
		}
	}
	return ComfortDecision{
		Action:       ComfortActionNatural,
		ShouldTurnOn: false,
		Reason:       fmt.Sprintf("当前%s，室温 %.1f℃、湿度 %.0f%%（室外 %.1f℃），室温处于 18–26℃ 舒适带内，建议自然通风", seasonName, temp, rh, outdoor),
	}
}

func clamp(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
