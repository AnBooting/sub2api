//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// TestReviewLegacyUpgradeRecordsNewFilename 复现并锁定「旧库升级」的完整 SQL 契约。
//
// 这条用例来自复审包对原集成用例的 sqlmock 重放。原集成用例断言容错路径
// **不写** schema_migrations（require.Zero），而当前执行器必须补记新文件名，
// 所以原断言必然失败（复审实测 Should be zero, but was 1）。
//
// 这里直接驱动真实的 applyMigrationsFS，断言修复后的行为：
//   - 容错成立时补记**恰好一条**新文件名，checksum 是当前文件的值；
//   - 不写第二条（否则下次启动会以 checksum mismatch 失败）。
//
// 注意：这不是 PostgreSQL 集成测试。本机无 Docker，真实 PG 行为未验证。
func TestReviewLegacyUpgradeRecordsNewFilename(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok, "251 必须在改名迁移登记表里")

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prepareMigrationsBootstrapExpectations(mock)
	// 新文件名尚未记录（旧库只记了旧名）。
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE channel_model_pricing ADD COLUMN").WillReturnError(
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))
	mock.ExpectRollback()
	// 旧历史取证：旧文件名记录存在且 checksum 与登记值一致。
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(rule.oldName).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(rule.oldChecksum))
	// 完整性取证：迁移应建出的列必须都已存在。
	for _, col := range rule.requiredColumns {
		mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM information_schema.columns").
			WithArgs(col.table, col.column).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	}
	// 关键断言点：执行器必须补记新文件名，且 checksum 是**当前文件**的值。
	mock.ExpectExec("INSERT INTO schema_migrations").
		WithArgs(name, rule.currentChecksum).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.NoError(t, err, "旧历史与完整性取证都成立时，容错必须放行")
	require.NoError(t, mock.ExpectationsWereMet(),
		"必须发出且只发出一条补记 INSERT（多写一条会让下次启动 checksum mismatch）")
}
