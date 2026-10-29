//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReview29DashboardContinuesAfterUpgradeGap(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-13 06:00+08';
	INSERT INTO usage_dashboard_daily(bucket_date,actual_cost) VALUES('2026-08-13',99);
	INSERT INTO usage_logs(created_at,actual_cost) VALUES('2026-08-13 10:15+08',3),('2026-08-14 10:15+08',5)`)
	loc := time.FixedZone("CST", 8*3600)
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	repo.clock = func() time.Time { return day.Add(12 * time.Hour) }
	for i := 0; i < 2; i++ {
		err := repo.AggregateRange(context.Background(), day.AddDate(0, 0, -1), day.Add(11*time.Hour))
		require.ErrorIs(t, err, service.ErrUsageStatsIncomplete)
		var oldCost, newCost float64
		require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_dashboard_daily WHERE bucket_date='2026-08-13'`).Scan(&oldCost))
		require.NoError(t, db.QueryRow(`SELECT actual_cost FROM usage_dashboard_daily WHERE bucket_date='2026-08-14'`).Scan(&newCost))
		require.Equal(t, float64(99), oldCost, "preserve the unproven old bucket")
		require.Equal(t, float64(5), newCost, "later safe days must still commit despite the earlier gap")
	}
	require.NoError(t, repo.AggregateRange(context.Background(), day, day.Add(11*time.Hour)))
}

func TestReview29DashboardReadersRejectQuarantine(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-14 06:00+08';
	INSERT INTO usage_logs(created_at,actual_cost) VALUES('2026-08-14 10:15+08',5)`)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	_, publicErr := repo.GetUsageTrendWithFilters(context.Background(), day, day.AddDate(0, 0, 1), "day", 0, 0, 0, 0, "", nil, nil, nil)
	require.ErrorIs(t, publicErr, service.ErrUsageStatsIncomplete, "the public entry must not fall back to incomplete raw logs")
	_, err := repo.getUsageTrendFromAggregates(context.Background(), day, day.AddDate(0, 0, 1), "day")
	require.ErrorIs(t, err, service.ErrUsageStatsIncomplete)
	err = repo.fillDashboardUsageStatsAggregated(context.Background(), &DashboardStats{}, day, day.Add(12*time.Hour))
	require.ErrorIs(t, err, service.ErrUsageStatsIncomplete)
	// A later range must recover independently, without clearing the unsafe day.
	_, err = repo.getUsageTrendFromAggregates(context.Background(), day.AddDate(0, 0, 1), day.AddDate(0, 0, 2), "day")
	require.NoError(t, err)
}

func TestReview29GroupSummaryCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name        string
		boundary    string
		quarantined string
		unavailable bool
	}{
		{"safe", "2026-08-01 00:00+08", "", false},
		{"unmarked raw gap", "2026-08-13 06:00+08", "", true},
		{"published quarantine", "2026-08-01 00:00+08", "2026-08-12 00:00+08", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGroupUsageRollupTestTimezone(t)
			db := review29DB(t)
			_, err := db.Exec(`UPDATE usage_stats_repair_control SET raw_safe_from=$1`, tc.boundary)
			require.NoError(t, err)
			review29Exec(t, db, `INSERT INTO groups VALUES(7);
			INSERT INTO usage_group_daily_rollups(bucket_date,group_id,actual_cost) VALUES('2026-08-12',7,10);
			INSERT INTO usage_logs(group_id,created_at,actual_cost) VALUES(7,'2026-08-13 12:00+08',5);
			UPDATE usage_group_rollup_state SET closed_before='2026-08-13'`)
			if tc.quarantined != "" {
				_, err = db.Exec(`INSERT INTO usage_stats_repair_days(timezone_name,day_start,day_end,generation,state)
				VALUES('Asia/Shanghai',$1::timestamptz,$1::timestamptz+INTERVAL '1 day',1,'quarantine')`, tc.quarantined)
				require.NoError(t, err)
			}
			day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
			rows, err := newUsageLogRepositoryWithSQL(nil, db).GetAllGroupUsageSummary(context.Background(), day)
			if tc.unavailable {
				require.ErrorIs(t, err, service.ErrUsageStatsIncomplete)
				require.Nil(t, rows)
			} else {
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, float64(15), rows[0].TotalCost)
			}
		})
	}
}

// Production interleaving: a dirty empty day is claimed, retention of another
// day quarantines it without changing its generation, then the worker fails its
// recompute. Fail must not undo that persistent quarantine.
func TestReview29QueueFailPreservesRetentionQuarantine(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-01 00:00+08';
	INSERT INTO usage_logs(created_at) VALUES('2026-08-10 12:00+08');
	DELETE FROM usage_logs;
	INSERT INTO usage_logs(created_at) VALUES('2026-08-12 12:00+08')`)
	ctx := context.Background()
	queue := NewUsageStatsRepairQueue(db)
	claims, err := queue.ClaimDue(ctx, "worker-a", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	c := claims[0]
	require.Equal(t, "2026-08-10", service.GroupUsageDate(c.DayStart))
	cutoff := time.Date(2026, 8, 13, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	deleted, err := cleanupUsageLogsBatchWithRollupInvalidation(ctx, db, cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Error(t, newDashboardAggregationRepositoryWithSQL(db).RecomputeRange(ctx, c.DayStart, c.DayEnd))
	err = queue.Fail(ctx, c.TimezoneName, c.DayStart, c.Generation, c.Owner, c.Fence, "recompute rejected", time.Second)
	require.ErrorIs(t, err, service.ErrUsageRepairLostLease)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM usage_stats_repair_days WHERE day_start=$1`, c.DayStart).Scan(&state))
	require.Equal(t, "quarantine", state)
}

func TestReview29RepairTriggerAndManualDeletion(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-01 00:00+08';
	INSERT INTO groups VALUES(7);
	INSERT INTO usage_logs(group_id,created_at,actual_cost) VALUES(7,'2026-08-13 12:00+08',5)`)
	ctx := context.Background()
	day := time.Date(2026, 8, 13, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	repo := newDashboardAggregationRepositoryWithSQL(db)
	repo.clock = func() time.Time { return day.AddDate(0, 0, 1).Add(12 * time.Hour) }
	review29Exec(t, db, `UPDATE usage_group_rollup_state SET closed_before='2026-08-13'`)
	require.NoError(t, repo.AggregateRange(ctx, day, day.AddDate(0, 0, 1)))
	require.NoError(t, repo.SyncGroupUsageRollups(ctx, day.AddDate(0, 0, 1)))
	cleanup := &usageCleanupRepository{sql: db}
	count, err := cleanup.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{StartTime: day, EndTime: day.AddDate(0, 0, 1)}, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM usage_stats_repair_days`).Scan(&state))
	require.Equal(t, "dirty", state, "manual deletion leaves complete source and schedules repair")
	require.NoError(t, repo.RecomputeRange(ctx, day, day.AddDate(0, 0, 1)))
	var buckets int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_dashboard_daily WHERE bucket_date='2026-08-13'`).Scan(&buckets))
	require.Zero(t, buckets, "the deleted amount must disappear from the dashboard")
	var cost float64
	require.NoError(t, db.QueryRow(`SELECT COALESCE(SUM(actual_cost),0) FROM usage_group_daily_rollups`).Scan(&cost))
	require.Zero(t, cost, "the deleted amount must disappear from group totals")
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	count, err = cleanup.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{StartTime: future, EndTime: future.AddDate(0, 0, 1)}, 100)
	require.NoError(t, err)
	require.Zero(t, count)
	var boundaryUnchanged bool
	require.NoError(t, db.QueryRow(`SELECT raw_safe_from='2026-08-01 00:00+08'::timestamptz AND generation=1 FROM usage_stats_repair_control`).Scan(&boundaryUnchanged))
	require.True(t, boundaryUnchanged, "a future empty manual cleanup must not change completeness")
	// P1-13: an inconsistent day_end must become explicit quarantine, rather
	// than an ON CONFLICT ... WHERE silently dropping the dirty event.
	review29Exec(t, db, `UPDATE usage_stats_repair_days SET day_end=day_end+INTERVAL '1 hour';
	INSERT INTO usage_logs(created_at) VALUES('2026-08-13 14:00+08')`)
	var reason string
	require.NoError(t, db.QueryRow(`SELECT state,reason_code FROM usage_stats_repair_days`).Scan(&state, &reason))
	require.Equal(t, "quarantine", state)
	require.Equal(t, "day_end_mismatch", reason)
}

func TestReview29QueueReclaimFencesOldWorker(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-01 00:00+08';
	INSERT INTO usage_logs(created_at) VALUES('2026-08-13 12:00+08')`)
	ctx := context.Background()
	queue := NewUsageStatsRepairQueue(db)
	first, err := queue.ClaimDue(ctx, "worker-a", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)
	busy, err := queue.ClaimDue(ctx, "worker-b", 1, time.Minute)
	require.NoError(t, err)
	require.Empty(t, busy)
	review29Exec(t, db, `UPDATE usage_stats_repair_days SET lease_until=clock_timestamp()-INTERVAL '1 second'`)
	second, err := queue.ClaimDue(ctx, "worker-b", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	a, b := first[0], second[0]
	require.Greater(t, b.Fence, a.Fence)
	require.NoError(t, queue.Ack(ctx, b.TimezoneName, b.DayStart, b.Generation, b.Owner, b.Fence))
	require.ErrorIs(t, queue.Fail(ctx, a.TimezoneName, a.DayStart, a.Generation, a.Owner, a.Fence, "stale", time.Second), service.ErrUsageRepairLostLease)
	require.ErrorIs(t, queue.Ack(ctx, a.TimezoneName, a.DayStart, a.Generation, a.Owner, a.Fence), service.ErrUsageRepairLostLease)
}

func TestReview29GroupContinuousPrefixAndExplicitReconciliation(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-01 00:00+08';
	UPDATE usage_group_rollup_state SET closed_before='2026-08-09';
	INSERT INTO groups VALUES(7);
	INSERT INTO usage_logs(group_id,created_at,actual_cost) VALUES
	(7,'2026-08-09 12:00+08',1),(7,'2026-08-10 12:00+08',2),(7,'2026-08-11 12:00+08',8);
	UPDATE usage_stats_repair_days SET state='quarantine' WHERE day_start='2026-08-11 00:00+08'`)
	ctx := context.Background()
	day := time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	repo := newDashboardAggregationRepositoryWithSQL(db)
	repo.clock = func() time.Time { return day.Add(12 * time.Hour) }
	for i := 0; i < 2; i++ {
		require.NoError(t, repo.SyncGroupUsageRollups(ctx, day))
		var watermark string
		require.NoError(t, db.QueryRow(`SELECT closed_before::text FROM usage_group_rollup_state`).Scan(&watermark))
		require.Equal(t, "2026-08-11", watermark, "only the complete continuous prefix is published, including on retry")
	}
	_, err := newUsageLogRepositoryWithSQL(nil, db).GetAllGroupUsageSummary(ctx, day)
	require.ErrorIs(t, err, service.ErrUsageStatsIncomplete)
	// Model an explicit operator reconciliation after the original source has
	// been verified/restored. Normal aggregation must never clear quarantine.
	review29Exec(t, db, `UPDATE usage_stats_repair_days SET state='dirty',reason_code='' WHERE day_start='2026-08-11 00:00+08'`)
	repairStart := day.AddDate(0, 0, -3)
	require.NoError(t, repo.RecomputeRange(ctx, repairStart, repairStart.AddDate(0, 0, 1)))
	rows, err := newUsageLogRepositoryWithSQL(nil, db).GetAllGroupUsageSummary(ctx, day)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, float64(11), rows[0].TotalCost)
}
