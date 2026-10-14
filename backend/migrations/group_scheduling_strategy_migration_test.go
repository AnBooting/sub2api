package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TS-02 C-4：245 只新增 groups.scheduling_strategy 列（默认 inherit）与 CHECK 约束，
// 不改写任何历史列/数据。
func TestGroupSchedulingStrategyMigration(t *testing.T) {
	content, err := FS.ReadFile("245_group_scheduling_strategy.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE groups ADD COLUMN IF NOT EXISTS scheduling_strategy VARCHAR(20) NOT NULL DEFAULT 'inherit';")
	require.Contains(t, sql,
		"ADD CONSTRAINT groups_scheduling_strategy_check CHECK (scheduling_strategy IN ('inherit', 'stable', 'balanced', 'latency'));")

	// 幂等：先按 pg_constraint 名判定再建，重复执行不报错。
	require.Contains(t, sql, "IF NOT EXISTS ( SELECT 1 FROM pg_constraint")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.scheduling_strategy IS")

	// 不得改写历史：没有 UPDATE/DELETE/DROP COLUMN 之类的破坏性语句。
	comments := regexp.MustCompile("(?ms)/\\*.*?\\*/|--[^\n]*")
	body := strings.ToUpper(string(comments.ReplaceAll(content, nil)))
	require.NotContains(t, body, "DROP COLUMN")
	require.NotContains(t, body, "DROP CONSTRAINT")
	require.NotContains(t, body, "DELETE FROM")
	require.NotRegexp(t, regexp.MustCompile(" UPDATE GROUPS "), body)
}

// 迁移编号必须紧接在既有最新迁移之后，且不改写已有文件。
//
// 245 是 TS-02 C-4 引入该列的迁移；后续 PRD-04/PRD-06 在其之后追加了
// 246（groups.allow_high_availability）与 247（groups.video_price_plans）。
// 本用例因此不再断言「245 恒为最新」（新迁移合法地推进迁移头），改为断言：
// 245 文件本身未被改写，且从 245 起编号连续、无缺号，最新迁移与磁盘一致。
func TestSchedulingStrategyMigrationNumbering(t *testing.T) {
	content, err := FS.ReadFile("245_group_scheduling_strategy.sql")
	require.NoError(t, err)
	require.NotEmpty(t, content)
	// 245 的语义不得被后续迁移改写：仍只新增该列并保留原 CHECK 约束。
	require.Contains(t, string(content), "scheduling_strategy VARCHAR(20) NOT NULL DEFAULT 'inherit'")

	entries, err := FS.ReadDir(".")
	require.NoError(t, err)

	seen := make(map[string]bool)
	latest := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		if len(name) < 3 || name[:3] < "245" {
			continue
		}
		seen[name[:3]] = true
		if name > latest {
			latest = name
		}
	}

	// 245 之后每个编号都必须存在，防止跳号（246/247 已随 PRD-04/PRD-06 追加）。
	require.True(t, seen["245"], "245_group_scheduling_strategy.sql 必须仍然存在")
	require.True(t, seen["246"], "246 迁移缺失：编号必须连续")
	require.True(t, seen["247"], "247 迁移缺失：编号必须连续")
	require.Equal(t, "247_group_video_price_plans.sql", latest,
		"最新迁移必须与磁盘上的最高编号一致")
}
