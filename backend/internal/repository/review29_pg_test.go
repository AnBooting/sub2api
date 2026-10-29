//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// Every test gets a separate schema on the disposable loopback server.
// Production repository methods and unmodified migration SQL do the work.
func review29DB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := review29DSN(t)
	require.Contains(t, dsn, "host=127.0.0.1", "only use a disposable loopback instance")
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("review29_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); _ = admin.Close() })
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE groups(id BIGINT PRIMARY KEY);
 CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY, group_id BIGINT, created_at TIMESTAMPTZ NOT NULL, actual_cost NUMERIC NOT NULL DEFAULT 0,
 user_id BIGINT NOT NULL DEFAULT 1, input_tokens BIGINT NOT NULL DEFAULT 0, output_tokens BIGINT NOT NULL DEFAULT 0,
 cache_creation_tokens BIGINT NOT NULL DEFAULT 0, cache_read_tokens BIGINT NOT NULL DEFAULT 0,
 total_cost NUMERIC NOT NULL DEFAULT 0, account_stats_cost NUMERIC, account_rate_multiplier NUMERIC, duration_ms INT)`)
	require.NoError(t, err)
	for _, name := range []string{"034_usage_dashboard_aggregation_tables.sql", "107_add_account_cost_to_dashboard_tables.sql", "222_group_usage_daily_rollups.sql", "223_group_usage_rollup_timezone.sql", "259_live_settlements.sql", "260_usage_stats_repair.sql"} {
		data, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = db.Exec(string(data))
		require.NoError(t, err, "execute production migration %s", name)
	}
	return db
}

func review29Exec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	_, err := db.Exec(query)
	require.NoError(t, err)
}

func TestReview29GroupWatermarkMustNotCrossNewlyQuarantinedGap(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-11 00:00+08';
 UPDATE usage_group_rollup_state SET closed_before='2026-08-09';
 INSERT INTO groups VALUES (7);
 INSERT INTO usage_logs(group_id,created_at,actual_cost) VALUES(7,'2026-08-12 12:00+08',5)`)
	today := time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	repo := newDashboardAggregationRepositoryWithSQL(db)
	require.NoError(t, repo.SyncGroupUsageRollups(context.Background(), today))
	var watermark string
	var quarantined int
	require.NoError(t, db.QueryRow(`SELECT closed_before::text FROM usage_group_rollup_state`).Scan(&watermark))
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_stats_repair_days WHERE state='quarantine'`).Scan(&quarantined))
	t.Logf("watermark=%s, quarantined gap days=%d", watermark, quarantined)
	require.LessOrEqual(t, watermark, "2026-08-09", "publication must not skip the two days just declared incomplete")
}

func TestReview29GroupSummaryMustRejectQuarantinedTail(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-01 00:00+08';
 INSERT INTO groups VALUES(7);
 INSERT INTO usage_group_daily_rollups(bucket_date,group_id,actual_cost) VALUES('2026-08-12',7,10);
 INSERT INTO usage_logs(group_id,created_at,actual_cost) VALUES(7,'2026-08-13 12:00+08',5);
 UPDATE usage_group_rollup_state SET closed_before='2026-08-13';
 UPDATE usage_stats_repair_days SET state='quarantine',reason_code='raw_history_incomplete'`)
	today := time.Date(2026, 8, 14, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	repo := newUsageLogRepositoryWithSQL(nil, db)
	rows, err := repo.GetAllGroupUsageSummary(context.Background(), today)
	t.Logf("summary=%+v, error=%v", rows, err)
	require.Error(t, err, "a partial quarantined day must not silently contribute to a normal total")
}

func TestReview29UpgradeDayMustNotSilentlySkipDashboardAggregation(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db := review29DB(t)
	review29Exec(t, db, `UPDATE usage_stats_repair_control SET raw_safe_from='2026-08-14 06:00+08';
 UPDATE usage_group_rollup_state SET closed_before='2026-08-14';
 INSERT INTO usage_logs(group_id,created_at,actual_cost) VALUES(NULL,'2026-08-14 10:15+08',5)`)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM usage_stats_repair_days`).Scan(&state))
	require.Equal(t, "quarantine", state, "an ordinary upgrade-day insert triggers this condition automatically")
	repo := newDashboardAggregationRepositoryWithSQL(db)
	loc := time.FixedZone("CST", 8*3600)
	repo.clock = func() time.Time { return time.Date(2026, 8, 14, 12, 0, 0, 0, loc) }
	err := repo.AggregateRange(context.Background(), time.Date(2026, 8, 14, 10, 0, 0, 0, loc), time.Date(2026, 8, 14, 11, 0, 0, 0, loc))
	var buckets int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM usage_dashboard_hourly`).Scan(&buckets))
	t.Logf("upgrade-day queue state=%s; hourly buckets=%d; AggregateRange error=%v", state, buckets, err)
	require.Error(t, err, "skipping all current-day work must not report success and allow the service watermark to advance")
}

func TestReview29FullMigrationRunner(t *testing.T) {
	dsn := review29DSN(t)
	require.Contains(t, dsn, "host=127.0.0.1")
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	name := fmt.Sprintf("reviewrunner_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	// The disposable server owns this database; stop that server before deleting its data directory.
	db, err := sql.Open("postgres", strings.Replace(dsn, "dbname=reviewfull", "dbname="+name, 1))
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, ApplyMigrations(ctx, db), "all migrations through the production runner on an empty database")
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE filename IN ('259_live_settlements.sql','260_usage_stats_repair.sql')`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, ApplyMigrations(ctx, db), "second application must be idempotent")
	t.Log("production migration runner applied all migrations including 259/260, then repeated successfully")
}

func review29DSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("REVIEW29_PG_DSN")
	if dsn == "" {
		if os.Getenv("REVIEW29_PG_REQUIRED") == "1" {
			t.Fatal("REVIEW29_PG_DSN is required; PostgreSQL regressions must execute in CI")
		}
		t.Skip("set REVIEW29_PG_DSN to a disposable loopback PostgreSQL instance")
	}
	return dsn
}
