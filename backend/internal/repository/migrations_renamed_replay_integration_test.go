//go:build integration

package repository

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// TestRenamedMigrationReplayOnLegacyDatabase 用真实 PostgreSQL 走一遍旧库升级路径。
//
// 场景：库在 0.2.17 重编号之前就升级过 —— schema_migrations 里记的是旧文件名
// （243_native_currency_pricing.sql），channel_model_pricing 上也已经存在同名约束
// （channel_model_pricing_currency_check）。升级到含 251 的新版本时，执行器按新文件名
// 查不到记录而重放，撞上已存在的约束（SQLSTATE 42710）会让服务启动直接失败。
//
// 这里用共享测试库里的**真实对象状态**（约束确实存在）模拟旧库：
//   - 把 schema_migrations 里 251 的记录换成旧文件名 243（模拟重命名前的历史）；
//   - 只用 251 的迁移集跑一遍执行器，断言不报错；
//   - 断言容错路径**补记**了新文件名：新名记录恰好一条（不是「不写库」——不写库会让
//     每次启动都重新走一遍重放），且补记的 checksum 等于当前文件内容的 SHA256；
//   - 再跑一次执行器，断言不重放：新名记录仍恰好一条，旧文件名记录也原样保留；
//   - finally 恢复 251 的记录。
//
// 另外验证护栏的边界：同一个库里，一个**未登记**的迁移撞同样的重复约束时必须照旧失败。
//
// 需要 Docker（testcontainers 起 PostgreSQL）；本机没有 Docker 时整包 integration 用例会被跳过。
func TestRenamedMigrationReplayOnLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	const newName = "251_native_currency_pricing.sql"
	const oldName = "243_native_currency_pricing.sql"

	// countSchemaMigrations 数出某个文件名在 schema_migrations 里的记录条数。
	countSchemaMigrations := func(filename string) int {
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE filename = $1", filename).Scan(&count))
		return count
	}

	body, err := fs.ReadFile(migrations.FS, newName)
	require.NoError(t, err)

	rule, ok := renamedMigrationReplayRules[newName]
	require.True(t, ok, "251 必须在改名迁移登记表里")
	require.Equal(t, oldName, rule.oldName)
	// 执行器按 trim 后的内容计算 checksum（见 migrationContentChecksum），下面用同一口径比对。
	fileChecksum := migrationContentChecksum(string(body))
	require.Equal(t, rule.currentChecksum, fileChecksum, "登记的当前校验和必须与嵌入文件一致")

	var currentChecksum string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT checksum FROM schema_migrations WHERE filename = $1", newName).Scan(&currentChecksum))
	require.NotEmpty(t, currentChecksum, "共享测试库应已应用 251 迁移")

	// 模拟旧库：删掉新文件名的记录，写回旧文件名。
	if _, err := integrationDB.ExecContext(ctx, "DELETE FROM schema_migrations WHERE filename = $1", newName); err != nil {
		require.NoError(t, err)
	}
	if _, err := integrationDB.ExecContext(ctx,
		"INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)", oldName, currentChecksum); err != nil {
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		// 先删模拟出来的旧名记录，再删容错路径可能补记的新名记录，最后把 251 的
		// 原始记录放回去；先删后插保证无论用例在哪一步失败都不会留下脏数据。
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM schema_migrations WHERE filename = $1", oldName)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM schema_migrations WHERE filename = $1", newName)
		_, _ = integrationDB.ExecContext(cleanupCtx,
			"INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)", newName, currentChecksum)
	})

	// 用新文件名跑执行器：旧库上重放不得让启动失败。
	err = applyMigrationsFS(ctx, integrationDB, fstest.MapFS{newName: &fstest.MapFile{Data: body}})
	require.NoError(t, err, "旧库上重放改名迁移必须被容错")

	// 容错路径必须补记新文件名，而且恰好一条。
	// 上轮这里断言的是 require.Zero（「容错路径不写 schema_migrations」），执行器改成
	// 自动补记之后该断言必然失败：正常完成修复就得到 1。缺了这条记录，每次启动都会
	// 重新走一遍重放（每次都多一次注定失败的事务），所以「不写库」不再是可接受行为。
	require.Equal(t, 1, countSchemaMigrations(newName), "容错路径必须补记且只补记一条新文件名记录")

	// 护栏边界：未登记的迁移撞同样的重复约束必须照旧失败。
	const unregistered = "999_not_registered_migration.sql"
	unregisteredBody := []byte("ALTER TABLE channel_model_pricing ADD CONSTRAINT channel_model_pricing_currency_check CHECK (currency IN ('USD', 'CNY'));")
	err = applyMigrationsFS(ctx, integrationDB, fstest.MapFS{unregistered: &fstest.MapFile{Data: unregisteredBody}})
	require.Error(t, err, "未登记的迁移必须照旧失败")
	require.Contains(t, err.Error(), "apply migration "+unregistered)

	// 以下两条断言只靠真实 PostgreSQL 才能验证：需要读回补记行的 checksum 并再跑一次
	// 执行器。sqlmock 重放装置（review_legacy_integration_contract_test.go）只钉到上面
	// 那条「新名记录恰好一条」的 COUNT 查询，因此它们排在护栏边界之后。

	// 补记的 checksum 必须等于当前文件内容的 SHA256：写错会让下一次启动以
	// checksum mismatch 直接失败，等于把容错变成一次性的定时炸弹。
	var recordedChecksum string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT checksum FROM schema_migrations WHERE filename = $1", newName).Scan(&recordedChecksum))
	require.Equal(t, fileChecksum, recordedChecksum,
		"补记的 checksum 必须等于当前文件内容的 SHA256")

	// 补记之后再跑一次：新名记录已存在且 checksum 一致，执行器必须直接跳过，
	// 不得重放迁移、也不得再插第二条记录（否则每次启动都会重复执行）。
	err = applyMigrationsFS(ctx, integrationDB, fstest.MapFS{newName: &fstest.MapFile{Data: body}})
	require.NoError(t, err, "补记之后再次执行必须直接跳过")
	require.Equal(t, 1, countSchemaMigrations(newName), "再次执行不得产生第二条新文件名记录")
	require.Equal(t, 1, countSchemaMigrations(oldName), "不得删除旧文件名记录（运维对账仍需要它）")
}
