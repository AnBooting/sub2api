package service

import (
	"context"
	"time"
)

// RequestAttemptBudgetLimits 返回 TS-02 单逻辑请求的发送次数与时间预算（C-8）。
// settingService 为 nil 或返回非正值时回退代码常量，保证既有测试（多处传 nil）行为不变。
//
// S3 只有 OpenAI E7（/v1/responses）handler 通过本访问器取配置；其余 Provider
// 的发送钩子在 S3 仍是 nil 预算 = 不计数，等 S3b 才铺开。
func (s *OpenAIGatewayService) RequestAttemptBudgetLimits(ctx context.Context) (int, time.Duration) {
	maxSends, window := DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow
	if s != nil && s.settingService != nil {
		if v, w := s.settingService.GatewayRequestAttemptBudgetLimits(ctx); v > 0 && w > 0 {
			maxSends, window = v, w
		}
	}
	return maxSends, window
}

// MaxSends 返回预算允许的真实上游发送次数上限（nil 预算返回 0）。
// S1 的 RequestAttemptBudget 只暴露 Used/Remaining/Deadline/Exhausted；S3 的
// 结构化日志需要 budget_max，故在此追加只读访问器（不改动 S1 文件）。
func (b *RequestAttemptBudget) MaxSends() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxSends
}
