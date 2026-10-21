//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// renamedMigrationReplayFixture 描述一次「旧库重放改名迁移」的最小装置：
// schema_migrations 里没有新文件名（模拟旧库只记了旧文件名），迁移正文执行时报重复约束。
func renamedMigrationReplayFixture(t *testing.T, name string, body []byte, execErr error) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE channel_model_pricing ADD COLUMN").WillReturnError(execErr)
	mock.ExpectRollback()
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	return db, mock
}

func renamedMigrationBody(t *testing.T, name string) []byte {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, name)
	require.NoError(t, err)
	return body
}

// 登记的改名迁移必须与嵌入的迁移文件一致：文件被改过（校验和漂移）时，
// 容错护栏必须失效，所以这里把两者钉在一起。
func TestRenamedMigrationReplayRegistryMatchesEmbeddedFiles(t *testing.T) {
	require.Len(t, renamedMigrationReplayRules, 8, "0.2.17 同步共重命名 8 个迁移")

	expected := map[string]string{
		"251_native_currency_pricing.sql":              "243_native_currency_pricing.sql",
		"252_api_key_group_links.sql":                  "244_api_key_group_links.sql",
		"253_group_scheduling_strategy.sql":            "245_group_scheduling_strategy.sql",
		"254_group_high_availability.sql":              "246_group_high_availability.sql",
		"255_group_video_price_plans.sql":              "247_group_video_price_plans.sql",
		"256_yunmao_orders.sql":                        "248_yunmao_orders.sql",
		"257_video_tasks.sql":                          "249_video_tasks.sql",
		"258_relax_group_link_authorization_check.sql": "250_relax_group_link_authorization_check.sql",
	}
	for name, rule := range renamedMigrationReplayRules {
		require.Equal(t, name, rule.newName)
		require.Equal(t, expected[name], rule.oldName, "改名映射必须与 0.2.17 同步记录一致")

		body, err := fs.ReadFile(migrations.FS, name)
		require.NoError(t, err, "登记的迁移必须存在于嵌入的迁移集中：%s", name)
		require.Len(t, rule.currentChecksum, 64, "%s 的登记校验和必须是完整的 SHA256", name)
		require.Len(t, rule.oldChecksum, 64, "%s 的旧校验和必须是完整的 SHA256", name)
		require.Equal(t, migrationContentChecksum(string(body)), rule.currentChecksum,
			"%s 的登记校验和与文件内容不一致（迁移文件被改过？）", name)

		if len(rule.duplicateErrorCodes) > 0 {
			// 只有内容与旧文件逐字一致的迁移才允许容错：内容变过就一律不容错。
			require.Equal(t, rule.oldChecksum, rule.currentChecksum,
				"%s 允许容错，但内容与重编号前不一致，不得放行", name)
		}
	}
}

// 正例：登记过的改名迁移在旧库上重放，撞重复约束时容错并继续启动。
func TestRenamedMigrationReplayToleratesRegisteredDuplicateConstraint(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	db, mock := renamedMigrationReplayFixture(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.NoError(t, err, "登记过的改名迁移在旧库上重放必须被容错")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：未登记的迁移即使报同一个错误码也必须照旧失败。
func TestRenamedMigrationReplayRejectsUnregisteredMigration(t *testing.T) {
	const name = "999_not_registered_migration.sql"
	body := []byte("ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS currency varchar(3) NOT NULL DEFAULT 'USD';")
	db, mock := renamedMigrationReplayFixture(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "未登记的迁移不得被容错")
	require.Contains(t, err.Error(), "apply migration "+name)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：错误码不在该迁移的允许集合里（251 只允许 42710）必须照旧失败。
func TestRenamedMigrationReplayRejectsUnexpectedErrorCode(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	db, mock := renamedMigrationReplayFixture(t, name, body,
		errors.New(`pq: relation "channel_model_pricing" already exists (SQLSTATE 42P07)`))

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "错误码不在允许集合里不得被容错")
	require.Contains(t, err.Error(), "apply migration "+name)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：迁移内容在改名之后被改过（校验和不一致）必须照旧失败，不能容错。
func TestRenamedMigrationReplayRejectsModifiedMigrationContent(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := append(renamedMigrationBody(t, name), []byte("\n-- 有人在改名之后改了这个迁移\n")...)
	db, mock := renamedMigrationReplayFixture(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "内容被改过的迁移不得被容错")
	require.Contains(t, err.Error(), "apply migration "+name)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 错误码解析：pq 强类型错误与只剩文本的错误都要能取出 SQLSTATE。
func TestPostgresErrorCodeExtraction(t *testing.T) {
	require.Equal(t, "42710", postgresErrorCode(errors.New(`pq: constraint "x" already exists (SQLSTATE 42710)`)))
	require.Equal(t, "", postgresErrorCode(errors.New("boom")))
	require.Equal(t, "", postgresErrorCode(nil))
}
