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
//   - 断言容错路径**不写** schema_migrations（因此不产生多余的数据库往返），
//     运维需按改名对账流程手工补记新文件名，避免每次启动都重试；
//   - finally 恢复 251 的记录。
//
// 另外验证护栏的边界：同一个库里，一个**未登记**的迁移撞同样的重复约束时必须照旧失败。
//
// 需要 Docker（testcontainers 起 PostgreSQL）；本机没有 Docker 时整包 integration 用例会被跳过。
func TestRenamedMigrationReplayOnLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	const newName = "251_native_currency_pricing.sql"
	const oldName = "243_native_currency_pricing.sql"

	body, err := fs.ReadFile(migrations.FS, newName)
	require.NoError(t, err)

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
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM schema_migrations WHERE filename = $1", oldName)
		_, _ = integrationDB.ExecContext(cleanupCtx,
			"INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)", newName, currentChecksum)
	})

	// 用新文件名跑执行器：旧库上重放不得让启动失败。
	err = applyMigrationsFS(ctx, integrationDB, fstest.MapFS{newName: &fstest.MapFile{Data: body}})
	require.NoError(t, err, "旧库上重放改名迁移必须被容错")

	// 容错路径不写 schema_migrations：库里仍只有旧文件名。
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE filename = $1", newName).Scan(&count))
	require.Zero(t, count, "容错路径不应写 schema_migrations（否则会多一次数据库往返）")

	// 护栏边界：未登记的迁移撞同样的重复约束必须照旧失败。
	const unregistered = "999_not_registered_migration.sql"
	unregisteredBody := []byte("ALTER TABLE channel_model_pricing ADD CONSTRAINT channel_model_pricing_currency_check CHECK (currency IN ('USD', 'CNY'));")
	err = applyMigrationsFS(ctx, integrationDB, fstest.MapFS{unregistered: &fstest.MapFile{Data: unregisteredBody}})
	require.Error(t, err, "未登记的迁移必须照旧失败")
	require.Contains(t, err.Error(), "apply migration "+unregistered)
}
