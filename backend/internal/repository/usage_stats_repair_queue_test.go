//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageStatsRepairQueueClaimDueLeasesDirtyDays(t *testing.T) {
	db, mock := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	require.NotNil(t, queue)
	dayStart := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)

	mock.ExpectQuery(`(?s)WITH due AS.*FOR UPDATE SKIP LOCKED`).
		WithArgs(20, `usage_stats_repair_worker`, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			`timezone_name`, `day_start`, `day_end`, `generation`, `attempts`, `reason_code`, `claim_owner`, `claim_fence`,
		}).AddRow(`Asia/Shanghai`, dayStart, dayEnd, int64(3), int(2), ``, `worker-a`, int64(7)))

	claims, err := queue.ClaimDue(context.Background(), `usage_stats_repair_worker`, 20, 2*time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.Equal(t, `Asia/Shanghai`, claims[0].TimezoneName)
	require.Equal(t, dayStart, claims[0].DayStart)
	require.Equal(t, dayEnd, claims[0].DayEnd)
	require.Equal(t, int64(3), claims[0].Generation)
	require.Equal(t, 2, claims[0].Attempts)
	require.Equal(t, `worker-a`, claims[0].Owner)
	require.Equal(t, int64(7), claims[0].Fence)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageStatsRepairQueueClaimDueInvalidArgs(t *testing.T) {
	db, _ := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	_, err := queue.ClaimDue(context.Background(), ``, 20, time.Minute)
	require.ErrorIs(t, err, service.ErrUsageRepairInvalid)
	_, err = queue.ClaimDue(context.Background(), `worker`, 0, time.Minute)
	require.ErrorIs(t, err, service.ErrUsageRepairInvalid)
}

func TestUsageStatsRepairQueueAckMarksCleanByGeneration(t *testing.T) {
	db, mock := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	dayStart := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE usage_stats_repair_days.*state = 'clean'`).
		WithArgs(`Asia/Shanghai`, dayStart, int64(3), `worker-a`, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, queue.Ack(context.Background(), `Asia/Shanghai`, dayStart, 3, `worker-a`, 7))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageStatsRepairQueueAckLostLeaseWhenGenerationMoved(t *testing.T) {
	db, mock := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	dayStart := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE usage_stats_repair_days.*state = 'clean'`).
		WithArgs(`Asia/Shanghai`, dayStart, int64(3), `worker-a`, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := queue.Ack(context.Background(), `Asia/Shanghai`, dayStart, 3, `worker-a`, 7)
	require.ErrorIs(t, err, service.ErrUsageRepairLostLease)
}

func TestUsageStatsRepairQueueFailBacksOffAndQuarantinesAtMax(t *testing.T) {
	db, mock := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	dayStart := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	// attempts(5) >= max(5) → quarantine
	mock.ExpectExec(`(?s)UPDATE usage_stats_repair_days.*CASE WHEN attempts >= \$6`).
		WithArgs(`Asia/Shanghai`, dayStart, int64(3), `worker-a`, int64(7), service.UsageStatsRepairMaxAttempts, `boom`, int64(1000)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, queue.Fail(context.Background(), `Asia/Shanghai`, dayStart, 3, `worker-a`, 7, `boom`, time.Second))
	require.NoError(t, mock.ExpectationsWereMet())
}

// P1-5：旧 owner/旧 fence 的 ack 不得把 clean/新归因状态改回——WHERE 不命中返回 0 行 → LostLease。
func TestUsageStatsRepairQueueAckStaleOwnerFenceIsLostLease(t *testing.T) {
	db, mock := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	dayStart := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	// 库中 claim_owner=worker-b, claim_fence=8（新 worker 已重领或归属变化）
	mock.ExpectExec(`(?s)UPDATE usage_stats_repair_days.*state = 'clean'`).
		WithArgs(`Asia/Shanghai`, dayStart, int64(3), `worker-a`, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := queue.Ack(context.Background(), `Asia/Shanghai`, dayStart, 3, `worker-a`, 7)
	require.ErrorIs(t, err, service.ErrUsageRepairLostLease, "旧 owner/fence 不得 ack 成功")
	require.NoError(t, mock.ExpectationsWereMet())
}

// P1-5：旧 owner/旧 fence 的 fail 不得把状态改 dirty/quarantine（尤其新 worker 已置 clean 后）。
func TestUsageStatsRepairQueueFailStaleOwnerFenceIsLostLease(t *testing.T) {
	db, mock := newSQLMock(t)
	queue := NewUsageStatsRepairQueue(db)
	dayStart := time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE usage_stats_repair_days.*CASE WHEN attempts >= \$6`).
		WithArgs(`Asia/Shanghai`, dayStart, int64(3), `worker-a`, int64(7), service.UsageStatsRepairMaxAttempts, `old`, int64(1000)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	err := queue.Fail(context.Background(), `Asia/Shanghai`, dayStart, 3, `worker-a`, 7, `old`, time.Second)
	require.ErrorIs(t, err, service.ErrUsageRepairLostLease, "旧 owner/fence 不得 fail 修改状态")
	require.NoError(t, mock.ExpectationsWereMet())
}
