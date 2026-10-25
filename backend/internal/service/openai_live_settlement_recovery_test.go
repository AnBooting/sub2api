//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// liveUsageLogFailingRepo 让用量行写入失败一次（其余成功），用于验证
// "金额没落库就不置 UsageLogWritten"：置早了这次结算不会再补写，余额扣了钱、
// 用户账单却永远是 0。
type liveUsageLogFailingRepo struct {
	UsageLogRepository
	mu       sync.Mutex
	failNext bool
	rows     map[string]UsageLog
}

func (r *liveUsageLogFailingRepo) Create(_ context.Context, log *UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext {
		r.failNext = false
		return false, errors.New("usage log store unavailable")
	}
	if r.rows == nil {
		r.rows = map[string]UsageLog{}
	}
	if _, exists := r.rows[log.RequestID]; exists {
		return false, nil
	}
	r.rows[log.RequestID] = *log
	return true, nil
}

// liveFirstInsertUsageRepo 只实现 Create，模拟生产 usage_logs 的唯一键契约：
// INSERT ... ON CONFLICT (request_id, api_key_id) DO NOTHING（重复写入被忽略）。
type liveFirstInsertUsageRepo struct {
	UsageLogRepository
	mu   sync.Mutex
	rows map[string]UsageLog
}

func (r *liveFirstInsertUsageRepo) Create(_ context.Context, log *UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rows == nil {
		r.rows = map[string]UsageLog{}
	}
	if _, found := r.rows[log.RequestID]; found {
		return false, nil
	}
	r.rows[log.RequestID] = *log
	return true, nil
}

// liveCheckpointFailStore 让 SaveLiveCall 失败（模拟 Redis 写冻结检查点失败）。
type liveCheckpointFailStore struct {
	*liveTestStore
	failSave bool
}

func (s *liveCheckpointFailStore) SaveLiveCall(ctx context.Context, record *LiveCallRecord, ttl time.Duration) error {
	if s.failSave {
		return errors.New("redis temporarily unavailable after close marker")
	}
	return s.liveTestStore.SaveLiveCall(ctx, record, ttl)
}

// TestLiveSettlementUsageLogWrittenOnlyAfterDurableAmount 金额行没落库时不能置
// UsageLogWritten：结算标记成"已写日志"之后就不会再补写，用户账单会永远停在 0。
func TestLiveSettlementUsageLogWrittenOnlyAfterDurableAmount(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	usage := &liveUsageLogFailingRepo{failNext: true}
	svc.usageLogRepo = usage
	record := newLiveSettlementRecord(t, store, 0, &price)

	svc.finalizeLiveCall(record)
	current, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, current.Settlement)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.Positive(t, billing.calls, "账本扣费已发生")
	require.False(t, current.Settlement.UsageLogWritten, "金额行没落库就不能置 UsageLogWritten")

	// 补写：金额行落库后才置位，且落的是真实金额。
	svc.finalizeLiveCall(record)
	current, err = store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.True(t, current.Settlement.UsageLogWritten, "金额行落库后必须置 UsageLogWritten")
	require.Positive(t, usage.rows[record.CallHash].ActualCost, "补写的金额行必须是真实金额")
}

// TestLiveSettlementRetryReconcilesZeroCostUsageRow 补扣成功后，正式费用行必须带着
// 真实金额落库：失败审计行（零费用）不能占住正式行的唯一键，否则余额扣了钱、用户
// 账单与汇总统计却永远是 0。
func TestLiveSettlementRetryReconcilesZeroCostUsageRow(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	usage := &liveFirstInsertUsageRepo{}
	svc.usageLogRepo = usage
	record := newLiveSettlementRecord(t, store, 0, &price)

	billing.err = errors.New("temporary billing outage")
	svc.finalizeLiveCall(record)
	billing.err = nil
	svc.finalizeLiveCall(record)

	current, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status)
	require.Equal(t, 2, billing.calls)
	require.Positive(t, billing.lastCmd.BalanceCost)
	require.Equal(t, billing.lastCmd.BalanceCost, usage.rows[record.CallHash].ActualCost,
		"补扣成功后正式费用行必须是真实金额")
	// 失败诊断不进 usage_logs：那张表是费用/时长/请求数的单一事实源，
	// 多一条行会让一次会话被重复统计。审计改走 ops 错误日志。
	_, hasPendingRow := usage.rows[record.CallHash+":pending"]
	require.False(t, hasPendingRow, "失败诊断不得写进 usage_logs")
	require.Len(t, usage.rows, 1, "一次会话在 usage_logs 里恰好一行")
}

// TestLiveSettlementCheckpointFailureDoesNotHideSettlement 关闭标记已落库、但冻结
// 检查点写失败时，后续结算不能被关闭标记永久遮住：那笔金额必须还能收上来。
func TestLiveSettlementCheckpointFailureDoesNotHideSettlement(t *testing.T) {
	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	failing := &liveCheckpointFailStore{liveTestStore: store, failSave: true}
	svc.cache = failing
	billing.err = errors.New("temporary billing outage")

	svc.finalizeLiveCall(record)
	require.Equal(t, 1, billing.calls)

	failing.failSave = false
	billing.err = nil
	// 从持久化存储重试（observer/重启后的路径）。
	persisted, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	svc.finalizeLiveCall(persisted)
	require.Equal(t, 2, billing.calls,
		"关闭标记已落库也不能让检查点失败的结算永远收不上来")
}

// TestLiveObserverFinalizationStartsPendingSettlementRetry observer 自己结束会话
// （到期/会话结束）时若结算停在 pending，必须进入自动重试——否则 observer 退出后
// 没有人再收这笔钱，记录只能等 TTL 过期被清掉。
func TestLiveObserverFinalizationStartsPendingSettlementRetry(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	price := 0.1
	svc, store, _, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	record.ExpiresAt = time.Now().Add(-time.Second)
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	billing.err = errors.New("temporary billing outage")

	svc.observeLiveCall(record)

	persisted, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, persisted.Settlement)
	require.Equal(t, LiveSettlementPending, persisted.Settlement.Status)
	require.GreaterOrEqual(t, billing.calls, 2,
		"observer 自己收尾留下 pending 时必须进入自动重试")
}

// TestLiveSettlementRetryReusesFrozenDurationAfterCheckpointFailure 规则 A：同一
// 进程内的重试链必须沿用**首次冻结**的时长/金额，不能按重试时刻重新冻结——否则用户
// 要为"Redis 故障的那几分钟"继续付费。
func TestLiveSettlementRetryReusesFrozenDurationAfterCheckpointFailure(t *testing.T) {
	price := 0.1
	svc, store, usage, billing := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	failing := &liveCheckpointFailStore{liveTestStore: store, failSave: true}
	svc.cache = failing
	billing.err = errors.New("temporary billing outage")

	// 第一次：冻结 → 账本失败 → 冻结检查点写失败（存储里始终没有冻结记录）。
	svc.finalizeLiveCall(record)
	require.Equal(t, 1, billing.calls)
	require.NotNil(t, record.Settlement, "冻结结果必须回写给调用方，供同进程重试链沿用")
	firstDuration := record.Settlement.DurationMs
	require.Positive(t, firstDuration)

	// 稍后重试（同进程）：账本恢复、检查点可写。
	time.Sleep(20 * time.Millisecond)
	billing.err = nil
	failing.failSave = false
	svc.finalizeLiveCall(record)

	require.Equal(t, 2, billing.calls)
	require.NotEmpty(t, usage.logs)
	retryLog := usage.logs[len(usage.logs)-1]
	require.NotNil(t, retryLog.DurationMs)
	require.Equal(t, firstDuration, *retryLog.DurationMs,
		"同进程重试链必须沿用首次冻结的时长，不能按重试时刻重算")
	require.LessOrEqual(t, *retryLog.DurationMs, svc.liveSettlementDurationCapMs(record),
		"重试结算的时长不得超过会话上限")
}

// TestLiveSettlementRefreezeDurationIsCappedToSessionLimit 规则 B：只能从存储重新
// 冻结时（跨进程），时长必须封顶到**会话自身的上限**（ExpiresAt - CreatedAt）——那是
// 会话开始时记下的历史事实，不会因为后来把配置调小而被追溯性地削掉已发生的用量时长。
// 故障再久，用户也只为整个会话上限付费。
func TestLiveSettlementRefreezeDurationIsCappedToSessionLimit(t *testing.T) {
	price := 0.1
	svc, store, usage, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	// 会话早已到期：now - CreatedAt 远大于会话上限（模拟跨进程只能从存储重新冻结）。
	record.CreatedAt = time.Now().Add(-4 * time.Hour)
	record.ExpiresAt = record.CreatedAt.Add(time.Hour)
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))

	svc.finalizeLiveCall(record)

	require.NotEmpty(t, usage.logs)
	log := usage.logs[len(usage.logs)-1]
	require.NotNil(t, log.DurationMs)
	sessionCap := svc.liveSettlementDurationCapMs(record)
	require.Equal(t, int(time.Hour/time.Millisecond), sessionCap, "上限锚在会话自己的 ExpiresAt 上")
	require.LessOrEqual(t, *log.DurationMs, sessionCap, "重新冻结的时长必须封顶到会话上限")
	require.Equal(t, sessionCap, *log.DurationMs, "封顶后按会话上限计费")
}

// TestLiveSettlementRefreezeDurationFallsBackWhenExpiresAtMissing 历史遗留记录没有
// ExpiresAt 时，上限回落到配置的最大会话时长（配置没配就是默认值）。
func TestLiveSettlementRefreezeDurationFallsBackWhenExpiresAtMissing(t *testing.T) {
	price := 0.1
	svc, store, usage, _ := newLiveSettlementTestService()
	record := newLiveSettlementRecord(t, store, 0, &price)
	record.CreatedAt = time.Now().Add(-4 * time.Hour)
	record.ExpiresAt = record.CreatedAt // 缺 ExpiresAt（历史遗留记录）
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))

	svc.finalizeLiveCall(record)

	require.NotEmpty(t, usage.logs)
	log := usage.logs[len(usage.logs)-1]
	require.NotNil(t, log.DurationMs)
	fallback := int(svc.liveMaxSessionDuration().Milliseconds())
	require.Equal(t, fallback, svc.liveSettlementDurationCapMs(record), "缺 ExpiresAt 时回落到配置的最大会话时长")
	require.LessOrEqual(t, *log.DurationMs, fallback, "时长必须封顶到回落上限")
}