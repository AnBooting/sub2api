//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestToModelPlazaGroupDTOCarriesDeepSeekPricingState 模型广场的分组必须带出峰谷状态。
func TestToModelPlazaGroupDTOCarriesDeepSeekPricingState(t *testing.T) {
	out := toModelPlazaGroupDTO(&service.PlazaGroup{
		ID:               1,
		Name:             "cn",
		Platform:         service.PlatformDeepseek,
		SubscriptionType: service.SubscriptionTypeStandard,
	}, nil)
	require.NotNil(t, out.DeepSeekPricingState, "模型广场必须带出峰谷状态")
	require.Contains(t,
		[]string{service.DeepSeekPricingStatePeak, service.DeepSeekPricingStateValley},
		out.DeepSeekPricingState.State)
	require.Equal(t, service.DeepSeekPricingTimezone, out.DeepSeekPricingState.Timezone)

	other := toModelPlazaGroupDTO(&service.PlazaGroup{ID: 2, Platform: service.PlatformOpenAI}, nil)
	require.NotNil(t, other.DeepSeekPricingState)
	require.Equal(t, service.DeepSeekPricingStateNone, other.DeepSeekPricingState.State)
}
