package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TestGroupFromServiceOmitsSchedulingStrategy 钉死普通用户侧分组 DTO 不暴露调度策略：
// 策略只对管理员可见，普通用户接口不应泄露内部调度配置。
func TestGroupFromServiceOmitsSchedulingStrategy(t *testing.T) {
	group := &service.Group{
		ID:                 7,
		Name:               "scheduling-gated",
		Platform:           service.PlatformAnthropic,
		Status:             service.StatusActive,
		SchedulingStrategy: string(service.SchedulingStrategyLatency),
	}

	for name, got := range map[string]any{
		"GroupFromService":        GroupFromService(group),
		"GroupFromServiceShallow": GroupFromServiceShallow(group),
	} {
		fields := marshalToMap(t, got)
		if _, ok := fields["scheduling_strategy"]; ok {
			t.Errorf("%s: 普通用户 DTO 不得包含 %q", name, "scheduling_strategy")
		}
	}
}

// TestGroupFromServiceAdminIncludesSchedulingStrategy 钉死管理端读写分组调度策略。
func TestGroupFromServiceAdminIncludesSchedulingStrategy(t *testing.T) {
	group := &service.Group{
		ID:                 7,
		Name:               "scheduling-gated",
		Platform:           service.PlatformAnthropic,
		Status:             service.StatusActive,
		SchedulingStrategy: string(service.SchedulingStrategyLatency),
	}

	admin := GroupFromServiceAdmin(group)
	if admin.SchedulingStrategy != string(service.SchedulingStrategyLatency) {
		t.Fatalf("管理员 DTO 未透传分组调度策略: %+v", admin)
	}

	fields := marshalToMap(t, admin)
	if _, ok := fields["scheduling_strategy"]; !ok {
		t.Error("管理员 DTO 应包含 scheduling_strategy")
	}
}
