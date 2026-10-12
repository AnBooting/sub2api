//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// budgetHookRecordingUpstream 记录 Do / DoWithTLS 的调用次数与最后一次 profile，
// 用于断言 S3 钩子「放行才转发、拒绝不转发」以及按 profile 选择转发方法。
type budgetHookRecordingUpstream struct {
	mu       sync.Mutex
	doCalls  int
	tlsCalls int
	lastTLS  *tlsfingerprint.Profile
}

func (u *budgetHookRecordingUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.doCalls++
	u.mu.Unlock()
	return budgetHookResponse(), nil
}

func (u *budgetHookRecordingUpstream) DoWithTLS(_ *http.Request, _ string, _ int64, _ int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.mu.Lock()
	u.tlsCalls++
	u.lastTLS = profile
	u.mu.Unlock()
	return budgetHookResponse(), nil
}

func (u *budgetHookRecordingUpstream) calls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.doCalls + u.tlsCalls
}

func (u *budgetHookRecordingUpstream) tlsProfile() *tlsfingerprint.Profile {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastTLS
}

func budgetHookResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("{}")),
	}
}

func budgetHookRequest(ctx context.Context) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/messages", strings.NewReader("{}"))
}

func newBudgetHookRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := budgetHookRequest(ctx)
	require.NoError(t, err)
	return req
}

// TestSendUpstreamWithBudgetNilBudgetForwardsWithoutCounting 固定 nil 预算语义：
// 未接线入口「无预算 = 不限制」，直接转发且不计数。
func TestSendUpstreamWithBudgetNilBudgetForwardsWithoutCounting(t *testing.T) {
	upstream := &budgetHookRecordingUpstream{}

	resp, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, context.Background()), "", 1, 1, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 1, upstream.calls(), "nil 预算必须照常转发一次")
	require.Equal(t, 1, upstream.doCalls)
	require.Equal(t, 0, upstream.tlsCalls)
	require.Nil(t, RequestAttemptBudgetFromContext(context.Background()))
}

// TestSendUpstreamWithBudgetRejectsAfterCeiling 固定 S3 拦截语义：超过 maxSends 后
// 返回哨兵错误且不再转发，used 停在 maxSends（被拒的尝试不虚增计数）。
func TestSendUpstreamWithBudgetRejectsAfterCeiling(t *testing.T) {
	const maxSends = 3
	budget := NewRequestAttemptBudget(time.Now(), maxSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)
	upstream := &budgetHookRecordingUpstream{}

	for i := 1; i <= maxSends; i++ {
		resp, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, ctx), "", 1, 1, nil)
		require.NoErrorf(t, err, "第 %d 次发送必须放行", i)
		require.NotNil(t, resp)
	}

	for i := maxSends + 1; i <= 5; i++ {
		resp, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, ctx), "", 1, 1, nil)
		require.ErrorIsf(t, err, ErrRequestAttemptBudgetExhausted, "第 %d 次发送必须被预算拒绝", i)
		require.Nil(t, resp, "被拒绝的发送不得返回响应")
	}

	require.Equal(t, maxSends, upstream.calls(), "预算耗尽后不得再转发")
	require.Equal(t, maxSends, budget.Used(), "used 停在 maxSends，被拒的尝试不计数")
	require.True(t, budget.Exhausted())
}

// TestSendUpstreamWithBudgetCountsEachHiddenRetry 模拟 gateway_forward.go 主循环的
// 5 次真实发送：同号隐藏重试各自占用一次预算，used == 5。
func TestSendUpstreamWithBudgetCountsEachHiddenRetry(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)
	upstream := &budgetHookRecordingUpstream{}

	for attempt := 1; attempt <= 5; attempt++ {
		_, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, ctx), "", 1, 1, nil)
		require.NoError(t, err)
		require.Equalf(t, attempt, budget.Used(), "第 %d 次隐藏重试必须各自 +1", attempt)
	}
	require.Equal(t, 5, upstream.calls())
}

// TestSendUpstreamWithBudgetExpiredDeadlineRejects 时间预算用尽同样拦截：返回
// deadline 哨兵错误且不转发。
func TestSendUpstreamWithBudgetExpiredDeadlineRejects(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now().Add(-time.Hour), DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)
	upstream := &budgetHookRecordingUpstream{}

	resp, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, ctx), "", 1, 1, nil)
	require.ErrorIs(t, err, ErrRequestAttemptBudgetDeadline)
	require.Nil(t, resp)
	require.Equal(t, 0, upstream.calls(), "deadline 过期后不得转发")
	require.Equal(t, 0, budget.Used(), "被 deadline 拒绝的尝试不计数")
}

// TestSendUpstreamWithBudgetConcurrentSendsStayWithinCeiling 并发下恰好放行
// maxSends 次，其余全部被拒且不转发。
func TestSendUpstreamWithBudgetConcurrentSendsStayWithinCeiling(t *testing.T) {
	const (
		maxSends   = 8
		goroutines = 64
	)
	budget := NewRequestAttemptBudget(time.Now(), maxSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)
	upstream := &budgetHookRecordingUpstream{}

	var (
		wg        sync.WaitGroup
		forwarded int64
		denied    int64
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := budgetHookRequest(ctx)
			if err != nil {
				t.Errorf("build request: %v", err)
				return
			}
			resp, err := sendUpstreamWithBudget(upstream, req, "", 1, 1, nil)
			if err == nil {
				if resp == nil {
					t.Errorf("放行的发送必须返回响应")
				}
				atomic.AddInt64(&forwarded, 1)
				return
			}
			if !errors.Is(err, ErrRequestAttemptBudgetExhausted) {
				t.Errorf("被拒的发送必须返回次数哨兵，实际: %v", err)
				return
			}
			atomic.AddInt64(&denied, 1)
		}()
	}
	wg.Wait()

	require.Equal(t, int64(maxSends), atomic.LoadInt64(&forwarded), "并发下恰好放行 maxSends 次")
	require.Equal(t, int64(goroutines-maxSends), atomic.LoadInt64(&denied), "其余并发发送必须全部被拒")
	require.Equal(t, maxSends, upstream.calls(), "只有放行的发送真正转发")
	require.Equal(t, maxSends, budget.Used(), "并发下放行次数恰好停在 maxSends")
}

// TestSendUpstreamWithBudgetSelectsTLSMethod profile 非 nil 走 DoWithTLS，nil 走 Do。
func TestSendUpstreamWithBudgetSelectsTLSMethod(t *testing.T) {
	upstream := &budgetHookRecordingUpstream{}
	profile := &tlsfingerprint.Profile{}

	resp, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, context.Background()), "http://proxy.local", 7, 3, profile)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 1, upstream.tlsCalls)
	require.Equal(t, 0, upstream.doCalls)
	require.Same(t, profile, upstream.tlsProfile())
}

// TestEnsureRequestAttemptBudgetIsIdempotent 已有预算时原样返回，不重建、不覆盖。
func TestEnsureRequestAttemptBudgetIsIdempotent(t *testing.T) {
	ctx, first := EnsureRequestAttemptBudget(context.Background(), DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	require.NotNil(t, first)
	require.Same(t, first, RequestAttemptBudgetFromContext(ctx))

	reusedCtx, second := EnsureRequestAttemptBudget(ctx, 1, time.Second, time.Time{})
	require.Same(t, first, second, "已有预算必须原样返回")
	require.Same(t, first, RequestAttemptBudgetFromContext(reusedCtx))

	freshCtx, fresh := EnsureRequestAttemptBudget(context.Background(), 2, time.Minute, time.Time{})
	require.NotSame(t, first, fresh, "没有预算的 ctx 必须新建")
	require.Same(t, fresh, RequestAttemptBudgetFromContext(freshCtx))
}

// TestEnsureRequestAttemptBudgetInvalidConfigIsFailClosed 非法配置仍然构造预算
// （S1 fail-closed）；S3 起该预算会拒绝所有发送。
func TestEnsureRequestAttemptBudgetInvalidConfigIsFailClosed(t *testing.T) {
	ctx, budget := EnsureRequestAttemptBudget(nil, 0, 0, time.Time{})
	require.NotNil(t, ctx)
	require.NotNil(t, budget, "maxSends<=0 或 window<=0 仍必须构造 fail-closed 预算")
	require.ErrorIs(t, budget.AllowSend(), ErrRequestAttemptBudgetExhausted)

	upstream := &budgetHookRecordingUpstream{}
	resp, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, ctx), "", 1, 1, nil)
	require.ErrorIs(t, err, ErrRequestAttemptBudgetExhausted, "S3：fail-closed 预算必须挡住发送")
	require.Nil(t, resp)
	require.Equal(t, 0, upstream.calls(), "fail-closed 预算下不得转发")
	require.Equal(t, 0, budget.Used())
}

// TestSendUpstreamWithBudgetSurvivesDetachedContext 流式路径用 context.WithoutCancel
// 脱离客户端取消/计费；ctx value（预算）必须保留，否则 detach 后的发送会漏计。
func TestSendUpstreamWithBudgetSurvivesDetachedContext(t *testing.T) {
	budget := NewRequestAttemptBudget(time.Now(), DefaultRequestAttemptBudgetSends, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)
	detached := context.WithoutCancel(ctx)
	require.Same(t, budget, RequestAttemptBudgetFromContext(detached), "detach 不得丢预算")

	upstream := &budgetHookRecordingUpstream{}
	_, err := sendUpstreamWithBudget(upstream, newBudgetHookRequest(t, detached), "", 1, 1, nil)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls())
	require.Equal(t, 1, budget.Used(), "detach 后的发送仍必须计数")
}

// TestAllowUpstreamSendRejectsWhenBudgetExhausted OpenAI 家族单点钩子的 S3 契约：
// nil 预算放行；预算耗尽时返回哨兵错误；放行时 used +1，被拒时不计数。
func TestAllowUpstreamSendRejectsWhenBudgetExhausted(t *testing.T) {
	require.NoError(t, allowUpstreamSend(context.Background()), "nil 预算直接放行")

	budget := NewRequestAttemptBudget(time.Now(), 1, DefaultRequestAttemptBudgetWindow, time.Time{})
	ctx := WithRequestAttemptBudget(context.Background(), budget)

	require.NoError(t, allowUpstreamSend(ctx))
	require.Equal(t, 1, budget.Used())

	err := allowUpstreamSend(ctx)
	require.ErrorIs(t, err, ErrRequestAttemptBudgetExhausted, "S3：预算耗尽必须返回哨兵错误")
	require.Equal(t, 1, budget.Used(), "被拒的尝试不计数")
}

// TestNewRequestAttemptBudgetFailoverErrorIsTerminating 预算拒绝转成的 failover
// 错误必须是终止型：503 + NextAccountStop（不重试、不换号），Reason 供耗尽出口识别。
func TestNewRequestAttemptBudgetFailoverErrorIsTerminating(t *testing.T) {
	failoverErr := NewRequestAttemptBudgetFailoverError()
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.StatusCode)
	require.Equal(t, NextAccountStop, failoverErr.NextAccountAction)
	require.False(t, failoverErr.ShouldRetryNextAccount(), "预算耗尽必须终止：不重试、不换号")
	require.Equal(t, RequestAttemptBudgetReason, failoverErr.Reason)
	require.False(t, failoverErr.IsCredentialFailure(), "预算耗尽不是账号凭据故障")

	require.True(t, IsRequestAttemptBudgetError(ErrRequestAttemptBudgetExhausted))
	require.True(t, IsRequestAttemptBudgetError(ErrRequestAttemptBudgetDeadline))
	require.True(t, IsRequestAttemptBudgetError(errors.Join(errors.New("wrapped"), ErrRequestAttemptBudgetExhausted)))
	require.False(t, IsRequestAttemptBudgetError(errors.New("other")))
	require.False(t, IsRequestAttemptBudgetError(nil))
}

// TestRetryBackoffDelayWithBudgetClamps 退避钳制：有预算时钳到 Remaining()；
// nil 预算不得被钳（Remaining() 对 nil 返回 0，写成 min 会把退避压成 0）。
func TestRetryBackoffDelayWithBudgetClamps(t *testing.T) {
	require.Equal(t, retryBaseDelay, retryBackoffDelayWithBudget(context.Background(), 1),
		"nil 预算必须保留原始退避")
	require.Equal(t, retryBackoffDelay(3), retryBackoffDelayWithBudget(context.Background(), 3))

	ctx := WithRequestAttemptBudget(context.Background(),
		NewRequestAttemptBudget(time.Now(), DefaultRequestAttemptBudgetSends, 50*time.Millisecond, time.Time{}))
	clamped := retryBackoffDelayWithBudget(ctx, 1)
	require.Greater(t, clamped, time.Duration(0))
	require.LessOrEqual(t, clamped, 50*time.Millisecond)
	require.Less(t, clamped, retryBaseDelay, "剩余预算小于退避时必须缩短")
}

// TestRequestAttemptBudgetMaxSends S3 结构化日志需要 budget_max，nil 预算返回 0。
func TestRequestAttemptBudgetMaxSends(t *testing.T) {
	require.Equal(t, 0, (*RequestAttemptBudget)(nil).MaxSends())
	budget := NewRequestAttemptBudget(time.Now(), 7, DefaultRequestAttemptBudgetWindow, time.Time{})
	require.Equal(t, 7, budget.MaxSends())
}
