package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 结算状态持久化不能把已关闭的会话重新变回可控制状态。
func TestLiveSettlementPersistKeepsClosedMarker(t *testing.T) {
	price := 0.10
	svc, store, _, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)

	svc.finalizeLiveCall(record)

	stored, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveControllerClosed, stored.Controller)
	require.Empty(t, stored.ControllerOwner)
}

// 财务仓储短时故障后，observer 的自动重试应把 pending 会话结算掉。
func TestRetryPendingLiveSettlementRecoversAfterTransientFailure(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	price := 0.10
	svc, store, _, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)

	billingRepo.err = errors.New("temporary database outage")
	svc.finalizeLiveCall(record)
	pending, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementPending, pending.Settlement.Status)

	billingRepo.err = nil
	svc.retryPendingLiveSettlement(pending)

	settled, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, settled.Settlement.Status)
	require.Equal(t, 2, billingRepo.calls)
}

// 简易模式与请求路径一致：不扣余额、不消耗订阅，只记录用量。
func TestLiveSettlementSimpleModeDoesNotDebitFinancialAccounts(t *testing.T) {
	for _, subscriptionID := range []int64{0, 9} {
		name := map[bool]string{true: "subscription", false: "balance"}[subscriptionID > 0]
		t.Run(name, func(t *testing.T) {
			price := 0.10
			svc, store, usageRepo, billingRepo := newLiveSettlementTestService()
			svc.cfg = &config.Config{RunMode: config.RunModeSimple}
			record := newLiveSettlementRecord(t, store, subscriptionID, &price)

			svc.finalizeLiveCall(record)

			require.Zero(t, billingRepo.calls)
			usageRepo.mu.Lock()
			defer usageRepo.mu.Unlock()
			require.Len(t, usageRepo.logs, 1)
			require.Zero(t, usageRepo.logs[0].ActualCost)
		})
	}
}

// 简易模式 + 密钥窗口限额：只累计密钥窗口用量，不能落到余额/订阅。
func TestLiveSettlementSimpleModeOnlyTracksKeyWindows(t *testing.T) {
	price := 0.10
	svc, store, _, billingRepo := newLiveSettlementTestService()
	svc.cfg = &config.Config{RunMode: config.RunModeSimple, SimpleModeKeyRateLimitEnabled: true}
	record := newLiveSettlementRecord(t, store, 0, &price)

	svc.finalizeLiveCall(record)

	require.Equal(t, 1, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd)
	require.Positive(t, billingRepo.lastCmd.APIKeyRateLimitCost)
	require.Zero(t, billingRepo.lastCmd.BalanceCost)
	require.Zero(t, billingRepo.lastCmd.SubscriptionCost)
}

// 失败后重试必须复用首次冻结的金额，而不是按再次结算时的时长重新计价。
func TestLiveSettlementRetryKeepsFrozenAmount(t *testing.T) {
	price := 0.10
	svc, store, _, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)

	billingRepo.err = errors.New("temporary database outage")
	svc.finalizeLiveCall(record)

	stored, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, stored.Settlement)
	require.Equal(t, LiveSettlementPending, stored.Settlement.Status)
	require.InDelta(t, 0.2, stored.Settlement.ActualCost, 0.02)

	// 模拟会话结束后又过了 8 分钟才重试。
	stored.CreatedAt = time.Now().Add(-10 * time.Minute)
	require.NoError(t, store.SaveLiveCall(context.Background(), stored, time.Hour))

	billingRepo.err = nil
	svc.finalizeLiveCall(stored)

	require.Equal(t, 2, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd)
	require.InDelta(t, 0.2, billingRepo.lastCmd.BalanceCost, 0.02)

	settled, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, settled.Settlement.Status)
	require.Equal(t, 1, settled.Settlement.Attempts)
}

// 结算成功后再次 finalize 不得重复扣费。
func TestLiveSettlementSettledRecordIsNotChargedAgain(t *testing.T) {
	price := 0.10
	svc, store, _, billingRepo := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)

	svc.finalizeLiveCall(record)
	svc.finalizeLiveCall(record)

	require.Equal(t, 1, billingRepo.calls)
}
