//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestDashboardPartialKeepsSafeTodayAndHidesUnknownTotals(t *testing.T) {
	setGroupUsageRollupTestTimezone(t)
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	day := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	for _, available := range []bool{false, true, true, true} {
		mock.ExpectQuery(`(?s)SELECT.*usage_stats_repair_control.*usage_stats_repair_days`).
			WillReturnRows(sqlmock.NewRows([]string{"available"}).AddRow(available))
	}
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_dashboard_daily\s+WHERE bucket_date`).
		WithArgs(day).WillReturnRows(sqlmock.NewRows([]string{"requests", "input", "output", "cache_create", "cache_read", "cost", "actual_cost", "account_cost", "active_users"}).AddRow(2, 100, 20, 0, 0, 3, 2, 1, 1))
	mock.ExpectQuery(`(?s)SELECT active_users.*FROM usage_dashboard_hourly`).
		WillReturnRows(sqlmock.NewRows([]string{"active_users"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*FROM usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "tokens"}).AddRow(10, 500))
	mock.ExpectCommit()
	stats := &DashboardStats{TotalUsers: 9}
	require.NoError(t, repo.fillDashboardUsageStatsAggregated(context.Background(), stats, day, day.Add(12*time.Hour)))
	data, err := json.Marshal(stats)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Nil(t, payload["total_actual_cost"])
	require.Nil(t, payload["average_duration_ms"])
	require.Equal(t, float64(2), payload["today_actual_cost"])
	require.Equal(t, float64(9), payload["total_users"])
	require.Equal(t, float64(2), payload["rpm"])
	require.Equal(t, map[string]any{"total": false, "today": true, "hourly": true, "performance": true}, payload["usage_availability"])
	require.NoError(t, mock.ExpectationsWereMet())
}
