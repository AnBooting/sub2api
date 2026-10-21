package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TS-02 C-4：253 只新增 groups.scheduling_strategy 列（默认 inherit）与 CHECK 约束，
// 不改写任何历史列/数据。
func TestGroupSchedulingStrategyMigration(t *testing.T) {
	content, err := FS.ReadFile("253_group_scheduling_strategy.sql")
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
// 253 是 TS-02 C-4 引入该列的迁移；后续 PRD-04/PRD-06 在其之后追加了
// 254（groups.allow_high_availability）与 255（groups.video_price_plans）。
// 本用例因此不再断言「253 恒为最新」（新迁移合法地推进迁移头），改为断言：
// 253 文件本身未被改写，且从 251 起编号连续、无缺号，最新迁移与磁盘一致。
//
// 0.2.17 上游同步：原 243–250 与上游 243/244/245（gemini_cached_contents 系列）
// 撞号，已整体重编号到 251–258（映射表见 project-docs/上游同步说明.md）。
// 246–250 被刻意留空，以免上游后续从 246 继续发号时再次撞号。
func TestSchedulingStrategyMigrationNumbering(t *testing.T) {
	content, err := FS.ReadFile("253_group_scheduling_strategy.sql")
	require.NoError(t, err)
	require.NotEmpty(t, content)
	// 253 的语义不得被后续迁移改写：仍只新增该列并保留原 CHECK 约束。
	require.Contains(t, string(content), "scheduling_strategy VARCHAR(20) NOT NULL DEFAULT 'inherit'")

	entries, err := FS.ReadDir(".")
	require.NoError(t, err)

	seen := make(map[string]bool)
	allNumbers := make(map[string]bool)
	latest := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		if len(name) < 3 {
			continue
		}
		allNumbers[name[:3]] = true
		if name[:3] < "251" {
			continue
		}
		seen[name[:3]] = true
		if name > latest {
			latest = name
		}
	}

	// 251 之后每个编号都必须存在，防止跳号（252 建 api_key_group_links，
	// 253 引入 scheduling_strategy，254/255 随 PRD-04/PRD-06 追加，
	// 256 随 TS-03 白标充值中心追加，257 随 TS-06 视频任务表追加，
	// 258 修正 api_key_group_links 的授权 CHECK 与写入契约矛盾）。
	require.True(t, seen["251"], "251_native_currency_pricing.sql 必须仍然存在")
	require.True(t, seen["252"], "252 迁移缺失：编号必须连续")
	require.True(t, seen["253"], "253_group_scheduling_strategy.sql 必须仍然存在")
	require.True(t, seen["254"], "254 迁移缺失：编号必须连续")
	require.True(t, seen["255"], "255 迁移缺失：编号必须连续")
	require.True(t, seen["256"], "256 迁移缺失：编号必须连续")
	require.True(t, seen["257"], "257 迁移缺失：编号必须连续")
	require.True(t, seen["258"], "258 迁移缺失：编号必须连续")
	require.Equal(t, "258_relax_group_link_authorization_check.sql", latest,
		"最新迁移必须与磁盘上的最高编号一致")

	// 246–250 必须保持为空档：0.2.17 同步把本 fork 的迁移整体后移到 251 起，
	// 空出来的号留给上游后续继续发号，避免再次撞号。
	for _, reserved := range []string{"246", "247", "248", "249", "250"} {
		require.False(t, allNumbers[reserved],
			"%s 必须留空：0.2.17 同步后本 fork 的迁移从 251 起号", reserved)
	}
}
