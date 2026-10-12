package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// TS-02（PRD-02）统一重试预算的默认值：单逻辑请求初值 16 次真实上游推理发送、
// 600 秒总尝试预算（project-docs/prd/11-共享契约.md:102；90-评审 Q-3：这是用户
// 确认的新初值，不是线上压测结论）。所有 Provider 与同号隐藏重试共享同一预算；
// 选号/预检不算发送；累计首字节后不重放；客户端更早 deadline 及原有更小限制优先。
// 对应 C-8 配置键 gateway.max_attempts_per_request / gateway.request_budget_seconds
// （配置化读取属于 S2，本文件只提供代码常量默认值）。
const (
	// DefaultRequestAttemptBudgetSends 单逻辑请求默认真实上游推理发送次数上限。
	DefaultRequestAttemptBudgetSends = 16
	// DefaultRequestAttemptBudgetWindow 单逻辑请求默认总尝试时间预算。
	DefaultRequestAttemptBudgetWindow = 600 * time.Second
)

// 预算拒绝哨兵。AllowSend 返回的错误以 %w 包装其中之一，调用方用 errors.Is 分类，
// 供 S5 失败分类器与结构化日志识别；错误消息内带 used/max 与 deadline/剩余时间，
// 便于不读代码也能定位是“次数到头”还是“时间到头”。
var (
	// ErrRequestAttemptBudgetExhausted 真实上游发送次数已达 maxSends 上限。
	ErrRequestAttemptBudgetExhausted = errors.New("request attempt budget exhausted: upstream send limit reached")
	// ErrRequestAttemptBudgetDeadline 总尝试时间预算（deadline）已用尽。
	ErrRequestAttemptBudgetDeadline = errors.New("request attempt budget deadline exceeded")
)

// RequestAttemptBudget 是单逻辑请求的统一重试预算（TS-02 S1：纯对象，无行为接线，
// 不接 httpUpstream.Do / handler / FailoverState，那些属于 S2/S3）。
//
// 契约（11-共享契约.md:102 + 90-评审 Q-3）：
//   - 一个逻辑请求从其第一次真实上游推理发送开始，共享 16 次发送与 600 秒总尝试
//     预算；同号隐藏重试、跨 Provider 重试都从同一本账里扣。
//   - deadline = min(start+window, clientDeadline)：客户端更早的 deadline 优先；
//     clientDeadline 为零值表示无客户端约束，忽略。
//   - 本对象只查询 deadline，刻意不给请求 ctx 加 context.WithTimeout——流式路径
//     用 context.WithoutCancel 脱离客户端取消/计费，自行 WithTimeout 会破坏该约定。
//   - maxSends <= 0 视为不允许任何发送（fail-closed）：“不限制”的表示方式是不挂
//     预算（nil），见各方法的 nil 语义。
type RequestAttemptBudget struct {
	start    time.Time
	maxSends int
	window   time.Duration
	deadline time.Time

	mu   sync.Mutex
	used int
}

// NewRequestAttemptBudget 构造单逻辑请求预算：deadline = min(start+window, clientDeadline)，
// clientDeadline 零值忽略。now 零值按“预算从此刻起算”兜底，避免零值 time 造出立即
// 过期的 deadline 把请求全部挡死。window 为负表示构造时即已超时（fail-closed）。
func NewRequestAttemptBudget(now time.Time, maxSends int, window time.Duration, clientDeadline time.Time) *RequestAttemptBudget {
	start := now
	if start.IsZero() {
		start = time.Now()
	}
	deadline := start.Add(window)
	if !clientDeadline.IsZero() && clientDeadline.Before(deadline) {
		deadline = clientDeadline
	}
	return &RequestAttemptBudget{
		start:    start,
		maxSends: maxSends,
		window:   window,
		deadline: deadline,
	}
}

// AllowSend 为一次真实上游推理发送申请预算：放行返回 nil，拒绝返回包装了
// ErrRequestAttemptBudgetExhausted / ErrRequestAttemptBudgetDeadline 的错误。
// 只有放行时才把 used +1——used 的语义是“已放行的真实上游发送次数”，被预算挡下
// 的尝试从未发生，不虚增 attempt_count（C-6）。选号/预检不算发送，不应调用本方法。
//
// nil 安全是刻意设计：S1 阶段部分入口尚未挂预算，语义为“无预算 = 不限制”，
// AllowSend 直接放行。S3 接入后所有生产入口都会挂预算；届时若仍出现 nil，说明
// 漏接线，调用方（S5 分类器）应把 nil 预算视作无约束而不是错误。
func (b *RequestAttemptBudget) AllowSend() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.used + 1
	if next > b.maxSends {
		return fmt.Errorf("%w: used=%d max=%d", ErrRequestAttemptBudgetExhausted, b.used, b.maxSends)
	}
	if !b.deadline.IsZero() && !time.Now().Before(b.deadline) {
		return fmt.Errorf("%w: deadline=%s remaining=%s",
			ErrRequestAttemptBudgetDeadline, b.deadline.Format(time.RFC3339Nano), b.remainingLocked())
	}
	b.used = next
	return nil
}

// Used 返回已放行的真实上游发送次数。nil 预算返回 0。
func (b *RequestAttemptBudget) Used() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Remaining 返回相对 deadline 的剩余时间，过期时钳为 0。nil 预算返回 0。
func (b *RequestAttemptBudget) Remaining() time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remainingLocked()
}

// Exhausted 报告预算是否已用尽：used >= maxSends（含 maxSends <= 0 的 fail-closed
// 情形）或已过 deadline。nil 预算返回 false（无预算 = 不限制，见 AllowSend 注释）。
func (b *RequestAttemptBudget) Exhausted() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.maxSends {
		return true
	}
	return !b.deadline.IsZero() && !time.Now().Before(b.deadline)
}

// Deadline 返回本预算的绝对截止时刻（min(start+window, clientDeadline)）。
// nil 预算返回零值 time.Time。
func (b *RequestAttemptBudget) Deadline() time.Time {
	if b == nil {
		return time.Time{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deadline
}

// remainingLocked 调用方必须已持有 b.mu；负数钳 0。
func (b *RequestAttemptBudget) remainingLocked() time.Duration {
	if b.deadline.IsZero() {
		return 0
	}
	if remaining := time.Until(b.deadline); remaining > 0 {
		return remaining
	}
	return 0
}

type gatewayRequestAttemptBudgetCtxKey struct{}

// WithRequestAttemptBudget 把单逻辑请求预算挂到 ctx 上，供转发路径各处查询。
// ctx 为 nil 时退回 context.Background()，与 WithGatewayTokenRequestPricing 一致。
func WithRequestAttemptBudget(ctx context.Context, budget *RequestAttemptBudget) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gatewayRequestAttemptBudgetCtxKey{}, budget)
}

// RequestAttemptBudgetFromContext 取回挂载的预算；未挂载（或 ctx 为 nil）返回 nil，
// 调用方按“无预算 = 不限制”处理（见 RequestAttemptBudget.AllowSend 的 nil 语义）。
func RequestAttemptBudgetFromContext(ctx context.Context) *RequestAttemptBudget {
	if ctx == nil {
		return nil
	}
	budget, _ := ctx.Value(gatewayRequestAttemptBudgetCtxKey{}).(*RequestAttemptBudget)
	return budget
}
