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

func TestUsageStatsRepairFailRequiresDirtyState(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		t.Run(string(rune('0'+affected)), func(t *testing.T) {
			db, mock := newSQLMock(t)
			day := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
			// Match the actual executed predicate, independently of the SQL constants.
			mock.ExpectExec(`(?s)UPDATE usage_stats_repair_days.*WHERE timezone_name = \$1 AND day_start = \$2 AND generation = \$3\s+AND claim_owner = \$4 AND claim_fence = \$5 AND state = 'dirty'\s*$`).
				WithArgs("Asia/Shanghai", day, int64(3), "worker-a", int64(7), service.UsageStatsRepairMaxAttempts, "failed", int64(1000)).
				WillReturnResult(sqlmock.NewResult(0, affected))
			err := NewUsageStatsRepairQueue(db).Fail(context.Background(), "Asia/Shanghai", day, 3, "worker-a", 7, "failed", time.Second)
			if affected == 0 {
				require.ErrorIs(t, err, service.ErrUsageRepairLostLease)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageTrendPublicEntriesPreserveCompletenessError(t *testing.T) {
	for _, entry := range []string{"filters", "usage filters"} {
		t.Run(entry, func(t *testing.T) {
			setGroupUsageRollupTestTimezone(t)
			db, mock := newSQLMock(t)
			repo := newUsageLogRepositoryWithSQL(nil, db)
			start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 0, 1)
			mock.ExpectBegin()
			mock.ExpectQuery(`(?s)SELECT\s+EXISTS .*usage_stats_repair_control.*NOT EXISTS .*usage_stats_repair_days`).
				WithArgs("Asia/Shanghai", start, end).
				WillReturnRows(sqlmock.NewRows([]string{"available"}).AddRow(false))
			mock.ExpectRollback()
			var rows []TrendDataPoint
			var err error
			switch entry {
			case "filters":
				rows, err = repo.GetUsageTrendWithFilters(context.Background(), start, end, "day", 0, 0, 0, 0, "", nil, nil, nil)
			default:
				rows, err = repo.GetUsageTrendWithUsageFilters(context.Background(), start, end, "day", UsageLogFilters{})
			}
			require.ErrorIs(t, err, service.ErrUsageStatsIncomplete)
			require.Nil(t, rows)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
