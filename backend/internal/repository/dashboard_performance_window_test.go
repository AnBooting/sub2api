//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// 仪表盘 RPM/TPM 的分母必须是窗口内实际覆盖的分钟数。5 分钟窗口里只有后 2 分钟有
// 数据时除以 5 会把速率系统性低估：4 次请求 / 800 token 应为 2 RPM / 400 TPM，
// 旧实现（写死 /5）只报 0 RPM / 160 TPM。
func TestDashboardPerformanceUsesCoveredMinutesForPartialWindow(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*FROM usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count", "first_log_at"}).
			AddRow(4, 800, now.Add(-2*time.Minute)))

	rpm, tpm, err := repo.getPerformanceStatsAt(context.Background(), 0, now)
	require.NoError(t, err)
	require.Equal(t, int64(2), rpm, "4 次请求 / 2 分钟实际覆盖 = 2 RPM（旧实现 /5 得 0）")
	require.Equal(t, int64(400), tpm, "800 token / 2 分钟实际覆盖 = 400 TPM（旧实现 /5 得 160）")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 整窗口都有数据时分母必须仍是 5，行为与旧实现逐位一致（回归护栏：不能把分母改成
// 别的口径而悄悄改变稳态数字）。
func TestDashboardPerformanceKeepsFiveMinuteDenominatorForFullWindow(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*FROM usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count", "first_log_at"}).
			AddRow(50, 1000, now.Add(-dashboardPerformanceWindow)))

	rpm, tpm, err := repo.getPerformanceStatsAt(context.Background(), 0, now)
	require.NoError(t, err)
	require.Equal(t, int64(10), rpm)
	require.Equal(t, int64(200), tpm)
}

// 窗口内没有任何数据时不得因为分母塌缩而产生非零速率，也不得返回 0 以外的分母。
func TestDashboardPerformanceEmptyWindowStaysZero(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*FROM usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count", "first_log_at"}).
			AddRow(0, 0, nil))

	rpm, tpm, err := repo.getPerformanceStatsAt(context.Background(), 0, now)
	require.NoError(t, err)
	require.Zero(t, rpm)
	require.Zero(t, tpm)
}

// 分母的边界语义单独断言，避免只靠 SQL 往返间接覆盖。
func TestDashboardPerformanceCoveredMinutesBoundaries(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 5.0, dashboardPerformanceCoveredMinutes(now, time.Time{}), "未知起点按整窗口")
	require.Equal(t, 5.0, dashboardPerformanceCoveredMinutes(now, now.Add(-time.Hour)), "起点早于窗口仍是 5")
	require.Equal(t, 5.0, dashboardPerformanceCoveredMinutes(now, now.Add(-dashboardPerformanceWindow)))
	require.Equal(t, 2.5, dashboardPerformanceCoveredMinutes(now, now.Add(-150*time.Second)))
	require.Equal(t, 1.0, dashboardPerformanceCoveredMinutes(now, now), "起点落在 now 时以 1 分钟兜底")
}

// API Key 路径必须与仪表盘共用同一个分母口径（token 组成保持不变）。
func TestDashboardPerformanceAPIKeyUsesCoveredMinutes(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	now := time.Now()

	mock.ExpectQuery(`(?s)SELECT.*COUNT\(\*\).*FROM usage_logs.*api_key_id = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count", "first_log_at"}).
			AddRow(9, 1800, now.Add(-3*time.Minute)))

	rpm, tpm, err := repo.getPerformanceStatsByAPIKey(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(3), rpm, "9 次请求 / 3 分钟实际覆盖 = 3 RPM")
	require.Equal(t, int64(600), tpm)
	require.NoError(t, mock.ExpectationsWereMet())
}
