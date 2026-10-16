//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRequestAttemptBudgetDefaults 固定 TS-02 契约初值：16 次发送 / 600 秒窗口。
func TestRequestAttemptBudgetDefaults(t *testing.T) {
	require.Equal(t, 16, DefaultRequestAttemptBudgetSends)
	require.Equal(t, 600*time.Second, DefaultRequestAttemptBudgetWindow)
}

// TestNewRequestAttemptBudgetDeadlineSelection 覆盖 deadline = min(start+window, clientDeadline)：
// 客户端更早优先、客户端更晚不生效、零值忽略、以及 now 零值兜底。
func TestNewRequestAttemptBudgetDeadlineSelection(t *testing.T) {
	now := time.Now()

	earlier := now.Add(30 * time.Second)
	byClient := NewRequestAttemptBudget(now, DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, earlier)
	require.Equal(t, earlier, byClient.Deadline(), "客户端更早的 deadline 必须优先")

	later := now.Add(2 * time.Hour)
	byWindow := NewRequestAttemptBudget(now, DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, later)
	require.Equal(t, now.Add(DefaultRequestAttemptBudgetWindow), byWindow.Deadline(), "客户端更晚时窗口 deadline 优先")

	zeroClient := NewRequestAttemptBudget(now, DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	require.Equal(t, now.Add(DefaultRequestAttemptBudgetWindow), zeroClient.Deadline(), "clientDeadline 零值必须忽略")

	zeroNow := NewRequestAttemptBudget(time.Time{}, DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	require.False(t, zeroNow.Deadline().IsZero(), "now 零值兜底后 deadline 不应是零值")
	require.Greater(t, zeroNow.Remaining(), time.Duration(0))
	require.NoError(t, zeroNow.AllowSend())
}

// TestRequestAttemptBudgetSendsCeiling 上限触顶：第 17 次 AllowSend 报错且 used 仍为 16
// （被拒的尝试不计数，attempt_count 只数真实发生的发送）。
func TestRequestAttemptBudgetSendsCeiling(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	for i := 0; i < DefaultRequestAttemptBudgetSends; i++ {
		require.NoError(t, budget.AllowSend(), "第 %d 次发送应放行", i+1)
	}
	require.Equal(t, DefaultRequestAttemptBudgetSends, budget.Used())
	require.True(t, budget.Exhausted(), "used>=maxSends 即次数耗尽，与剩余时间无关")

	err := budget.AllowSend()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRequestAttemptBudgetExhausted)
	require.NotErrorIs(t, err, ErrRequestAttemptBudgetDeadline)
	require.Contains(t, err.Error(), "used=16")
	require.Contains(t, err.Error(), "max=16")
	require.Equal(t, DefaultRequestAttemptBudgetSends, budget.Used(), "被拒的尝试不得虚增 used")
	require.True(t, budget.Exhausted())
}

// TestRequestAttemptBudgetWindowExhausted 窗口触顶：不真 sleep，用构造时即已过去的
// deadline（负 window / 过去的 clientDeadline）驱动时间耗尽分支。
func TestRequestAttemptBudgetWindowExhausted(t *testing.T) {
	now := time.Now()

	byNegativeWindow := NewRequestAttemptBudget(now, DefaultRequestAttemptBudgetSends, -time.Second, time.Time{})
	err := byNegativeWindow.AllowSend()
	require.ErrorIs(t, err, ErrRequestAttemptBudgetDeadline)
	require.NotErrorIs(t, err, ErrRequestAttemptBudgetExhausted)
	require.Contains(t, err.Error(), "deadline=")
	require.Equal(t, 0, byNegativeWindow.Used())
	require.True(t, byNegativeWindow.Exhausted())
	require.Equal(t, time.Duration(0), byNegativeWindow.Remaining())

	byPastClientDeadline := NewRequestAttemptBudget(now, DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, now.Add(-time.Second))
	err = byPastClientDeadline.AllowSend()
	require.ErrorIs(t, err, ErrRequestAttemptBudgetDeadline)
	require.Equal(t, 0, byPastClientDeadline.Used())
	require.True(t, byPastClientDeadline.Exhausted())
	require.Equal(t, time.Duration(0), byPastClientDeadline.Remaining())
}

// TestRequestAttemptBudgetRemainingAndExhausted 覆盖 Remaining/Exhausted/Deadline 语义：
// 次数用尽但时间仍有剩余时 Exhausted=true 而 Remaining>0；deadline 过后 Remaining 钳 0。
func TestRequestAttemptBudgetRemainingAndExhausted(t *testing.T) {
	now := time.Now()
	budget := NewRequestAttemptBudget(now, 2, time.Hour, time.Time{})

	require.Equal(t, now.Add(time.Hour), budget.Deadline())
	require.False(t, budget.Exhausted())
	require.Greater(t, budget.Remaining(), time.Duration(0))
	require.LessOrEqual(t, budget.Remaining(), time.Hour)

	require.NoError(t, budget.AllowSend())
	require.NoError(t, budget.AllowSend())
	require.True(t, budget.Exhausted(), "次数用尽即 Exhausted")
	require.Greater(t, budget.Remaining(), time.Duration(0), "次数用尽不影响剩余时间")

	expired := NewRequestAttemptBudget(now, 2, time.Hour, now.Add(-time.Millisecond))
	require.Equal(t, time.Duration(0), expired.Remaining(), "过期剩余时间钳 0")
	require.True(t, expired.Exhausted())
}

// TestRequestAttemptBudgetZeroMaxSendsFailsClosed maxSends<=0 时不允许任何发送：
// “不限制”只能通过不挂预算（nil）表达，非 nil 预算零值 fail-closed。
func TestRequestAttemptBudgetZeroMaxSendsFailsClosed(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), 0, time.Hour, time.Time{})
	err := budget.AllowSend()
	require.ErrorIs(t, err, ErrRequestAttemptBudgetExhausted)
	require.Equal(t, 0, budget.Used())
	require.True(t, budget.Exhausted())

	var zero RequestAttemptBudget
	require.ErrorIs(t, zero.AllowSend(), ErrRequestAttemptBudgetExhausted)
}

// TestRequestAttemptBudgetConcurrentSendsCap 30 个 goroutine 各发一次，恰好 16 次放行，
// 且全部拒绝都归类为次数耗尽（-race 下验证 mu 保护 used）。
func TestRequestAttemptBudgetConcurrentSendsCap(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), DefaultRequestAttemptBudgetSends, time.Hour, time.Time{})

	const goroutines = 30
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		allowed  int
		denied   int
		firstErr error
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := budget.AllowSend()
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				allowed++
				return
			}
			denied++
			if firstErr == nil {
				firstErr = err
			}
		}()
	}
	wg.Wait()

	require.Equal(t, DefaultRequestAttemptBudgetSends, allowed)
	require.Equal(t, goroutines-DefaultRequestAttemptBudgetSends, denied)
	require.Equal(t, DefaultRequestAttemptBudgetSends, budget.Used())
	require.ErrorIs(t, firstErr, ErrRequestAttemptBudgetExhausted)
}

// TestRequestAttemptBudgetNilSafety nil receiver 全方法安全：无预算 = 不限制。
func TestRequestAttemptBudgetNilSafety(t *testing.T) {
	var budget *RequestAttemptBudget
	require.NoError(t, budget.AllowSend(), "nil 预算必须放行（S1 语义：无预算=不限制）")
	require.Equal(t, 0, budget.Used())
	require.Equal(t, time.Duration(0), budget.Remaining())
	require.False(t, budget.Exhausted())
	require.True(t, budget.Deadline().IsZero())
	require.Nil(t, RequestAttemptBudgetFromContext(nil))
	require.Nil(t, RequestAttemptBudgetFromContext(context.Background()))
	require.NotNil(t, WithRequestAttemptBudget(nil, budget))
}

// TestRequestAttemptBudgetContextRoundTrip ctx 存取：挂载后可取回同一指针并正常放行；
// 未挂载返回 nil。
func TestRequestAttemptBudgetContextRoundTrip(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)

	got := RequestAttemptBudgetFromContext(ctx)
	require.Same(t, budget, got)
	require.NoError(t, got.AllowSend())
	require.Equal(t, 1, budget.Used())

	require.Nil(t, RequestAttemptBudgetFromContext(context.Background()))
}
