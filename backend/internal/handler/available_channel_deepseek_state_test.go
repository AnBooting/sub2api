//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestFilterUserVisibleGroupsCarriesDeepSeekPricingState 可用渠道里的分组必须带出峰谷状态：
// 前端据此渲染徽标（读不到就不显示，不自己算峰谷）。
func TestFilterUserVisibleGroupsCarriesDeepSeekPricingState(t *testing.T) {
	groups := []service.AvailableGroupRef{
		{ID: 1, Name: "deepseek 分组", Platform: service.PlatformDeepseek, SubscriptionType: service.SubscriptionTypeSubscription},
		{ID: 2, Name: "openai 分组", Platform: service.PlatformOpenAI},
	}
	visible := filterUserVisibleGroups(groups, map[int64]struct{}{1: {}, 2: {}})
	require.Len(t, visible, 2)

	deepseekState := visible[0].DeepSeekPricingState
	require.NotNil(t, deepseekState, "DeepSeek 分组必须带出峰谷状态")
	require.Contains(t,
		[]string{service.DeepSeekPricingStatePeak, service.DeepSeekPricingStateValley},
		deepseekState.State)
	require.Equal(t, service.DeepSeekPricingTimezone, deepseekState.Timezone)

	// 非 DeepSeek 平台不适用峰谷：state=none（前端据此不渲染徽标）。
	require.NotNil(t, visible[1].DeepSeekPricingState)
	require.Equal(t, service.DeepSeekPricingStateNone, visible[1].DeepSeekPricingState.State)
}
