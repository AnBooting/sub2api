//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newLiveBillingTestService 构造一个只用于 Live 结算测试的最小 service。
func newLiveBillingTestService(usageRepo *liveTestUsageRepo) (*OpenAIGatewayService, *liveTestStore, *liveTestConcurrencyCache) {
	store := &liveTestStore{}
	concurrencyCache := &liveTestConcurrencyCache{}
	svc := &OpenAIGatewayService{
		cache:              store,
		concurrencyService: NewConcurrencyService(concurrencyCache),
		usageLogRepo:       usageRepo,
		billingService:     NewBillingService(rawChatCompletionsTestConfig(), nil),
	}
	return svc, store, concurrencyCache
}

// TestFinalizeLiveCallBillsByDuration 验证按时长计费：
// 单价 0.10/分钟、会话 2 分钟 → TotalCost=0.20，且 DurationMs 如实落库。
func TestFinalizeLiveCallBillsByDuration(t *testing.T) {
	price := 0.10
	usageRepo := &liveTestUsageRepo{}
	svc, store, _ := newLiveBillingTestService(usageRepo)
	record := &LiveCallRecord{
		CallID:              "call_billed",
		CallHash:            hashLiveCallID("call_billed"),
		AccountID:           1,
		APIKeyID:            2,
		UserID:              3,
		GroupID:             4,
		LeaseID:             "lease-billed",
		Model:               "gpt-live-test",
		CreatedAt:           time.Now().Add(-2 * time.Minute),
		ExpiresAt:           time.Now().Add(time.Hour),
		Controller:          LiveControllerPending,
		RealtimePricePerMin: &price,
	}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))

	svc.finalizeLiveCall(record)

	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 1)
	log := usageRepo.logs[0]
	require.Equal(t, RequestTypeLive, log.RequestType)
	// 允许 2 分钟附近 ±5 秒的计时抖动。
	require.InDelta(t, 0.20, log.TotalCost, 0.02)
	require.InDelta(t, 0.20, log.ActualCost, 0.02)
	require.NotNil(t, log.BillingMode)
	require.Equal(t, "per_request", *log.BillingMode)
}

// TestFinalizeLiveCallWithoutPriceWritesNoCost 锁定"缺价不得放行"：
// 分组未配置单价时，不得按内置默认价 0.05 静默计费，账单必须是 0 且不带计费模式。
func TestFinalizeLiveCallWithoutPriceWritesNoCost(t *testing.T) {
	usageRepo := &liveTestUsageRepo{}
	svc, store, _ := newLiveBillingTestService(usageRepo)
	record := &LiveCallRecord{
		CallID:     "call_unpriced",
		CallHash:   hashLiveCallID("call_unpriced"),
		AccountID:  1,
		APIKeyID:   2,
		UserID:     3,
		GroupID:    4,
		LeaseID:    "lease-unpriced",
		Model:      "gpt-live-test",
		CreatedAt:  time.Now().Add(-2 * time.Minute),
		ExpiresAt:  time.Now().Add(time.Hour),
		Controller: LiveControllerPending,
	}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))

	svc.finalizeLiveCall(record)

	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 1)
	log := usageRepo.logs[0]
	require.Zero(t, log.TotalCost)
	require.Zero(t, log.ActualCost)
	require.Nil(t, log.BillingMode)
}

// TestCalculateLiveSessionCostRejectsMissingPrice 直接锁定计费函数本身：
// nil 单价、0 单价、0 时长都必须返回错误，而不是算出 0 元。
func TestCalculateLiveSessionCostRejectsMissingPrice(t *testing.T) {
	billing := NewBillingService(rawChatCompletionsTestConfig(), nil)
	zero := 0.0
	price := 0.5

	_, err := billing.CalculateLiveSessionCost(60_000, nil, 1)
	require.ErrorIs(t, err, ErrLivePricingUnavailable)

	_, err = billing.CalculateLiveSessionCost(60_000, &zero, 1)
	require.ErrorIs(t, err, ErrLivePricingUnavailable)

	_, err = billing.CalculateLiveSessionCost(0, &price, 1)
	require.ErrorIs(t, err, ErrLivePricingUnavailable)

	cost, err := billing.CalculateLiveSessionCost(60_000, &price, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.5, cost.TotalCost, 1e-9)
	require.InDelta(t, 0.5, cost.ActualCost, 1e-9)
	require.Equal(t, "per_minute", cost.Unit)
}
