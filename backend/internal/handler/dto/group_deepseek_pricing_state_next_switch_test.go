//go:build unit

package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TestGroupDTODeepSeekPricingStateCarriesNextSwitchAt 用户与管理员两条 DTO 映射都要带出下一次切换时刻。
func TestGroupDTODeepSeekPricingStateCarriesNextSwitchAt(t *testing.T) {
	group := &service.Group{
		ID:                 1,
		Name:               "deepseek",
		Platform:           service.PlatformDeepseek,
		SubscriptionType:   service.SubscriptionTypeSubscription,
		PeakRateEnabled:    true,
		PeakStart:          "00:00",
		PeakEnd:            "23:59",
		PeakRateMultiplier: 1.5,
	}

	now := time.Now()
	for name, state := range map[string]*DeepSeekPricingStateDTO{
		"user":  GroupFromService(group).DeepSeekPricingState,
		"admin": GroupFromServiceAdmin(group).DeepSeekPricingState,
	} {
		if state == nil {
			t.Fatalf("%s DTO 必须带出 deepseek_pricing_state", name)
		}
		if state.NextSwitchAt == nil {
			t.Fatalf("%s DTO 必须带出 next_switch_at", name)
		}
		if !state.NextSwitchAt.After(now) {
			t.Fatalf("%s DTO 的 next_switch_at 必须是将来时刻，实际 %s", name, state.NextSwitchAt)
		}
	}
}
