//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

// TestDeepSeekOfficialNextSwitchAtIsConsistentWithState 锁住「下一次切换时刻」与档位的自洽：
// 官方峰谷的状态与下一次切换必须来自同一次判定（同一份日历、同一套窗口规则），
// 到切换点时档位必须真的发生变化。
func TestDeepSeekOfficialNextSwitchAtIsConsistentWithState(t *testing.T) {
	group := &Group{Platform: PlatformDeepseek}
	for _, tc := range []struct {
		name       string
		now        time.Time
		wantState  string
		wantSwitch time.Time
	}{
		{"上午峰段切到午间谷价", beijing(2026, time.October, 8, 10, 0), DeepSeekPricingStatePeak, beijing(2026, time.October, 8, 12, 0)},
		{"午间谷价切到下午峰段", beijing(2026, time.October, 8, 13, 0), DeepSeekPricingStateValley, beijing(2026, time.October, 8, 14, 0)},
		{"下午峰段切到晚间谷价", beijing(2026, time.October, 8, 15, 0), DeepSeekPricingStatePeak, beijing(2026, time.October, 8, 18, 0)},
		{"晚间谷价切到次日上班峰段", beijing(2026, time.October, 8, 19, 0), DeepSeekPricingStateValley, beijing(2026, time.October, 9, 9, 0)},
		{"周五晚间切到下周一上班", beijing(2026, time.October, 9, 19, 0), DeepSeekPricingStateValley, beijing(2026, time.October, 12, 9, 0)},
		{"法定假日全天谷价，节后首个工作日上班", beijing(2026, time.October, 5, 10, 0), DeepSeekPricingStateValley, beijing(2026, time.October, 8, 9, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := group.DeepSeekPricingStateAt(tc.now)
			require.Equal(t, tc.wantState, state.State)
			require.NotNil(t, state.NextSwitchAt, "必须给出下一次切换时刻")
			require.True(t, tc.wantSwitch.Equal(*state.NextSwitchAt),
				"下一次切换时刻应为 %s，实际 %s", tc.wantSwitch, state.NextSwitchAt)

			// 自洽性：到了切换点，档位必须真的变了。
			after := group.DeepSeekPricingStateAt(tc.wantSwitch)
			require.NotEqual(t, state.State, after.State,
				"切换时刻 %s 的档位与之前相同，state 与 next_switch_at 不自洽", tc.wantSwitch)
		})
	}
}

// TestDeepSeekOfficialNextSwitchAtUsesSameCalendarAsState 切换点必须与档位用同一份日历：
// 假日期间没有峰段，切换点应落在节后第一个工作日，而不是按工作日规则硬算。
func TestDeepSeekOfficialNextSwitchAtUsesSameCalendarAsState(t *testing.T) {
	group := &Group{Platform: PlatformDeepseek}
	holiday := beijing(2026, time.October, 6, 10, 0)
	state := group.DeepSeekPricingStateAt(holiday)
	require.Equal(t, DeepSeekPricingStateValley, state.State)
	require.NotNil(t, state.NextSwitchAt)
	require.True(t, beijing(2026, time.October, 8, 9, 0).Equal(*state.NextSwitchAt),
		"实际 %s", state.NextSwitchAt)
	// 假日当天不会有峰段：切换点之前倍率恒为 1。
	require.Equal(t, 1.0, deepseekPeakMultiplierWithCalendar(state.NextSwitchAt.Add(-time.Minute), defaultDeepseekCalendar))
}

// TestGroupCustomPeakWindowNextSwitch 自定义窗口的下一次切换时刻。
func TestGroupCustomPeakWindowNextSwitch(t *testing.T) {
	group := &Group{
		Platform:           PlatformDeepseek,
		SubscriptionType:   SubscriptionTypeSubscription,
		PeakRateEnabled:    true,
		PeakStart:          "10:00",
		PeakEnd:            "14:00",
		PeakRateMultiplier: 1.5,
	}
	loc := timezone.Location()

	inside := group.DeepSeekPricingStateAt(time.Date(2026, time.October, 8, 11, 0, 0, 0, loc))
	require.Equal(t, DeepSeekPricingStatePeak, inside.State)
	require.NotNil(t, inside.NextSwitchAt)
	require.True(t, time.Date(2026, time.October, 8, 14, 0, 0, 0, loc).Equal(*inside.NextSwitchAt),
		"峰段内应在今天窗口结束时切回谷价，实际 %s", inside.NextSwitchAt)

	outside := group.DeepSeekPricingStateAt(time.Date(2026, time.October, 8, 15, 0, 0, 0, loc))
	require.Equal(t, DeepSeekPricingStateValley, outside.State)
	require.NotNil(t, outside.NextSwitchAt)
	require.True(t, time.Date(2026, time.October, 9, 10, 0, 0, 0, loc).Equal(*outside.NextSwitchAt),
		"谷段内应切到明天同一窗口，实际 %s", outside.NextSwitchAt)
}

// TestGroupDeepSeekStateHasNoNextSwitchWhenNotApplicable 不适用峰谷的分组没有切换点。
func TestGroupDeepSeekStateHasNoNextSwitchWhenNotApplicable(t *testing.T) {
	group := &Group{Platform: PlatformOpenAI}
	state := group.DeepSeekPricingStateAt(beijing(2026, time.October, 8, 10, 0))
	require.Equal(t, DeepSeekPricingStateNone, state.State)
	require.Nil(t, state.NextSwitchAt, "不适用的分组不应给出切换时刻")
}
