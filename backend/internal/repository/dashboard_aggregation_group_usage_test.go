//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDashboardAggregationRepositorySyncGroupUsageRollupsNoopsAtCurrentDate(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-08-14", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositorySyncGroupUsageRollupsRebuildsWhenTimezoneChanges(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "America/New_York")

	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 3, 9, 4, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-03-09", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "America/New_York"))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositorySyncGroupUsageRollupsPublishesWatermarkLast(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)
	rebuildStart := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-08-13", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC), todayStart).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}))
	mock.ExpectExec(`DELETE FROM usage_group_daily_rollups`).
		WithArgs("2026-08-13", "2026-08-14").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_group_daily_rollups`).
		WithArgs(rebuildStart, todayStart, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs("2026-08-14", time.Unix(0, 0).UTC(), "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositorySyncGroupUsageRollupsRejectsFutureWatermark(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow("2026-08-15", time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectRollback()

	err := repo.SyncGroupUsageRollups(context.Background(), todayStart)
	require.ErrorContains(t, err, "未来")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryRecomputeRangeInvalidatesGroupRollupsBeforeDashboardRebuild(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS SHARE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs(start, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM usage_dashboard_hourly`).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err := repo.RecomputeRange(context.Background(), start, end)
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryRecomputeRangeRebuildsGroupRollupsBeforeCommit(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }
	todayStart := service.GroupUsageTodayStart(fixedNow)
	startDate := service.GroupUsageDate(start)
	rebuildStart, err := service.ParseGroupUsageDate(startDate)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS SHARE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs(start, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, query := range []string{
		`DELETE FROM usage_dashboard_hourly WHERE`,
		`DELETE FROM usage_dashboard_hourly_users WHERE`,
		`DELETE FROM usage_dashboard_daily WHERE`,
		`DELETE FROM usage_dashboard_daily_users WHERE`,
		`INSERT INTO usage_dashboard_hourly_users`,
		`INSERT INTO usage_dashboard_daily_users`,
		`INSERT INTO usage_dashboard_hourly`,
		`INSERT INTO usage_dashboard_daily`,
	} {
		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow(startDate, time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", rebuildStart.UTC(), todayStart.UTC()).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}))
	mock.ExpectExec(`DELETE FROM usage_group_daily_rollups`).
		WithArgs(startDate, service.GroupUsageDate(todayStart)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_group_daily_rollups`).
		WithArgs(rebuildStart.UTC(), todayStart.UTC(), "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs(service.GroupUsageDate(todayStart), time.Unix(0, 0).UTC(), "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.RecomputeRange(context.Background(), start, end))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsNonPartitionedInvalidatesEachBatchAndSyncs(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	earliestDeletedAt := time.Date(2026, 5, 3, 2, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }
	todayStart := service.GroupUsageTodayStart(fixedNow)

	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ROW EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	npBoundary := advanceToBoundary(t, cutoff)
	mock.ExpectExec(`UPDATE usage_stats_repair_control`).
		WithArgs(npBoundary).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_stats_repair_days`).
		WithArgs(npBoundary).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)DELETE FROM usage_logs.*RETURNING created_at`).
		WithArgs(cutoff, usageLogsCleanupBatchSize).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).
			AddRow(earliestDeletedAt.Add(time.Hour)).
			AddRow(earliestDeletedAt))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow(service.GroupUsageDate(todayStart), time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectCommit()

	require.NoError(t, repo.CleanupUsageLogs(context.Background(), cutoff))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsPartitionedSortsAndInvalidatesEachDropBeforeSync(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	aprilStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	juneStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fixedNow := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	repo.clock = func() time.Time { return fixedNow }
	todayStart := service.GroupUsageTodayStart(fixedNow)

	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT c.relname`).
		WillReturnRows(sqlmock.NewRows([]string{"relname"}).
			AddRow("usage_logs_202606").
			AddRow("usage_logs_invalid").
			AddRow("usage_logs_202604").
			AddRow("usage_logs_202607"))

	for _, partition := range []struct {
		name  string
		start time.Time
	}{
		{name: "usage_logs_202604", start: aprilStart},
		{name: "usage_logs_202606", start: juneStart},
	} {
		mock.ExpectBegin()
		mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
			WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
				AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
		advanceArg := advanceToBoundary(t, truncateToMonthUTC(partition.start).AddDate(0, 1, 0))
		mock.ExpectExec(`UPDATE usage_stats_repair_control`).
			WithArgs(advanceArg).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`UPDATE usage_stats_repair_days`).
			WithArgs(advanceArg).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`DROP TABLE IF EXISTS "` + partition.name + `"`).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
	}
	// The boundary partition is pruned by exact timestamp. tableoid is required
	// because ctid alone can also identify a retained row in another partition.
	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ROW EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	boundaryArg := advanceToBoundary(t, cutoff)
	mock.ExpectExec(`UPDATE usage_stats_repair_control`).
		WithArgs(boundaryArg).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_stats_repair_days`).
		WithArgs(boundaryArg).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT tableoid, ctid.*WHERE created_at < \$1.*WHERE \(tableoid, ctid\) IN.*RETURNING created_at`).
		WithArgs(cutoff, usageLogsCleanupBatchSize).
		WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(cutoff.Add(-time.Hour)))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow(service.GroupUsageDate(todayStart), time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectCommit()

	require.NoError(t, repo.CleanupUsageLogs(context.Background(), cutoff))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsNonPartitionedFailureRollsBackWithoutSync(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ROW EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	npfBoundary := advanceToBoundary(t, cutoff)
	mock.ExpectExec(`UPDATE usage_stats_repair_control`).
		WithArgs(npfBoundary).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_stats_repair_days`).
		WithArgs(npfBoundary).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err := repo.CleanupUsageLogs(context.Background(), cutoff)
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryCleanupUsageLogsPartitionFailureRollsBackAndStops(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	cutoff := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	aprilStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	dropErr := errors.New("drop partition failed")

	mock.ExpectQuery(`SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`SELECT c.relname`).
		WillReturnRows(sqlmock.NewRows([]string{"relname"}).
			AddRow("usage_logs_202606").
			AddRow("usage_logs_202604"))
	mock.ExpectBegin()
	mock.ExpectExec(`LOCK TABLE ONLY usage_logs IN ACCESS EXCLUSIVE MODE`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	pfBoundary := advanceToBoundary(t, truncateToMonthUTC(aprilStart).AddDate(0, 1, 0))
	mock.ExpectExec(`UPDATE usage_stats_repair_control`).
		WithArgs(pfBoundary).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE usage_stats_repair_days`).
		WithArgs(pfBoundary).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DROP TABLE IF EXISTS "usage_logs_202604"`).
		WillReturnError(dropErr)
	mock.ExpectRollback()

	err := repo.CleanupUsageLogs(context.Background(), cutoff)
	require.ErrorIs(t, err, dropErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

// P1-7：raw_safe_from 落在当天中途（迁移时刻）时，最早可重算日为下一天；
// 当天因明细不完整必须隔离，不得 DELETE/重建当天桶。
func TestDashboardAggregationRepositorySyncGroupUsageRollupsSafeFromMiddayStartsNextDay(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC) // 2026-08-14 00:00+08
	rawSafeFrom := time.Date(2026, 8, 9, 22, 0, 0, 0, time.UTC) // 2026-08-10 06:00+08（当天中途）
	closedBefore := "2026-08-09"

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow(closedBefore, time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(rawSafeFrom, "Asia/Shanghai"))
	// 08-10（raw_safe_from 所在日）不完整 → 持久隔离
	// gap 隔离起点是已发布水位 closedBefore 的本地零点（08-09 00:00+08 = 08-08 16:00 UTC）
	closedTime := time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC)
	// gap [closedTime, rebuildStart) 含 08-09、08-10 两日，均持久隔离
	mock.ExpectExec(`INSERT INTO usage_stats_repair_days`).
		WithArgs("Asia/Shanghai", closedTime, closedTime.AddDate(0, 0, 1), "raw_history_incomplete").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_stats_repair_days`).
		WithArgs("Asia/Shanghai", closedTime.AddDate(0, 0, 1), closedTime.AddDate(0, 0, 2), "raw_history_incomplete").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// The newly quarantined gap starts at closedBefore. No bucket or watermark
	// may be changed, even though raw data after 08-11 is complete.
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet(), "raw_safe_from 当天中途→重建从下一天起，当天隔离不 DELETE")
}

// P1-16：sync 后水位不得越过未证实完整的隔离日——mid-range quarantine 日
// 使 watermark 只推进到该日之前；DELETE/重建不覆盖隔离日。
func TestDashboardAggregationRepositorySyncGroupUsageRollupsStopsAtQuarantinedDay(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC) // 2026-08-14 00:00+08
	closedBefore := "2026-08-09"
	quarantineDay := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC) // 2026-08-13 00:00+08 隔离
	rebuildStart := time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC)   // Parse("2026-08-09") 本地零点

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT closed_before::text, retained_from.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"closed_before", "retained_from", "timezone_name"}).
			AddRow(closedBefore, time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	mock.ExpectQuery(`SELECT raw_safe_from, timezone_name FROM usage_stats_repair_control.*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"raw_safe_from", "timezone_name"}).
			AddRow(time.Unix(0, 0).UTC(), "Asia/Shanghai"))
	// mid-range quarantine 日（08-13）
	// rebuildStart = 已发布水位本地零点（08-09 00:00+08 = 08-08 16:00 UTC）
	mock.ExpectQuery(`(?s)SELECT day_start.*state = 'quarantine'`).
		WithArgs("Asia/Shanghai", time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC), todayStart).
		WillReturnRows(sqlmock.NewRows([]string{"day_start"}).AddRow(quarantineDay))
	// WATERMARK 从 08-09 起 DELETE/INSERT，但只覆盖到隔离日之前 [08-09, 08-13)
	mock.ExpectExec(`DELETE FROM usage_group_daily_rollups`).
		WithArgs("2026-08-09", "2026-08-13").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO usage_group_daily_rollups`).
		WithArgs(rebuildStart, quarantineDay, "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// 水位推进到隔离日前（08-13），绝不越过 08-13
	mock.ExpectExec(`UPDATE usage_group_rollup_state`).
		WithArgs("2026-08-13", time.Unix(0, 0).UTC(), "Asia/Shanghai").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), todayStart))
	require.NoError(t, mock.ExpectationsWereMet(), "水位不越过未证实完整的隔离日")
}

func setGroupUsageRollupTestTimezone(t *testing.T) {
	t.Helper()
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
}
