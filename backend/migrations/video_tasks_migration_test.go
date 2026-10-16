package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// PRD-06/TS-06：249 只新增 video_tasks 表（本站视频任务账本），不改写任何历史列/数据。
func TestVideoTasksMigration(t *testing.T) {
	content, err := FS.ReadFile("249_video_tasks.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS video_tasks (")
	// 归属关系（PRD-06 §2.3 / BE-03）必须可追溯：用户、Key、实际服务分组、承载账号。
	for _, column := range []string{
		"task_no VARCHAR(64) NOT NULL",
		"user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE",
		"api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE",
		"group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE",
		// 必须是 TEXT：该列保存完整的结算占位 key，截断会导致重复扣费
		// 或任务静默登记失败（见 service.VideoTask.BillingKey）。
		"upstream_task_id TEXT NOT NULL",
		"account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL",
		"billing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb",
	} {
		require.Contains(t, sql, column, "video_tasks 缺少归属/快照列: %s", column)
	}

	// 最小生命周期：processing / completed / failed（PRD-06 §2.4）。
	require.Contains(t, sql,
		"CHECK (status IN ('processing', 'completed', 'failed'))")

	// 尝试次数与错误留痕，供「上游不明保持 processing」的可观测性使用。
	require.Contains(t, sql, "attempts INTEGER NOT NULL DEFAULT 0")
	require.Contains(t, sql, "last_error TEXT")
	require.Contains(t, sql, "next_check_at TIMESTAMPTZ")
	require.Contains(t, sql, "settled_at TIMESTAMPTZ")

	// 幂等：同一 Key 的同一上游任务只能有一行。
	require.Contains(t, sql, "UNIQUE (user_id, api_key_id, upstream_task_id)")

	// worker 扫描索引与归属索引。
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS video_tasks_due_idx")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS video_tasks_owner_created_idx")

	// 不得改写历史：没有 UPDATE/DELETE/DROP COLUMN 之类的破坏性语句。
	comments := regexp.MustCompile("(?ms)/\\*.*?\\*/|--[^\n]*")
	body := strings.ToUpper(string(comments.ReplaceAll(content, nil)))
	require.NotContains(t, body, "DROP COLUMN")
	require.NotContains(t, body, "DROP CONSTRAINT")
	require.NotContains(t, body, "DELETE FROM")
	require.NotContains(t, body, "DROP TABLE VIDEO_TASKS")
	require.NotRegexp(t, regexp.MustCompile(" UPDATE "), body)
}
