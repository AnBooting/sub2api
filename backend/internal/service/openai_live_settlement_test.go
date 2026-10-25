//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newLiveSettlementTestService wires the same dependencies production uses for
// Live settlement: the unified billing repo plus balance/subscription repos.
func newLiveSettlementTestService() (*OpenAIGatewayService, *liveTestStore, *liveTestUsageRepo, *openAIRecordUsageBillingRepoStub) {
	store := &liveTestStore{}
	usageRepo := &liveTestUsageRepo{}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	svc := &OpenAIGatewayService{
		cache:              store,
		concurrencyService: NewConcurrencyService(&liveTestConcurrencyCache{}),
		usageLogRepo:       usageRepo,
		usageBillingRepo:   billingRepo,
		userRepo:           &openAIRecordUsageUserRepoStub{},
		userSubRepo:        &openAIRecordUsageSubRepoStub{},
		// 生产上密钥额度/窗口限额由 APIKeyService 更新并失效鉴权缓存；
		// 统一账本路径只把它当作启用开关与缓存失效入口。
		apiKeyService:  &APIKeyService{},
		billingService: NewBillingService(rawChatCompletionsTestConfig(), nil),
	}
	return svc, store, usageRepo, billingRepo
}

func newLiveSettlementRecord(t *testing.T, store *liveTestStore, subscriptionID int64, price *float64) *LiveCallRecord {
	t.Helper()
	record := &LiveCallRecord{
		CallID:              "call_settlement",
		CallHash:            hashLiveCallID("call_settlement"),
		AccountID:           1,
		APIKeyID:            2,
		UserID:              3,
		GroupID:             4,
		SubscriptionID:      subscriptionID,
		LeaseID:             "lease-settlement",
		Model:               "gpt-live-test",
		CreatedAt:           time.Now().Add(-2 * time.Minute),
		ExpiresAt:           time.Now().Add(time.Hour),
		Controller:          LiveControllerPending,
		RealtimePricePerMin: price,
		// 模拟开播时冻结的计费策略：密钥有总额度且配置了 5h/1d/7d 窗口限额。
		BillingPolicy: &LiveBillingPolicy{
			APIKeyQuota:   1,
			RateLimit5h:   1,
			RateLimit1d:   1,
			RateLimit7d:   1,
			GroupPlatform: "openai",
		},
	}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	return record
}

// TestFinalizeLiveCallSettlesBalanceUsage 锁定余额模式：账单金额必须对应一次
// 真实的幂等扣费，且 request_id 与 usage_logs 一致。
func TestFinalizeLiveCallSettlesBalanceUsage(t *testing.T) {
	price := 0.10
	svc, store, usageRepo, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)

	svc.finalizeLiveCall(record)

	require.Equal(t, 1, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd)
	require.Equal(t, record.CallHash, billingRepo.lastCmd.RequestID)
	require.InDelta(t, 0.2, billingRepo.lastCmd.BalanceCost, 0.02)
	require.Zero(t, billingRepo.lastCmd.SubscriptionCost)
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 1)
	require.InDelta(t, 0.2, usageRepo.logs[0].ActualCost, 0.02)
}

// TestFinalizeLiveCallSettlesSubscriptionUsage 锁定订阅模式：费用计入订阅额度，
// 不能误扣余额。
func TestFinalizeLiveCallSettlesSubscriptionUsage(t *testing.T) {
	price := 0.10
	svc, store, usageRepo, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 9, &price)

	svc.finalizeLiveCall(record)

	require.Equal(t, 1, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd.SubscriptionID)
	require.Equal(t, int64(9), *billingRepo.lastCmd.SubscriptionID)
	require.InDelta(t, 0.2, billingRepo.lastCmd.SubscriptionCost, 0.02)
	require.Zero(t, billingRepo.lastCmd.BalanceCost)
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.InDelta(t, 0.2, usageRepo.logs[0].ActualCost, 0.02)
}

// TestFinalizeLiveCallBillingFailureWritesNoCharge 锁定失败处理：结算失败时不得
// 留下"已计费但未扣费"的账单。
//
// 对账责任由**结算记录本身**（Settlement 保持 pending + 冻结金额）与 ops 审计承担，
// 不再往 usage_logs 写零费用占位行：usage_logs 是请求数、目录费用、账号成本与会话
// 时长的单一事实源，汇总 SQL 直接 SUM，多一条行会把一次会话重复统计。
func TestFinalizeLiveCallBillingFailureWritesNoCharge(t *testing.T) {
	price := 0.10
	svc, store, usageRepo, billingRepo := newLiveSettlementTestService()
	billingRepo.err = errors.New("billing unavailable")
	record := newLiveSettlementRecord(t, store, 0, &price)

	require.NotPanics(t, func() { svc.finalizeLiveCall(record) })

	require.Equal(t, 1, billingRepo.calls)
	// 待结算状态与冻结金额保留在结算记录里，重试与对账都以它为准。
	current, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, current.Settlement)
	require.Equal(t, LiveSettlementPending, current.Settlement.Status)
	require.InDelta(t, 0.2, current.Settlement.TotalCost, 0.02)
	require.Equal(t, 1, current.Settlement.Attempts)
	// 失败的尝试不得在 usage_logs 留下任何行。
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 0, "失败尝试不得写 usage_logs")
}

// TestFinalizeLiveCallWithoutPriceSkipsSettlement 锁定缺价不得放行：没有冻结单价时
// 既不结算也不伪造零价扣费。
func TestFinalizeLiveCallWithoutPriceSkipsSettlement(t *testing.T) {
	svc, store, usageRepo, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, nil)

	svc.finalizeLiveCall(record)

	require.Zero(t, billingRepo.calls)
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 1)
	require.Zero(t, usageRepo.logs[0].ActualCost)
}

// TestFinalizeLiveCallSettlesAtMostOnce 锁定幂等：重复 finalize（或控制器重试）
// 不得重复扣费。
func TestFinalizeLiveCallSettlesAtMostOnce(t *testing.T) {
	price := 0.10
	svc, store, usageRepo, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)

	svc.finalizeLiveCall(record)
	svc.finalizeLiveCall(record)

	require.Equal(t, 1, billingRepo.calls)
	usageRepo.mu.Lock()
	defer usageRepo.mu.Unlock()
	require.Len(t, usageRepo.logs, 1)
}
