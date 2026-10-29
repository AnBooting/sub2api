//go:build unit

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageStatsRepair_RecomputeRejectsIncompleteClosedDayBeforeDeleting(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	repo.clock = func() time.Time { return time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC) }
	dayStart := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)
	rawSafeFrom := time.Date(2026, 10, 25, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS SHARE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).AddRow(rawSafeFrom, "Asia/Shanghai"))
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", dayStart, dayEnd).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}))
	mock.ExpectExec(`INSERT INTO usage_stats_repair_days`).
		WithArgs("Asia/Shanghai", dayStart, dayEnd, "raw_history_incomplete").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.RecomputeRange(context.Background(), dayStart, dayEnd)
	require.ErrorContains(t, err, "cannot safely recompute")
	require.NoError(t, mock.ExpectationsWereMet(), "no dashboard or group bucket may be deleted or overwritten")
}

func TestUsageStatsRepair_ScheduledSkipsUnsafeClosedDayButRefreshesOpenDay(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	today := time.Date(2026, 10, 28, 16, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return today.Add(6 * time.Hour) }
	yesterday := today.AddDate(0, 0, -1)
	rawSafeFrom := today

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS SHARE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).AddRow(rawSafeFrom, "Asia/Shanghai"))
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", yesterday, today.AddDate(0, 0, 1)).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}))
	mock.ExpectExec(`INSERT INTO usage_stats_repair_days`).
		WithArgs("Asia/Shanghai", yesterday, today, "raw_history_incomplete").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_hourly_users`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_daily_users`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_hourly`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_daily`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.ErrorIs(t, repo.AggregateRange(context.Background(), yesterday, today.Add(6*time.Hour)), service.ErrUsageStatsIncomplete)
	require.NoError(t, mock.ExpectationsWereMet(), "only the open day may be refreshed")
}

func TestUsageStatsRepair_AggregateSkipsAlreadyQuarantinedDay(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	today := time.Date(2026, 10, 28, 16, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return today.Add(6 * time.Hour) }
	yesterday := today.AddDate(0, 0, -1)
	rawSafeFrom := today

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS SHARE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).AddRow(rawSafeFrom, "Asia/Shanghai"))
	// 昨日已是 quarantine（持久隔离）→ 不得聚合覆盖；开放日仍刷新。
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", yesterday, today.AddDate(0, 0, 1)).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}).AddRow(yesterday))
	mock.ExpectExec(`INSERT INTO usage_dashboard_hourly_users`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_daily_users`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_hourly`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_dashboard_daily`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.ErrorIs(t, repo.AggregateRange(context.Background(), yesterday, today.Add(6*time.Hour)), service.ErrUsageStatsIncomplete)
	require.NoError(t, mock.ExpectationsWereMet(), "已隔离日不聚合，开放日仍刷新")
}

func TestUsageStatsRepair_RetentionAdvancesBoundaryBeforeDeleting(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "America/New_York")
	db, mock := newSQLMock(t)
	cutoff := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	nextMidnight := time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ROW EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).AddRow(cutoff.AddDate(0, 0, -10), "America/New_York"))
	mock.ExpectExec(`UPDATE usage_stats_repair_control`).WithArgs(nextMidnight).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_stats_repair_days`).WithArgs(nextMidnight).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`DELETE FROM usage_logs.*RETURNING created_at`).
		WithArgs(cutoff, usageLogsCleanupBatchSize).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(cutoff.Add(-time.Hour)))
	mock.ExpectCommit()

	count, err := cleanupUsageLogsBatchWithRollupInvalidation(context.Background(), db, cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.NoError(t, mock.ExpectationsWereMet(), "retention must not invalidate an incomplete published group bucket")
}

func TestUsageStatsRepair_ManualCleanupUsesConsistentLocksAndDoesNotRewindOldBuckets(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := &usageCleanupRepository{sql: db}
	start := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ROW EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	// 人工删除不得推进全局 raw_safe_from（否则删除当日无法重算、旧桶残留被删数据）：
	// 任何 repair_control / repair_days 写入都会成为未预期查询而使本测试失败。
	mock.ExpectQuery(`DELETE FROM usage_logs.*RETURNING created_at`).
		WithArgs(start, end, 1).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(start.Add(time.Hour)))
	mock.ExpectCommit()

	deleted, err := repo.deleteUsageLogsBatchWithRollupInvalidation(context.Background(), db, "created_at >= $1 AND created_at < $2", []any{start, end, 1}, end)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.NoError(t, mock.ExpectationsWereMet(), "manual cleanup must not advance raw_safe_from nor rewind published buckets")
}

// P1-6：RecomputeRange 范围若含已有 quarantine 日，即使混合安全日也必须整体拒绝，
// 不得跳过隔离日继续 DELETE/重建（那会覆盖旧桶）。
func TestUsageStatsRepair_RecomputeMixedQuarantineDayDoesNotDeleteOldBucket(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	repo.clock = func() time.Time { return time.Date(2026, 10, 29, 4, 0, 0, 0, time.UTC) }
	dayStart := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)
	rawSafeFrom := time.Date(2026, 10, 25, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS SHARE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).AddRow(rawSafeFrom, "Asia/Shanghai"))
	// 范围内第 1 日已是 quarantine（持久隔离）
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", dayStart, dayEnd).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}).AddRow(dayStart))
	// 整体拒绝但仍提交（持久化隔离记录），绝不 DELETE/重建
	mock.ExpectCommit()

	err := repo.RecomputeRange(context.Background(), dayStart, dayEnd)
	require.ErrorContains(t, err, "cannot safely recompute")
	require.NoError(t, mock.ExpectationsWereMet(), "不 DELETE 不重建隔离日；整体拒绝")
}
func TestUsageStatsRepair_DailyAggregateReadsOriginalUsage(t *testing.T) {
	var executed string
	matcher := sqlmock.QueryMatcherFunc(func(_, actual string) error {
		if strings.TrimSpace(actual) == "" {
			return fmt.Errorf("empty query")
		}
		executed = actual
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	mock.ExpectExec("daily").WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.upsertDailyAggregates(context.Background(), start, start.AddDate(0, 0, 1)))
	require.Contains(t, executed, "FROM usage_logs")
	require.NotContains(t, executed, "FROM usage_dashboard_hourly")
	require.NoError(t, mock.ExpectationsWereMet())
}
