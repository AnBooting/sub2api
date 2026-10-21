//go:build unit

package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// beijing 把墙上时间转换成官方判定使用的北京时间。
func beijing(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
}

// TestGroupDeepSeekPricingState_OfficialPeak 官方峰价窗口：peak 且倍率>1。
func TestGroupDeepSeekPricingState_OfficialPeak(t *testing.T) {
	group := &Group{Platform: PlatformDeepseek}

	// 2026-10-08 是周四，且不在内置法定假日区间（10-01..10-07）内。
	state := group.DeepSeekPricingStateAt(beijing(2026, time.October, 8, 10, 0))

	if state.State != DeepSeekPricingStatePeak {
		t.Fatalf("峰价窗口应为 %s，实际 %s", DeepSeekPricingStatePeak, state.State)
	}
	if state.Multiplier <= 1 {
		t.Fatalf("峰价时段倍率必须 >1，实际 %v", state.Multiplier)
	}
	if state.Timezone != DeepSeekPricingTimezone {
		t.Fatalf("时区应为 %s，实际 %s", DeepSeekPricingTimezone, state.Timezone)
	}
	if !strings.Contains(state.Label, "09:00-12:00") {
		t.Fatalf("Label 必须含官方窗口信息，实际 %q", state.Label)
	}
}

// TestGroupDeepSeekPricingState_OfficialValley 官方谷价时段：valley 且倍率 1。
func TestGroupDeepSeekPricingState_OfficialValley(t *testing.T) {
	group := &Group{Platform: PlatformDeepseek}

	state := group.DeepSeekPricingStateAt(beijing(2026, time.October, 8, 13, 0))

	if state.State != DeepSeekPricingStateValley {
		t.Fatalf("谷价时段应为 %s，实际 %s", DeepSeekPricingStateValley, state.State)
	}
	if state.Multiplier != 1 {
		t.Fatalf("谷价时段倍率应为 1，实际 %v", state.Multiplier)
	}
	if !strings.Contains(state.Label, "09:00-12:00") {
		t.Fatalf("Label 必须含官方窗口信息，实际 %q", state.Label)
	}
}

// TestGroupDeepSeekPricingState_OfficialHoliday 法定假日优先：假日窗口内也是谷价。
func TestGroupDeepSeekPricingState_OfficialHoliday(t *testing.T) {
	group := &Group{Platform: PlatformDeepseek}

	// 2026-10-05 在内置法定假日区间（10-01..10-07）内。
	state := group.DeepSeekPricingStateAt(beijing(2026, time.October, 5, 10, 0))

	if state.State != DeepSeekPricingStateValley {
		t.Fatalf("法定假日必须按谷价处理，实际 %s", state.State)
	}
}

// TestGroupDeepSeekPricingState_NotApplicable 未启用峰谷倍率：none 且倍率 1。
func TestGroupDeepSeekPricingState_NotApplicable(t *testing.T) {
	group := &Group{Platform: PlatformOpenAI}

	state := group.DeepSeekPricingStateAt(beijing(2026, time.October, 8, 10, 0))

	if state.State != DeepSeekPricingStateNone {
		t.Fatalf("未适用分组应为 %s，实际 %s", DeepSeekPricingStateNone, state.State)
	}
	if state.Multiplier != 1 {
		t.Fatalf("未适用分组倍率应为 1，实际 %v", state.Multiplier)
	}
}

// TestGroupDeepSeekPricingState_CustomWindow 自定义窗口优先，Label 反映自定义窗口。
func TestGroupDeepSeekPricingState_CustomWindow(t *testing.T) {
	group := &Group{
		Platform:           PlatformDeepseek,
		SubscriptionType:   SubscriptionTypeSubscription,
		PeakRateEnabled:    true,
		PeakStart:          "10:00",
		PeakEnd:            "14:00",
		PeakRateMultiplier: 1.5,
	}
	inside := time.Date(2026, time.October, 8, 11, 0, 0, 0, timezone.Location())

	state := group.DeepSeekPricingStateAt(inside)
	if state.State != DeepSeekPricingStatePeak {
		t.Fatalf("自定义窗口内应为 %s，实际 %s", DeepSeekPricingStatePeak, state.State)
	}
	if state.Multiplier != 1.5 {
		t.Fatalf("自定义窗口倍率应为 1.5，实际 %v", state.Multiplier)
	}
	if !strings.Contains(state.Label, "10:00-14:00") {
		t.Fatalf("Label 必须反映自定义窗口，实际 %q", state.Label)
	}

	outside := time.Date(2026, time.October, 8, 15, 0, 0, 0, timezone.Location())
	state = group.DeepSeekPricingStateAt(outside)
	if state.State != DeepSeekPricingStateValley {
		t.Fatalf("自定义窗口外应为 %s，实际 %s", DeepSeekPricingStateValley, state.State)
	}
	if state.Multiplier != 1 {
		t.Fatalf("自定义窗口外倍率应为 1，实际 %v", state.Multiplier)
	}
}

// TestGroupDeepSeekPricingState_NilGroup 空分组必须安全返回 none。
func TestGroupDeepSeekPricingState_NilGroup(t *testing.T) {
	var group *Group

	state := group.DeepSeekPricingStateAt(time.Now())
	if state.State != DeepSeekPricingStateNone {
		t.Fatalf("空分组应为 %s，实际 %s", DeepSeekPricingStateNone, state.State)
	}
}

// TestGroupPeakMultiplierAtUnchanged 抽出窗口判定后，计费倍率语义必须保持不变。
func TestGroupPeakMultiplierAtUnchanged(t *testing.T) {
	group := &Group{
		SubscriptionType:   SubscriptionTypeSubscription,
		PeakRateEnabled:    true,
		PeakStart:          "10:00",
		PeakEnd:            "14:00",
		PeakRateMultiplier: 2,
	}
	inside := time.Date(2026, time.October, 8, 11, 0, 0, 0, timezone.Location())
	outside := time.Date(2026, time.October, 8, 15, 0, 0, 0, timezone.Location())
	if got := group.PeakMultiplierAt(inside); got != 2 {
		t.Fatalf("窗口内倍率应为 2，实际 %v", got)
	}
	if got := group.PeakMultiplierAt(outside); got != 1 {
		t.Fatalf("窗口外倍率应为 1，实际 %v", got)
	}
	// 未启用的分组恒为 1。
	disabled := &Group{SubscriptionType: SubscriptionTypeSubscription}
	if got := disabled.PeakMultiplierAt(inside); got != 1 {
		t.Fatalf("未启用分组倍率应为 1，实际 %v", got)
	}
}
