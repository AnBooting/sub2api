package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type usageRepairQueueTestStub struct {
	claims       []UsageRepairDayClaim
	claimErr     error
	ackCalls     int
	failCalls    int
	acked        []UsageRepairDayClaim
	failed       []UsageRepairDayClaim
	failReasons  []string
	failBackoffs []time.Duration
	failErr      error
}

func (s *usageRepairQueueTestStub) ClaimDue(ctx context.Context, owner string, limit int, ttl time.Duration) ([]UsageRepairDayClaim, error) {
	return s.claims, s.claimErr
}

func (s *usageRepairQueueTestStub) Ack(ctx context.Context, timezoneName string, dayStart time.Time, generation int64, owner string, fence int64) error {
	s.ackCalls++
	s.acked = append(s.acked, UsageRepairDayClaim{TimezoneName: timezoneName, DayStart: dayStart, Generation: generation, Owner: owner, Fence: fence})
	return nil
}

func (s *usageRepairQueueTestStub) Fail(ctx context.Context, timezoneName string, dayStart time.Time, generation int64, owner string, fence int64, reason string, backoff time.Duration) error {
	s.failCalls++
	s.failed = append(s.failed, UsageRepairDayClaim{TimezoneName: timezoneName, DayStart: dayStart, Generation: generation, Owner: owner, Fence: fence})
	s.failReasons = append(s.failReasons, reason)
	s.failBackoffs = append(s.failBackoffs, backoff)
	return s.failErr
}

type usageRepairDashboardStub struct {
	recomputeCalls int
	recomputeErr   error
	lastStart      time.Time
	lastEnd        time.Time
}

func (s *usageRepairDashboardStub) AggregateRange(ctx context.Context, start, end time.Time) error {
	return nil
}
func (s *usageRepairDashboardStub) RecomputeRange(ctx context.Context, start, end time.Time) error {
	s.recomputeCalls++
	s.lastStart, s.lastEnd = start, end
	return s.recomputeErr
}
func (s *usageRepairDashboardStub) GetAggregationWatermark(ctx context.Context) (time.Time, error) {
	return time.Time{}, nil
}
func (s *usageRepairDashboardStub) UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error {
	return nil
}
func (s *usageRepairDashboardStub) CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error {
	return nil
}
func (s *usageRepairDashboardStub) CleanupUsageLogs(ctx context.Context, cutoff time.Time) error {
	return nil
}
func (s *usageRepairDashboardStub) CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error {
	return nil
}
func (s *usageRepairDashboardStub) EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error {
	return nil
}

func TestUsageStatsRepairBackoffBounds(t *testing.T) {
	require.Equal(t, 5*time.Second, usageRepairBackoff(1))
	require.Equal(t, 10*time.Second, usageRepairBackoff(2))
	require.Equal(t, usageRepairMaxBackoff, usageRepairBackoff(20))
}

func TestUsageStatsRepairServiceAcksAfterRecompute(t *testing.T) {
	queue := &usageRepairQueueTestStub{
		claims: []UsageRepairDayClaim{{TimezoneName: "Asia/Shanghai", DayStart: time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC), DayEnd: time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC), Generation: 3, Attempts: 1, Owner: "worker-a", Fence: 7}},
	}
	dash := &usageRepairDashboardStub{}
	svc := &UsageStatsRepairService{queue: queue, dashboards: dash}
	svc.processClaim(context.Background(), queue.claims[0])
	require.Equal(t, 1, dash.recomputeCalls)
	require.Equal(t, 1, queue.ackCalls)
	require.Equal(t, 0, queue.failCalls)
}

func TestUsageStatsRepairServiceFailsWithBackoffWhenRecomputeErrors(t *testing.T) {
	queue := &usageRepairQueueTestStub{
		claims: []UsageRepairDayClaim{{TimezoneName: "Asia/Shanghai", DayStart: time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC), DayEnd: time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC), Generation: 3, Attempts: 2, Owner: "worker-a", Fence: 7}},
	}
	dash := &usageRepairDashboardStub{recomputeErr: errors.New("boom")}
	svc := &UsageStatsRepairService{queue: queue, dashboards: dash}
	svc.processClaim(context.Background(), queue.claims[0])
	require.Equal(t, 1, queue.failCalls)
	require.Equal(t, 0, queue.ackCalls, "失败的日不应被 ack，防止把不完整补算当 clean")
	require.Equal(t, "boom", queue.failReasons[0])
	require.True(t, queue.failBackoffs[0] >= 5*time.Second)
	require.GreaterOrEqual(t, queue.failBackoffs[0], time.Duration(1<<uint(min(2-1, 10)))*5*time.Second)
}
