//go:build unit

package repository

import (
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// The companion file is generated from the unchanged integration scenario:
// only the build tag, function name and DB variable name are changed. This is
// a unit replay of its SQL contract, not a PostgreSQL integration test run.
func TestReviewExistingLegacyIntegrationContract(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	reviewLegacyIntegrationDB = db
	t.Cleanup(func() { _ = db.Close() })
	const name = "251_native_currency_pricing.sql"
	rule := renamedMigrationReplayRules[name]
	mock.ExpectQuery("SELECT checksum FROM schema_migrations").WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(rule.currentChecksum))
	mock.ExpectExec("DELETE FROM schema_migrations").WithArgs(name).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO schema_migrations").WithArgs(rule.oldName, rule.currentChecksum).WillReturnResult(sqlmock.NewResult(0, 1))
	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations").WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE channel_model_pricing ADD COLUMN").WillReturnError(
		errors.New(`constraint "channel_model_pricing_currency_check" already exists (SQLSTATE 42710)`))
	mock.ExpectRollback()
	mock.ExpectQuery("SELECT checksum FROM schema_migrations").WithArgs(rule.oldName).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(rule.oldChecksum))
	// 旧历史成立后还要逐列取证（迁移正文在同一事务里执行，失败即整体回滚）。
	for _, col := range rule.requiredColumns {
		mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM information_schema.columns").
			WithArgs(col.table, col.column).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	}
	// This expectation requires production to actually issue the new history INSERT.
	mock.ExpectExec("INSERT INTO schema_migrations").WithArgs(name, rule.currentChecksum).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_unlock").WithArgs(migrationsAdvisoryLockID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM schema_migrations").WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// The original scenario's t.Cleanup still runs after its require.Zero fails.
	mock.ExpectExec("DELETE FROM schema_migrations").WithArgs(rule.oldName).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO schema_migrations").WithArgs(name, rule.currentChecksum).
		WillReturnError(errors.New("duplicate key already exists"))
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()) })
	reviewOriginalLegacyIntegrationScenario(t)
}
