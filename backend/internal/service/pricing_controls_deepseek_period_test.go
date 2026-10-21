//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// TestGetPricingControlStatusExposesDeepSeekCurrentPeriod 管理端要能看到官方峰谷的当前档位与下一档切换时间。
func TestGetPricingControlStatusExposesDeepSeekCurrentPeriod(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	status := s.GetPricingControlStatus()

	period, ok := status["deepseek_current_period"].(map[string]any)
	require.True(t, ok, "管理端状态必须给出 deepseek_current_period")
	require.Contains(t, period, "state")
	require.Contains(t, period, "label")
	require.Contains(t, period, "timezone")
	require.Contains(t, period, "window")
	require.Contains(t, period, "multiplier")
	require.Contains(t, period, "next_switch_at")
	require.Contains(t, period, "calendar_year_missing")
	require.Equal(t, DeepSeekPricingTimezone, period["timezone"])
	require.Equal(t, deepseekOfficialPeakWindow, period["window"])

	state, _ := period["state"].(string)
	require.Contains(t, []string{DeepSeekPricingStatePeak, DeepSeekPricingStateValley}, state)
	multiplier, _ := period["multiplier"].(float64)
	require.Equal(t, deepseekPeakMultiplierWithCalendar(time.Now(), s.deepseekCalendarAt(time.Now())), multiplier,
		"管理端展示的倍率必须与计费口径一致")
}
