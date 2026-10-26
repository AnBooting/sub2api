//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// renamedMigrationReplayFixture 描述一次「旧库重放改名迁移」的最小装置：
// schema_migrations 里没有新文件名（模拟旧库只记了旧文件名），迁移正文执行时报重复约束。
// 对**已登记**的改名迁移，装置还会补上「查旧文件名取证 + 补记新文件名」两步，
// 因为执行器现在必须先拿到旧历史证据才允许容错。
func renamedMigrationReplayFixture(t *testing.T, name string, body []byte, execErr error) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	// 默认**不提供**旧历史证据：调用方要么显式给出旧 checksum（正例），
	// 要么就是在验证「缺证据必须失败」的负例。
	return renamedMigrationReplayFixtureWithLegacy(t, name, body, execErr, "")
}

// renamedMigrationReplayFixtureWithLegacy 允许显式指定数据库里旧记录的 checksum：
// 空串 = 没有旧记录（模拟历史缺失），其余 = 旧记录存在且 checksum 为该值。
// skipLegacyHistory 作为 legacyChecksum 的哨兵值：表示这条用例走的是
// 「在 tolerateRenamedMigrationReplay 就已判定不容错」的路径，根本不会访问数据库，
// 因此装置不能声明任何取证期望。
const skipLegacyHistory = "\x00skip"

// expectLegacyHistory 声明「查旧文件名 → 补记新文件名」两步期望。
func expectLegacyHistory(mock sqlmock.Sqlmock, name string, rule renamedMigrationReplayRule, legacyChecksum string) {
	legacyRows := sqlmock.NewRows([]string{"checksum"})
	if legacyChecksum != "" {
		legacyRows = legacyRows.AddRow(legacyChecksum)
	}
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(rule.oldName).
		WillReturnRows(legacyRows)
	if legacyChecksum != rule.oldChecksum {
		return
	}
	// 旧历史成立后还要逐列取证：缺失说明旧迁移没跑完，必须失败。
	for _, col := range rule.requiredColumns {
		mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM information_schema.columns").
			WithArgs(col.table, col.column).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	}
	mock.ExpectExec("INSERT INTO schema_migrations").
		// 补记的必须是**当前文件**的 checksum：写错会让下一次启动以
		// checksum mismatch 直接失败，所以这里不能用 AnyArg 放过。
		WithArgs(name, rule.currentChecksum).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func renamedMigrationReplayFixtureWithLegacy(t *testing.T, name string, body []byte, execErr error, legacyChecksum string) (*sql.DB, sqlmock.Sqlmock) {
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

	// 容错判定：只有「有资格容错」的路径才会走到查旧历史这一步。
	// 反例（未登记 / 错误码不符 / 内容被改过）在 tolerateRenamedMigrationReplay
	// 就已经返回 false，不会访问数据库，因此这里不能声明任何期望。
	if rule, ok := renamedMigrationReplayRules[name]; ok && legacyChecksum != skipLegacyHistory {
		expectLegacyHistory(mock, name, rule, legacyChecksum)
	}

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
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok, "251 必须在改名迁移登记表里")
	db, mock := renamedMigrationReplayFixtureWithLegacy(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`), rule.oldChecksum)

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.NoError(t, err, "登记过的改名迁移在旧库上重放必须被容错")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：未登记的迁移即使报同一个错误码也必须照旧失败。
//
// 这条用例专门隔离「登记表闸门」：文件名**不在**登记表里，但错误码（42710）与
// 内容都刻意做成与已登记的 251 完全相同——否则错误码闸门会先把请求拦下，
// 即使删掉登记表闸门这条用例也照样绿（假绿）。
func TestRenamedMigrationReplayRejectsUnregisteredMigration(t *testing.T) {
	// 用 251 的真实内容，但文件名换成未登记的——保证错误码与内容两个闸门都放行，
	// 唯一能拦住它的就是「未登记」这一条。
	const registered = "251_native_currency_pricing.sql"
	require.NotContains(t, renamedMigrationReplayRules, "999_not_registered_migration.sql",
		"这条用例要求 999 确实未登记")
	body := renamedMigrationBody(t, registered)
	const name = "999_not_registered_migration.sql"
	db, mock := renamedMigrationReplayFixtureWithLegacy(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`), skipLegacyHistory)

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "未登记的迁移不得被容错")
	require.Contains(t, err.Error(), "apply migration "+name)
	// 关键：错误必须来自**原始 DDL 失败**，而不是「走到取证才发现没登记」。
	// 只断言 require.Error 会让「登记表闸门被删掉」的回归照样绿——那时代码会继续
	// 去查旧历史，sqlmock 抛 call was not expected，仍然满足 require.Error。
	require.Contains(t, err.Error(), "already exists", "错误必须是原始 DDL 失败，而不是取证阶段的意外")
	require.NotContains(t, err.Error(), "verify legacy history",
		"未登记迁移不得进入取证流程")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：错误码不在该迁移的允许集合里（251 只允许 42710）必须照旧失败。
func TestRenamedMigrationReplayRejectsUnexpectedErrorCode(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	db, mock := renamedMigrationReplayFixtureWithLegacy(t, name, body,
		errors.New(`pq: relation "channel_model_pricing" already exists (SQLSTATE 42P07)`), skipLegacyHistory)

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "错误码不在允许集合里不得被容错")
	require.Contains(t, err.Error(), "apply migration "+name)
	// 关键：错误必须是**原始的 42P07**，而不是取证阶段的意外错误。
	// 否则「错误码闸门被删掉」的回归会照样绿（走到查库 → sqlmock 报 unexpected → 有 error）。
	require.Contains(t, err.Error(), "42P07", "错误必须原样透传 42P07，而不是取证阶段的意外")
	require.NotContains(t, err.Error(), "verify legacy history",
		"错误码不符时不得进入取证流程")
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：迁移内容在改名之后被改过（校验和不一致）必须照旧失败，不能容错。
func TestRenamedMigrationReplayRejectsModifiedMigrationContent(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := append(renamedMigrationBody(t, name), []byte("\n-- 有人在改名之后改了这个迁移\n")...)
	db, mock := renamedMigrationReplayFixtureWithLegacy(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`), skipLegacyHistory)

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "内容被改过的迁移不得被容错")
	require.Contains(t, err.Error(), "apply migration "+name)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 反例：库里没有旧文件名记录（旧历史缺失）必须照旧失败。
// 重复对象错误不能证明旧迁移完整执行过——251 的第三条语句可能从未运行。
func TestRenamedMigrationReplayRejectsMissingLegacyHistory(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok)

	db, _ := renamedMigrationReplayFixtureWithLegacy(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`), "")

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "旧历史缺失时不得容错")
	require.Contains(t, err.Error(), "not proof that")
	require.Contains(t, err.Error(), rule.oldName)
}

// 反例：数据库里旧记录的 checksum 与登记不一致（历史漂移）必须照旧失败。
func TestRenamedMigrationReplayRejectsLegacyChecksumDrift(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok)

	db, _ := renamedMigrationReplayFixtureWithLegacy(t, name, body,
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`), strings.Repeat("a", 64))

	err := applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "旧 checksum 不一致时不得容错")
	require.Contains(t, err.Error(), "legacy history drift")
	require.Contains(t, err.Error(), rule.oldName)
}

// 反例：查旧历史的查询本身报错（不是 ErrNoRows）必须照旧失败。
// 「查询失败」和「没有记录」是两种不同的失败，都不能被当作证据缺失而放行。
func TestRenamedMigrationReplayRejectsLegacyHistoryQueryError(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE channel_model_pricing ADD COLUMN").WillReturnError(
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))
	mock.ExpectRollback()
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(rule.oldName).
		WillReturnError(errors.New("connection reset by peer"))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "取证查询失败时不得容错")
	require.Contains(t, err.Error(), "verify legacy history")
}

// 反例：取证通过但补记新文件名失败必须照旧失败。
// 否则「容错了却没记」会让每次启动都重复走这条路径，日志持续告警。
func TestRenamedMigrationReplayRejectsLegacyHistoryRecordFailure(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE channel_model_pricing ADD COLUMN").WillReturnError(
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))
	mock.ExpectRollback()
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(rule.oldName).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(rule.oldChecksum))
	for _, col := range rule.requiredColumns {
		mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM information_schema.columns").
			WithArgs(col.table, col.column).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	}
	mock.ExpectExec("INSERT INTO schema_migrations").
		WithArgs(name, rule.currentChecksum).
		WillReturnError(errors.New("disk full"))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "补记失败时不得静默继续")
	require.Contains(t, err.Error(), "record tolerated migration")
}

// 反例：旧历史成立，但这份迁移的目标列**不存在**，必须照旧失败。
//
// 这是 R1 的核心：迁移正文在同一个事务里执行，第 2 条 ADD CONSTRAINT 撞 42710
// 会把第 3 条 ADD COLUMN 一起回滚。旧历史只能证明「旧库当年执行过旧名迁移」，
// 不能证明「新名迁移的每条语句都生效过」。若此时放行，缺失的列会被永久标记为
// 已应用，再也补不上。
func TestRenamedMigrationReplayRejectsMissingRequiredColumn(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	body := renamedMigrationBody(t, name)
	rule, ok := renamedMigrationReplayRules[name]
	require.True(t, ok)
	require.NotEmpty(t, rule.requiredColumns, "251 必须登记完整性判据")

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	prepareMigrationsBootstrapExpectations(mock)
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(name).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}))
	mock.ExpectBegin()
	mock.ExpectExec("ALTER TABLE channel_model_pricing ADD COLUMN").WillReturnError(
		errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`))
	mock.ExpectRollback()
	mock.ExpectQuery("SELECT checksum FROM schema_migrations WHERE filename = \\$1").
		WithArgs(rule.oldName).
		WillReturnRows(sqlmock.NewRows([]string{"checksum"}).AddRow(rule.oldChecksum))
	// 第一列存在，第二列（usage_logs.pricing_snapshot）缺失。
	mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM information_schema.columns").
		WithArgs(rule.requiredColumns[0].table, rule.requiredColumns[0].column).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM information_schema.columns").
		WithArgs(rule.requiredColumns[1].table, rule.requiredColumns[1].column).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WithArgs(migrationsAdvisoryLockID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = applyMigrationsFS(context.Background(), db, fstest.MapFS{name: &fstest.MapFile{Data: body}})
	require.Error(t, err, "目标列缺失时不得把迁移标记为已应用")
	require.Contains(t, err.Error(), "is missing")
	require.Contains(t, err.Error(), rule.requiredColumns[1].column)
	// 绝不能补记：缺列却记成「已应用」会让这个列永久补不上。
	require.NoError(t, mock.ExpectationsWereMet(), "缺失列时不得发出补记 INSERT")
}

// 未登记的迁移名必须被 tolerateRenamedMigrationReplay 拒绝。
//
// 注意这里断言的是**行为契约**而不是某一行代码：判定函数对未登记名有两道独立防线
// （登记表查找失败、以及查到的零值 rule 的 duplicateErrorCodes 为空），任一道都能
// 拦住。红队实测删掉第一道后本用例仍绿——那不是假绿，而是第二道防线确实生效。
// 真正要守住的是「未登记名永远返回 false」这个外部行为。
func TestTolerateRenamedMigrationReplayRejectsUnregisteredName(t *testing.T) {
	const registered = "251_native_currency_pricing.sql"
	rule := renamedMigrationReplayRules[registered]
	// 内容与错误码都按已登记迁移的真实值构造：除「未登记」外没有任何理由拒绝。
	err := errors.New(`pq: constraint "channel_model_pricing_currency_check" for relation "channel_model_pricing" already exists (SQLSTATE 42710)`)

	if _, ok := tolerateRenamedMigrationReplay(registered, rule.oldChecksum, err); !ok {
		t.Fatalf("已登记迁移在错误码与内容都匹配时必须通过判定")
	}
	for _, name := range []string{
		"999_not_registered_migration.sql",
		"",
		"251_native_currency_pricing", // 少扩展名也算未登记
	} {
		if _, ok := tolerateRenamedMigrationReplay(name, rule.oldChecksum, err); ok {
			t.Fatalf("未登记的迁移名 %q 不得通过判定", name)
		}
	}
}

// 错误码闸门：已登记迁移但错误码不在允许集合时必须直接被拒绝。
func TestTolerateRenamedMigrationReplayRejectsUnexpectedCode(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	rule := renamedMigrationReplayRules[name]
	allowed := errors.New(`pq: constraint "x" already exists (SQLSTATE 42710)`)
	// 42P07 不在 251 的允许集合（只有 42710）。
	notAllowed := errors.New(`pq: relation "x" already exists (SQLSTATE 42P07)`)

	if _, ok := tolerateRenamedMigrationReplay(name, rule.oldChecksum, allowed); !ok {
		t.Fatalf("251 必须允许 42710")
	}
	if _, ok := tolerateRenamedMigrationReplay(name, rule.oldChecksum, notAllowed); ok {
		t.Fatalf("251 不得允许 42P07（错误码闸门失效）")
	}
	// 识别不出 SQLSTATE 也必须拒绝。
	if _, ok := tolerateRenamedMigrationReplay(name, rule.oldChecksum, errors.New("boom")); ok {
		t.Fatalf("识别不出 SQLSTATE 时不得通过判定")
	}
}

// 内容闸门：迁移文件在改名之后被改过（checksum 漂移）时必须直接被拒绝。
func TestTolerateRenamedMigrationReplayRejectsChecksumDrift(t *testing.T) {
	const name = "251_native_currency_pricing.sql"
	rule := renamedMigrationReplayRules[name]
	err := errors.New(`pq: constraint "x" already exists (SQLSTATE 42710)`)

	if _, ok := tolerateRenamedMigrationReplay(name, rule.oldChecksum, err); !ok {
		t.Fatalf("内容与登记一致时必须通过判定")
	}
	if _, ok := tolerateRenamedMigrationReplay(name, rule.oldChecksum+"x", err); ok {
		t.Fatalf("checksum 漂移时不得通过判定（内容闸门失效）")
	}
}

// 错误码解析：pq 强类型错误与只剩文本的错误都要能取出 SQLSTATE。
func TestPostgresErrorCodeExtraction(t *testing.T) {
	require.Equal(t, "42710", postgresErrorCode(errors.New(`pq: constraint "x" already exists (SQLSTATE 42710)`)))
	require.Equal(t, "", postgresErrorCode(errors.New("boom")))
	require.Equal(t, "", postgresErrorCode(nil))
}
