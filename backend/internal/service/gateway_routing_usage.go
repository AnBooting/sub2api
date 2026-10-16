package service

import (
	"context"
	"fmt"
)

// RoutingFailover 是 usage_logs.failover 的合法取值。
const (
	RoutingFailoverNone    = "none"
	RoutingFailoverAccount = "account"
	RoutingFailoverGroup   = "group"
	RoutingFailoverHA      = "ha"
)

// RoutingUsageMetadata 是一次成功计费请求的不可变 C6 路由元数据。
// 外层指针可选：nil 表示调用方未提供路由事实，UsageLog 保持历史 NULL；
// 非 nil 时四个标量构成完整记录，调用方不得靠零值猜测缺省路由事实。
type RoutingUsageMetadata struct {
	RequestedGroupID int64
	ServedGroupID    int64
	Failover         string
	AttemptCount     int
}

// cloneRoutingUsageMetadata 在 RecordUsage 入口把调用方元数据值快照到独立地址。
// 非 nil 时返回副本，调用方随后修改原指针不会污染本请求的校验与 UsageLog。
func cloneRoutingUsageMetadata(m *RoutingUsageMetadata) *RoutingUsageMetadata {
	if m == nil {
		return nil
	}
	copy := *m
	return &copy
}

func (m *RoutingUsageMetadata) validate() error {
	if m == nil {
		return nil
	}
	if m.RequestedGroupID <= 0 {
		return fmt.Errorf("%w: requested_group_id must be positive", ErrModelPricingUnavailable)
	}
	if m.ServedGroupID <= 0 {
		return fmt.Errorf("%w: served_group_id must be positive", ErrModelPricingUnavailable)
	}
	if m.AttemptCount < 0 {
		return fmt.Errorf("%w: attempt_count must be non-negative", ErrModelPricingUnavailable)
	}
	switch m.Failover {
	case RoutingFailoverNone, RoutingFailoverAccount, RoutingFailoverGroup, RoutingFailoverHA:
	default:
		return fmt.Errorf("%w: invalid failover value %q", ErrModelPricingUnavailable, m.Failover)
	}
	return nil
}

// validateRoutingUsageFacts 在 RecordUsage 任何扣费/额度/日志副作用之前校验路由事实：
//   - nil 保持既有流程；
//   - 锁定 bundle 存在时必须有真实账号执行事实，served 必须等于实际执行分组，
//     requested 必须等于 bundle 的请求首组；
//   - 非锁价路径 served 必须等于 APIKey 的认证分组；
//   - attempt_count/failover 语法由 RoutingUsageMetadata.validate 覆盖。
func validateRoutingUsageFacts(ctx context.Context, apiKey *APIKey, m *RoutingUsageMetadata) error {
	if m == nil {
		return nil
	}
	if err := m.validate(); err != nil {
		return err
	}
	requestPricing := GatewayRequestBillingFromContext(ctx)
	if requestPricing != nil {
		facts, known := gatewayRequestBillingAccountFromContext(ctx)
		if !known {
			return fmt.Errorf("%w: missing actual account execution facts", ErrModelPricingUnavailable)
		}
		if m.ServedGroupID != facts.groupID {
			return fmt.Errorf("%w: served_group_id %d differs from frozen execution group %d", ErrModelPricingUnavailable, m.ServedGroupID, facts.groupID)
		}
		requested := requestPricing.RequestedGroupID()
		if requested == nil || m.RequestedGroupID != *requested {
			return fmt.Errorf("%w: requested_group_id %d differs from frozen requested group", ErrModelPricingUnavailable, m.RequestedGroupID)
		}
		return nil
	}
	if apiKey == nil || apiKey.GroupID == nil || m.ServedGroupID != *apiKey.GroupID {
		return fmt.Errorf("%w: served_group_id differs from the API key group", ErrModelPricingUnavailable)
	}
	return nil
}

// applyRoutingUsageLog 纯写入 UsageLog，每字段都取新地址，调用方必须先 validate。
// nil 保持历史 NULL。
func (m *RoutingUsageMetadata) apply(usageLog *UsageLog) {
	if m == nil || usageLog == nil {
		return
	}
	requested := m.RequestedGroupID
	served := m.ServedGroupID
	failover := m.Failover
	attempts := m.AttemptCount
	usageLog.RequestedGroupID = &requested
	usageLog.ServedGroupID = &served
	usageLog.Failover = &failover
	usageLog.AttemptCount = &attempts
}
