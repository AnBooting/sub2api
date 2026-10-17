package service

import (
	"context"
	"time"
)

// RequestAttemptBudgetLimits 返回 TS-02 单逻辑请求的发送次数与时间预算（C-8）。
// 与 OpenAIGatewayService.RequestAttemptBudgetLimits 同一契约；settingService 为
// nil 或返回非正值时回退代码常量，保证既有测试（多处传 nil）行为不变。
func (s *GatewayService) RequestAttemptBudgetLimits(ctx context.Context) (int, time.Duration) {
	maxSends, window := DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow
	if s != nil && s.settingService != nil {
		if v, w := s.settingService.GatewayRequestAttemptBudgetLimits(ctx); v > 0 && w > 0 {
			maxSends, window = v, w
		}
	}
	return maxSends, window
}
