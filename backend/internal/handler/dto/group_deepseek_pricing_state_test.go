//go:build unit

package dto

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TestGroupDTODeepSeekPricingState 用户与管理员两条 DTO 映射都要带出峰谷状态。
func TestGroupDTODeepSeekPricingState(t *testing.T) {
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

	userDTO := GroupFromService(group)
	if userDTO == nil {
		t.Fatal("用户分组 DTO 不应为 nil")
	}
	if userDTO.DeepSeekPricingState == nil {
		t.Fatal("用户分组 DTO 必须带出 deepseek_pricing_state")
	}
	adminDTO := GroupFromServiceAdmin(group)
	if adminDTO == nil {
		t.Fatal("管理员分组 DTO 不应为 nil")
	}
	if adminDTO.DeepSeekPricingState == nil {
		t.Fatal("管理员分组 DTO 必须带出 deepseek_pricing_state")
	}

	for name, state := range map[string]*DeepSeekPricingStateDTO{
		"user":  userDTO.DeepSeekPricingState,
		"admin": adminDTO.DeepSeekPricingState,
	} {
		switch state.State {
		case service.DeepSeekPricingStatePeak, service.DeepSeekPricingStateValley, service.DeepSeekPricingStateNone:
		default:
			t.Fatalf("%s DTO 的 state 非法：%q", name, state.State)
		}
		if state.Timezone != service.DeepSeekPricingTimezone {
			t.Fatalf("%s DTO 的 timezone 应为 %s，实际 %s", name, service.DeepSeekPricingTimezone, state.Timezone)
		}
		if state.Multiplier != 1.5 {
			t.Fatalf("%s DTO 的 multiplier 应为自定义窗口倍率 1.5，实际 %v", name, state.Multiplier)
		}
		if state.Label == "" {
			t.Fatalf("%s DTO 的 label 不得为空", name)
		}
		// 全天自定义窗口恒为峰价，且 Label 反映自定义窗口。
		if state.State != service.DeepSeekPricingStatePeak {
			t.Fatalf("%s DTO 全天窗口应为 peak，实际 %s", name, state.State)
		}
		if !strings.Contains(state.Label, "00:00-23:59") {
			t.Fatalf("%s DTO 的 label 必须反映自定义窗口，实际 %q", name, state.Label)
		}
	}
}

// TestGroupDTODeepSeekPricingStateNone 普通分组应给出 none 且倍率为 1。
func TestGroupDTODeepSeekPricingStateNone(t *testing.T) {
	group := &service.Group{ID: 2, Name: "openai", Platform: service.PlatformOpenAI}

	userDTO := GroupFromService(group)
	if userDTO.DeepSeekPricingState == nil {
		t.Fatal("用户分组 DTO 必须带出 deepseek_pricing_state")
	}
	if userDTO.DeepSeekPricingState.State != service.DeepSeekPricingStateNone {
		t.Fatalf("普通分组应为 %s，实际 %s", service.DeepSeekPricingStateNone, userDTO.DeepSeekPricingState.State)
	}
	if userDTO.DeepSeekPricingState.Multiplier != 1 {
		t.Fatalf("普通分组倍率应为 1，实际 %v", userDTO.DeepSeekPricingState.Multiplier)
	}
}
