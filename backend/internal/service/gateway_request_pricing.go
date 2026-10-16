package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type gatewayRequestBillingCtxKey struct{}

// gatewayRequestBillingResetCtxKey 标记"忽略此前链上的冻结 bundle"。context
// 无法删除已写入的值，长连接按 turn 强制重冻时必须显式重置，否则旧的 bundle
// 会继续命中 prepareRequestBilling 的幂等复用分支。
type gatewayRequestBillingResetCtxKey struct{}

func gatewayRequestBillingReset(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	reset, _ := ctx.Value(gatewayRequestBillingResetCtxKey{}).(bool)
	return reset
}

// withoutGatewayRequestBilling 返回逻辑上不携带冻结 bundle 的 ctx，仅供强制
// 重冻入口使用；HTTP 单请求的单次冻结仍走 PrepareRequestBilling 的幂等语义。
func withoutGatewayRequestBilling(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gatewayRequestBillingResetCtxKey{}, true)
}

// WithoutGatewayRequestBilling 供长连接在"本 turn 不进锁价"时显式剥离上层冻结
// bundle（WS 中途出现的显式生图回合按设计回退 legacy 按次计费）。剥离后
// GatewayRequestBillingFromContext 返回 nil，用量落账走 legacy 分支，既不会沿用
// 连接级文本锁价，也不会因缺少 token 价被整笔拒绝。
func WithoutGatewayRequestBilling(ctx context.Context) context.Context {
	return withoutGatewayRequestBilling(ctx)
}

// GatewayRequestBillingFromContext returns the opaque immutable request bundle.
func GatewayRequestBillingFromContext(ctx context.Context) *RequestBillingContext {
	if ctx == nil {
		return nil
	}
	if gatewayRequestBillingReset(ctx) {
		return nil
	}
	request, _ := ctx.Value(gatewayRequestBillingCtxKey{}).(*RequestBillingContext)
	return request
}

func withGatewayRequestBilling(ctx context.Context, request *RequestBillingContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	// 新 bundle 生效：清除重置标记，并把上层 attempt 的账号执行事实置空。冻结
	// 后的账号事实必须由调用方按新 bundle 重新绑定，不能让旧 attempt 的事实
	// 继续透过 context 链被读到。
	ctx = context.WithValue(ctx, gatewayRequestBillingResetCtxKey{}, false)
	ctx = context.WithValue(ctx, gatewayRequestBillingAccountCtxKey{}, gatewayRequestBillingAccount{})
	ctx = context.WithValue(ctx, gatewayRequestBillingCtxKey{}, request)
	ctx = context.WithValue(ctx, gatewayTokenRequestPricingAtCtxKey{}, request.StartedAt())
	if id := request.requestedGroupID; id != nil {
		if candidate := request.candidates[*id]; candidate != nil && candidate.group != nil {
			ctx = context.WithValue(ctx, gatewayTokenRequestBillingGroupCtxKey{}, cloneAPIKeyGroup(candidate.group))
		}
	}
	return ctx
}

type gatewayRequestBillingAccountCtxKey struct{}

// These are server-selected execution facts, never customer price overrides.
type gatewayRequestBillingAccount struct {
	groupID   int64
	accountID int64
}

// WithGatewayRequestBillingAccount binds the real selected account to the
// existing immutable bundle before any upstream write. Unknown facts fail closed.
func WithGatewayRequestBillingAccount(ctx context.Context, groupID, accountID int64) (context.Context, error) {
	request := GatewayRequestBillingFromContext(ctx)
	if request == nil || accountID <= 0 {
		return ctx, ErrModelPricingUnavailable
	}
	upstream, err := request.UpstreamModelFor(groupID, accountID)
	if err != nil {
		return ctx, err
	}
	if _, err = request.ScopeFor(groupID, upstream, ""); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, gatewayRequestBillingAccountCtxKey{}, gatewayRequestBillingAccount{groupID: groupID, accountID: accountID}), nil
}

func gatewayRequestBillingAccountFromContext(ctx context.Context) (gatewayRequestBillingAccount, bool) {
	if ctx == nil {
		return gatewayRequestBillingAccount{}, false
	}
	facts, ok := ctx.Value(gatewayRequestBillingAccountCtxKey{}).(gatewayRequestBillingAccount)
	if !ok || facts.accountID <= 0 {
		// 零值账户事实由 withGatewayRequestBilling 写入，用于遮蔽上层 attempt
		// 的旧事实；它在语义上等同"尚未绑定账号"。
		return gatewayRequestBillingAccount{}, false
	}
	return facts, true
}

func frozenGatewayRequestAccountModel(ctx context.Context, accountID int64) (string, bool, error) {
	request := GatewayRequestBillingFromContext(ctx)
	if request == nil {
		return "", false, nil
	}
	facts, ok := gatewayRequestBillingAccountFromContext(ctx)
	if !ok || facts.accountID != accountID {
		return "", true, fmt.Errorf("%w: selected account does not match frozen execution", ErrModelPricingUnavailable)
	}
	model, err := request.UpstreamModelFor(facts.groupID, accountID)
	return model, true, err
}

// TransferGatewayRequestPricingContext preserves immutable values on a worker
// context without inheriting the client's cancellation or discarding the worker
// deadline. Parent is normally context.WithoutCancel(requestContext).
func TransferGatewayRequestPricingContext(base, parent context.Context) context.Context {
	if base == nil {
		base = context.Background()
	}
	if request := GatewayRequestBillingFromContext(parent); request != nil {
		base = withGatewayRequestBilling(base, request)
	}
	if facts, ok := gatewayRequestBillingAccountFromContext(parent); ok {
		base = context.WithValue(base, gatewayRequestBillingAccountCtxKey{}, facts)
	}
	return base
}

type gatewayTokenRequestPricingAtCtxKey struct{}
type gatewayTokenRequestBillingGroupCtxKey struct{}
type requestPricingSnapshotCtxKey struct{}

// RequestPricingSnapshot holds immutable calendar rules captured at admission.
// It does not claim to freeze unrelated catalogue updates or group settings.
type RequestPricingSnapshot struct {
	deepseekCalendar *deepseekHolidayCalendar
}

func RequestPricingSnapshotFromContext(ctx context.Context) *RequestPricingSnapshot {
	if ctx == nil {
		return nil
	}
	snapshot, _ := ctx.Value(requestPricingSnapshotCtxKey{}).(*RequestPricingSnapshot)
	return snapshot
}

func WithRequestPricingSnapshot(ctx context.Context, snapshot *RequestPricingSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot == nil {
		return ctx
	}
	return context.WithValue(ctx, requestPricingSnapshotCtxKey{}, snapshot)
}

func captureRequestPricingSnapshot(ctx context.Context, billing *BillingService) context.Context {
	if request := GatewayRequestBillingFromContext(ctx); request != nil {
		billing = request.billing
	}
	var pricing *PricingService
	if billing != nil {
		pricing = billing.pricingService
	}
	return WithRequestPricingSnapshot(ctx, &RequestPricingSnapshot{deepseekCalendar: pricing.deepseekCalendar()})
}

func requestDeepseekCalendar(ctx context.Context, pricing *PricingService, at time.Time) *deepseekHolidayCalendar {
	if snapshot := RequestPricingSnapshotFromContext(ctx); snapshot != nil {
		return effectiveDeepseekCalendar(snapshot.deepseekCalendar, at.In(time.FixedZone("Asia/Shanghai", 8*3600)).Year())
	}
	return pricing.deepseekCalendarAt(at)
}

func (s *GatewayService) WithGatewayTokenRequestPricing(ctx context.Context) (context.Context, time.Time) {
	ctx, at := WithGatewayTokenRequestPricing(ctx)
	var billing *BillingService
	if s != nil {
		billing = s.billingService
	}
	return captureRequestPricingSnapshot(ctx, billing), at
}

func requestPricingAt(ctx context.Context, explicit time.Time) time.Time {
	if !explicit.IsZero() {
		return explicit
	}
	if at := GatewayTokenRequestPricingAtFromContext(ctx); !at.IsZero() {
		return at
	}
	if ctx != nil {
		if at, ok := openAIPricingAtFromContext(ctx); ok {
			return at
		}
	}
	return timezone.Now()
}

// WithGatewayTokenRequestPricing marks a shared-gateway request as token billed
// and freezes the downstream pricing instant for its whole lifetime. Media and
// metadata-only handlers deliberately do not call this helper.
func WithGatewayTokenRequestPricing(ctx context.Context) (context.Context, time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	if request := GatewayRequestBillingFromContext(ctx); request != nil {
		return ctx, request.StartedAt()
	}
	pricingAt := timezone.Now()
	ctx = context.WithValue(ctx, gatewayTokenRequestPricingAtCtxKey{}, pricingAt)
	// 调度过程中可能因 fallback/composite 路由覆盖 ctxkey.Group；计费 D 仍必须
	// 使用认证时刻的父分组，和最终 RecordUsage 的计费归属保持一致。
	if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(group) {
		ctx = context.WithValue(ctx, gatewayTokenRequestBillingGroupCtxKey{}, group)
	}
	return ctx, pricingAt
}

func gatewayTokenRequestPricingAtFromContext(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	pricingAt, ok := ctx.Value(gatewayTokenRequestPricingAtCtxKey{}).(time.Time)
	return pricingAt, ok && !pricingAt.IsZero()
}

// GatewayTokenRequestPricingAtFromContext exposes the frozen instant to
// handlers before they detach asynchronous usage-recording work.
func GatewayTokenRequestPricingAtFromContext(ctx context.Context) time.Time {
	pricingAt, _ := gatewayTokenRequestPricingAtFromContext(ctx)
	return pricingAt
}

func gatewayTokenRequestBillingGroupFromContext(ctx context.Context) *Group {
	if ctx == nil {
		return nil
	}
	group, _ := ctx.Value(gatewayTokenRequestBillingGroupCtxKey{}).(*Group)
	if IsGroupContextValid(group) {
		return group
	}
	return nil
}
