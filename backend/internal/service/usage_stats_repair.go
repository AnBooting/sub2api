package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ErrUsageRepairInvalid 表示脏日队列调用参数非法。
var ErrUsageRepairInvalid = errors.New("usage statistics repair queue: invalid argument")

// ErrUsageRepairStoreUnavailable 表示脏日队列存储不可用。
var ErrUsageRepairStoreUnavailable = errors.New("usage statistics repair store is unavailable")

// ErrUsageRepairLostLease 表示该日的代次已被后续写入推进，本次 ack/fail 已过期。
var ErrUsageRepairLostLease = errors.New("usage statistics repair claim has expired or changed")

// ErrUsageStatsIncomplete means the requested statistics include unproven history.
// Callers must not publish a successful watermark or substitute a zero total.
var ErrUsageStatsIncomplete = errors.New("usage statistics unavailable: incomplete history requires reconciliation")

// UsageRepairDayClaim 是一次脏日领取快照。
// Owner/Fence 是本次领取的独占凭证：Ack/Fail 必须携带并命中，
// 防止旧 worker 在租期过期或其他 worker 重领后越权修改状态。
type UsageRepairDayClaim struct {
	TimezoneName string
	DayStart     time.Time
	DayEnd       time.Time
	Generation   int64
	Attempts     int
	ReasonCode   string
	Owner        string
	Fence        int64
}

// UsageStatsRepairQueue 是迟到用量统计脏日持久队列的仓储契约。
type UsageStatsRepairQueue interface {
	ClaimDue(ctx context.Context, owner string, limit int, ttl time.Duration) ([]UsageRepairDayClaim, error)
	// Ack 以 owner+fence+generation 作为独占凭证：仅当前 claim 仍归本 worker 且未被新 claim 覆盖时置 clean。
	Ack(ctx context.Context, timezoneName string, dayStart time.Time, generation int64, owner string, fence int64) error
	// Fail 同上，记录失败并退避；达到上限转为 quarantine。
	Fail(ctx context.Context, timezoneName string, dayStart time.Time, generation int64, owner string, fence int64, reason string, backoff time.Duration) error
}

const (
	usageRepairWorkerName = "usage_stats_repair_worker"
	// usageRepairMaxBackoff 单日重试退避上限，防止反复重算同一天。
	usageRepairMaxBackoff = time.Hour
)

// UsageStatsRepairService 扫描脏日并按代次补算，失败有界重试，不吞事件。
type UsageStatsRepairService struct {
	queue       UsageStatsRepairQueue
	dashboards  DashboardAggregationRepository
	timingWheel *TimingWheelService

	instanceID string

	claimLimit  int
	leaseTTL    time.Duration
	maxAttempts int

	running   int32
	startOnce sync.Once
	stopOnce  sync.Once

	workerCtx    context.Context
	workerCancel context.CancelFunc
}

// NewUsageStatsRepairService 创建脏日补算服务。
func NewUsageStatsRepairService(queue UsageStatsRepairQueue, dashboards DashboardAggregationRepository, timingWheel *TimingWheelService) *UsageStatsRepairService {
	workerCtx, workerCancel := context.WithCancel(context.Background())
	return &UsageStatsRepairService{
		queue:        queue,
		dashboards:   dashboards,
		timingWheel:  timingWheel,
		instanceID:   usageRepairWorkerName + "-" + uuid.NewString()[:8],
		claimLimit:   20,
		leaseTTL:     2 * time.Minute,
		maxAttempts:  UsageStatsRepairMaxAttempts,
		workerCtx:    workerCtx,
		workerCancel: workerCancel,
	}
}

// ownerID 返回本实例唯一标识，作为领取/ack/fail 的独占 owner 凭证。
func (s *UsageStatsRepairService) ownerID() string {
	if s != nil && s.instanceID != "" {
		return s.instanceID
	}
	return usageRepairWorkerName + "-" + uuid.NewString()[:8]
}

// Start 启动定时脏日扫描（复用 timingWheel 以在测试中可控）。
func (s *UsageStatsRepairService) Start() {
	if s == nil || s.queue == nil || s.dashboards == nil || s.timingWheel == nil {
		return
	}
	s.startOnce.Do(func() {
		s.timingWheel.ScheduleRecurring(usageRepairWorkerName, time.Minute, s.runOnce)
	})
}

// Stop 停止脏日扫描。
func (s *UsageStatsRepairService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.workerCancel != nil {
			s.workerCancel()
		}
		if s.timingWheel != nil {
			s.timingWheel.Cancel(usageRepairWorkerName)
		}
	})
}

// runOnce 领取一批到期脏日并逐个处理；任一日失败只记录，不吞整体扫描。
func (s *UsageStatsRepairService) runOnce() {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)

	parent := context.Background()
	if s.workerCtx != nil {
		parent = s.workerCtx
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()

	claims, err := s.queue.ClaimDue(ctx, s.ownerID(), s.claimLimit, s.leaseTTL)
	if err != nil {
		slog.Error("usage stats repair claim failed", "err", err)
		return
	}
	for _, claim := range claims {
		s.processClaim(ctx, claim)
	}
}

func (s *UsageStatsRepairService) processClaim(ctx context.Context, claim UsageRepairDayClaim) {
	if err := s.recomputeClaim(ctx, claim); err != nil {
		backoff := usageRepairBackoff(claim.Attempts)
		if failErr := s.queue.Fail(ctx, claim.TimezoneName, claim.DayStart, claim.Generation, claim.Owner, claim.Fence, err.Error(), backoff); failErr != nil {
			slog.Error("usage stats repair fail failed", "day", claim.DayStart, "err", failErr)
		}
		return
	}
	if err := s.queue.Ack(ctx, claim.TimezoneName, claim.DayStart, claim.Generation, claim.Owner, claim.Fence); err != nil {
		slog.Error("usage stats repair ack failed", "day", claim.DayStart, "err", err)
	}
}

func (s *UsageStatsRepairService) recomputeClaim(ctx context.Context, claim UsageRepairDayClaim) error {
	groupCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return s.dashboards.RecomputeRange(groupCtx, claim.DayStart, claim.DayEnd)
}

func usageRepairBackoff(attempts int) time.Duration {
	if attempts <= 1 {
		return 5 * time.Second
	}
	d := time.Duration(1<<uint(min(attempts-1, 10))) * 5 * time.Second
	if d > usageRepairMaxBackoff {
		return usageRepairMaxBackoff
	}
	return d
}

// UsageStatsRepairMaxAttempts 与 repository 层阈值一致：达到后转 quarantine。
// 导出以便 repository 层复用同一阈值。
const UsageStatsRepairMaxAttempts = 5
