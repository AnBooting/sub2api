package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

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
