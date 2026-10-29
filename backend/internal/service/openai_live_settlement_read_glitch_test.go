//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// liveLoopReadGlitchStore 只做一件事：按次数注入 GetLiveCall 的故障，其余读写
// 全部透传给真实的测试 store。用于锁定「重试循环内的瞬时读失败」这一条路径。
type liveLoopReadGlitchStore struct {
	*liveTestStore
	reads int
	// failAt > 0：第 failAt 次读取返回一次瞬时错误（之后恢复）。
	failAt int
	// failAlways：每次读取都返回瞬时错误（模拟 Redis 持续抖动）。
	failAlways bool
	// notFoundFrom > 0：从第 notFoundFrom 次读取起一律返回 ErrLiveCallNotFound
	// （模拟记录已被关闭 TTL 清理）。
	notFoundFrom int
}

func (s *liveLoopReadGlitchStore) GetLiveCall(ctx context.Context, key string) (*LiveCallRecord, error) {
	s.reads++
	if s.notFoundFrom > 0 && s.reads >= s.notFoundFrom {
		return nil, ErrLiveCallNotFound
	}
	if s.failAlways {
		return nil, errors.New("transient Redis read failure")
	}
	if s.failAt > 0 && s.reads == s.failAt {
		return nil, errors.New("transient Redis read failure")
	}
	return s.liveTestStore.GetLiveCall(ctx, key)
}

// pendingSettlementRecord 造一条「首次结算已失败、冻结检查点已落库」的记录。
// 返回的记录副本带着 pending 的 Settlement，正是 observer 交给重试链的入参。
func pendingSettlementRecord(t *testing.T, svc *OpenAIGatewayService, store *liveTestStore, billing *openAIRecordUsageBillingRepoStub) *LiveCallRecord {
	t.Helper()
	price := 0.1
	record := newLiveSettlementRecord(t, store, 0, &price)
	billing.err = errors.New("temporary ledger outage")
	svc.finalizeLiveCall(record)
	require.Equal(t, 1, billing.calls, "首次 finalize 应尝试一次扣费")
	persisted, err := store.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotNil(t, persisted.Settlement)
	require.Equal(t, LiveSettlementPending, persisted.Settlement.Status)
	return persisted
}

// TestLiveSettlementRetryLoopSurvivesOneTransientRead 锁定缺陷本体：重试循环**开头**
// 的一次瞬时 Redis 读失败不得丢掉剩余重试预算，也不得丢掉已冻结的金额。
//
// 修复前该用例红：读失败直接 return，扣费次数停在 1、状态停在 pending。
func TestLiveSettlementRetryLoopSurvivesOneTransientRead(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	svc, store, _, billing := newLiveSettlementTestService()
	persisted := pendingSettlementRecord(t, svc, store, billing)
	frozenAt := persisted.Settlement.FrozenAt
	frozenCost := persisted.Settlement.TotalCost
	require.Positive(t, frozenCost)

	billing.err = nil
	// 第 1 次读取是入口回读（成功），第 2 次是重试循环开头的那次回读（瞬时失败）。
	glitch := &liveLoopReadGlitchStore{liveTestStore: store, failAt: 2}
	svc.cache = glitch

	svc.retryPendingLiveSettlement(persisted)

	require.Equal(t, 2, billing.calls,
		"循环内一次瞬时读失败不得丢弃剩余重试预算")
	settled, err := store.GetLiveCall(context.Background(), persisted.CallHash)
	require.NoError(t, err)
	require.NotNil(t, settled.Settlement)
	require.Equal(t, LiveSettlementSettled, settled.Settlement.Status)
	require.Equal(t, frozenAt, settled.Settlement.FrozenAt,
		"重试必须复用首次冻结，不得在故障后的重试时刻重新冻结金额")
	require.Equal(t, frozenCost, settled.Settlement.TotalCost,
		"重试必须复用首次冻结的金额")
}

// TestLiveSettlementRetryBudgetStaysBoundedWhenReadsKeepFailing 锁定「有界」：
// 读一直失败时重试次数仍受 liveSettlementRetryAttempts 封顶，不引入无限重试，
// 同时冻结金额原样留在 pending 检查点里等下一轮/下次进程接手。
func TestLiveSettlementRetryBudgetStaysBoundedWhenReadsKeepFailing(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	svc, store, _, billing := newLiveSettlementTestService()
	persisted := pendingSettlementRecord(t, svc, store, billing)
	frozenCost := persisted.Settlement.TotalCost

	billing.err = nil
	glitch := &liveLoopReadGlitchStore{liveTestStore: store, failAlways: true}
	svc.cache = glitch

	svc.retryPendingLiveSettlement(persisted)

	// 读不到记录就无法 finalize：不允许凭空扣费，也不允许无限循环。
	require.Equal(t, 1, billing.calls, "记录读不到时不得结算")
	// 入口与循环各自按 liveSettlementRetryAttempts 封顶，总计有界：
	//   入口 1 次首发读 + 入口退避重试 N 次；若入口全部失败则不会进循环。
	// 这里 Redis 永久故障，入口退避耗尽后因内存有冻结而继续，循环再烧 N 次。
	require.Equal(t, 1+2*liveSettlementRetryAttempts, glitch.reads,
		"读失败必须有界（入口 1+N 次 + 循环 N 次，N=%d）", liveSettlementRetryAttempts)
	current, err := store.GetLiveCall(context.Background(), persisted.CallHash)
	require.NoError(t, err)
	require.NotNil(t, current.Settlement)
	require.Equal(t, LiveSettlementPending, current.Settlement.Status)
	require.Equal(t, frozenCost, current.Settlement.TotalCost, "冻结金额必须原样保留")
}

// TestLiveSettlementRetryExitsWhenRecordIsGone 锁定退出判据的另一半：只有明确的
// 「记录不存在」才真正退出重试链，不必把预算烧完。
func TestLiveSettlementRetryExitsWhenRecordIsGone(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	svc, store, _, billing := newLiveSettlementTestService()
	persisted := pendingSettlementRecord(t, svc, store, billing)

	billing.err = nil
	// 入口回读仍能读到记录，之后记录被 TTL 清理：循环第一次回读即 ErrLiveCallNotFound。
	glitch := &liveLoopReadGlitchStore{liveTestStore: store, notFoundFrom: 2}
	svc.cache = glitch

	svc.retryPendingLiveSettlement(persisted)

	require.Equal(t, 1, billing.calls, "记录已不存在时不得结算")
	require.Equal(t, 2, glitch.reads,
		"明确记录不存在必须立即退出，不得把剩余预算烧在必然失败的读上")
}

// liveNilThenOKStore 第一次返回 (nil, nil)（非法但可能来自未来的 store 实现），
// 之后透传真实 store。用于锁定「循环内 latest 为 nil 不得 panic」。
type liveNilThenOKStore struct {
	*liveTestStore
	reads int
	nilAt int
}

func (s *liveNilThenOKStore) GetLiveCall(ctx context.Context, key string) (*LiveCallRecord, error) {
	s.reads++
	if s.nilAt > 0 && s.reads == s.nilAt {
		return nil, nil
	}
	return s.liveTestStore.GetLiveCall(ctx, key)
}

// TestLiveSettlementRetryDoesNotPanicOnNilRecord store 返回 (nil, nil) 时不得 panic。
//
// 入口对 latest 有 nil 守卫，循环里必须同样处理——否则任何返回 (nil, nil) 的
// store 实现都会让重试链空指针崩溃。真实 gatewayCache 不会这样返回，但这是同一
// 函数内两处判空不对称留下的隐患。
func TestLiveSettlementRetryDoesNotPanicOnNilRecord(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	svc, store, _, billing := newLiveSettlementTestService()
	record := pendingSettlementRecord(t, svc, store, billing)
	glitch := &liveNilThenOKStore{liveTestStore: store, nilAt: 2}
	svc.cache = glitch

	require.NotPanics(t, func() { svc.retryPendingLiveSettlement(record) },
		"store 返回 (nil, nil) 时重试链不得 panic")
}

// TestLiveSettlementRetryKeepsBudgetWhenEntryReadFailsWithoutFrozen 入口回读失败、
// 调用方副本**不带** Settlement 时，仍必须靠这次回读的结果继续，而不是直接放弃。
//
// 这条锁定 R3：三处容错点（入口 / 循环头 / 循环尾）必须一致。修复前入口
// 「frozen == nil 就 return」会让一次瞬时读失败直接丢掉全部剩余预算。
// 注意 record 必须不带 Settlement —— 否则入口的 frozen 一开始就非空，
// 无论入口怎么改都能通过（红队实测：那样测不到 R3）。
func TestLiveSettlementRetryKeepsBudgetWhenEntryReadFailsWithoutFrozen(t *testing.T) {
	previous := liveSettlementRetryInterval
	liveSettlementRetryInterval = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { liveSettlementRetryInterval = previous })

	svc, store, _, billing := newLiveSettlementTestService()
	persisted := pendingSettlementRecord(t, svc, store, billing)
	// 账本已恢复：只让入口回读失败一次，之后一切都正常。
	billing.err = nil
	// 调用方副本**不带** Settlement：模拟「手上这份记录还没带上刚冻结的检查点」。
	bare := &LiveCallRecord{CallHash: persisted.CallHash, UserID: persisted.UserID,
		APIKeyID: persisted.APIKeyID, AccountID: persisted.AccountID, Model: persisted.Model}
	glitch := &liveLoopReadGlitchStore{liveTestStore: store, failAt: 1}
	svc.cache = glitch

	svc.retryPendingLiveSettlement(bare)

	current, err := store.GetLiveCall(context.Background(), persisted.CallHash)
	require.NoError(t, err)
	require.NotNil(t, current.Settlement)
	require.Equal(t, LiveSettlementSettled, current.Settlement.Status,
		"入口回读的一次瞬时失败不得让结算永远停在 pending")
}
