package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type gatewayRequestBillingCtxKey struct{}

// GatewayRequestBillingFromContext returns the opaque immutable request bundle.
func GatewayRequestBillingFromContext(ctx context.Context) *RequestBillingContext {
	if ctx == nil {
		return nil
	}
	request, _ := ctx.Value(gatewayRequestBillingCtxKey{}).(*RequestBillingContext)
	return request
}

func withGatewayRequestBilling(ctx context.Context, request *RequestBillingContext) context.Context {
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
	return facts, ok
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
