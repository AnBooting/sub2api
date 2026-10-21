package service

import (
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// 官方峰谷档位与「下一次切换时刻」的单一实现。
//
// 判定口径必须与计费路径完全一致：
//   - 档位倍率用 deepseekPeakMultiplierWithCalendar（计费热路径同一函数）；
//   - 日历用同一份 deepseekHolidayCalendar（effectiveDeepseekCalendar 同一套回落）；
//   - 窗口边界用官方固定时段（09:00-12:00 / 14:00-18:00，工作日，北京时间）。
// 因此不会出现「界面说在峰段、计费按谷价」或「切换时间与档位对不上」。

// deepseekOfficialPeakBoundaries 是官方峰价窗口的边界时刻（当日分钟数，北京时间）。
var deepseekOfficialPeakBoundaries = []int{9 * 60, 12 * 60, 14 * 60, 18 * 60}

// deepseekOfficialNextSwitchLookaheadDays 是向前查找下一个切换点的天数上限。
//
// 覆盖最长法定假期（春节等 9 天连休）后仍有工作日，故 14 天足够；
// 找不到切换点时返回 false（调用方按「无切换点」处理）。
const deepseekOfficialNextSwitchLookaheadDays = 14

// deepseekOfficialStateAt 返回官方峰谷在 now 的档位、文案与倍率。
//
// 档位判定与计费共用 deepseekPeakMultiplierWithCalendar，界面与计费不会两套。
func deepseekOfficialStateAt(now time.Time, calendar *deepseekHolidayCalendar) (state string, label string, multiplier float64) {
	multiplier = deepseekPeakMultiplierWithCalendar(now, calendar)
	if multiplier > 1 {
		return DeepSeekPricingStatePeak, deepseekOfficialPeakWindowLabel, multiplier
	}
	return DeepSeekPricingStateValley, fmt.Sprintf("谷价（%s 之外）", deepseekOfficialPeakWindowLabel), multiplier
}

// deepseekOfficialNextSwitchAt 返回官方峰谷档位的下一次切换时刻。
//
// 做法：枚举接下来若干天的窗口边界（09:00/12:00/14:00/18:00，北京时间），
// 逐个用**同一个** deepseekPeakMultiplierWithCalendar 求值，
// 取第一个与当前倍率不同的边界——即下一次档位变化发生的时刻。
// 这样「下一次切换时间」与「当前档位」必然自洽。
func deepseekOfficialNextSwitchAt(now time.Time, calendar *deepseekHolidayCalendar) (time.Time, bool) {
	current := deepseekPeakMultiplierWithCalendar(now, calendar)
	beijing := now.In(deepSeekLocation)
	day := time.Date(beijing.Year(), beijing.Month(), beijing.Day(), 0, 0, 0, 0, deepSeekLocation)
	for offset := 0; offset <= deepseekOfficialNextSwitchLookaheadDays; offset++ {
		date := day.AddDate(0, 0, offset)
		for _, minute := range deepseekOfficialPeakBoundaries {
			candidate := date.Add(time.Duration(minute) * time.Minute)
			if !candidate.After(now) {
				continue
			}
			if deepseekPeakMultiplierWithCalendar(candidate, calendar) != current {
				return candidate, true
			}
		}
	}
	return time.Time{}, false
}

// peakWindowGroup 返回只带峰谷判定所需字段的 Group 副本。
//
// 可用渠道/模型广场只持有分组概要（不是完整 Group），但峰谷判定只需要这几个字段，
// 因此拼一个最小 Group 复用同一份判定，不另写一套口径。
func peakWindowGroup(platform, subscriptionType string, peakEnabled bool, peakStart, peakEnd string, peakMultiplier float64) *Group {
	return &Group{
		Platform:           platform,
		SubscriptionType:   subscriptionType,
		PeakRateEnabled:    peakEnabled,
		PeakStart:          peakStart,
		PeakEnd:            peakEnd,
		PeakRateMultiplier: peakMultiplier,
	}
}

// DeepSeekPricingStateAt 返回分组概要引用的峰谷状态（与 Group.DeepSeekPricingStateAt 同一口径）。
func (r AvailableGroupRef) DeepSeekPricingStateAt(now time.Time) *DeepSeekPricingState {
	return peakWindowGroup(r.Platform, r.SubscriptionType, r.PeakRateEnabled, r.PeakStart, r.PeakEnd, r.PeakRateMultiplier).DeepSeekPricingStateAt(now)
}

// DeepSeekPricingStateAt 返回广场分组概要引用的峰谷状态（与 Group.DeepSeekPricingStateAt 同一口径）。
func (g PlazaGroup) DeepSeekPricingStateAt(now time.Time) *DeepSeekPricingState {
	return peakWindowGroup(g.Platform, g.SubscriptionType, g.PeakRateEnabled, g.PeakStart, g.PeakEnd, g.PeakRateMultiplier).DeepSeekPricingStateAt(now)
}

// customPeakWindowNextSwitchAt 返回自定义峰谷窗口的下一次切换时刻。
//
// 与 customPeakWindowAt 同一口径：同一时区（全局系统时区）、同一当日分钟数、
// 同一「左闭右开」窗口。窗口是每日常驻的，所以：
//   - 峰段内 → 今天窗口结束即切回谷价；
//   - 谷段内 → 今天窗口开始（若已过则明天同一时刻）切回峰价。
func (g *Group) customPeakWindowNextSwitchAt(now time.Time) (time.Time, bool) {
	window := g.customPeakWindowAt(now)
	if !window.Configured {
		return time.Time{}, false
	}
	loc := timezone.Location()
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if window.Active {
		end := day.Add(time.Duration(window.EndMinutes) * time.Minute)
		if end.After(now) {
			return end, true
		}
		return day.AddDate(0, 0, 1).Add(time.Duration(window.StartMinutes) * time.Minute), true
	}
	start := day.Add(time.Duration(window.StartMinutes) * time.Minute)
	if start.After(now) {
		return start, true
	}
	return day.AddDate(0, 0, 1).Add(time.Duration(window.StartMinutes) * time.Minute), true
}
