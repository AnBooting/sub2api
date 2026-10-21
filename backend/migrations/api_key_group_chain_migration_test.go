package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAPIKeyGroupChainMigrationPreservesHistory(t *testing.T) {
	raw, err := FS.ReadFile("252_api_key_group_links.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(raw)), " ")
	require.Contains(t, sql, "group_chain_revision BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "UNIQUE (api_key_id, group_id)")
	require.Contains(t, sql, "UNIQUE (api_key_id, role, position)")
	require.Contains(t, sql, "SELECT id, group_id, 'normal', 0 FROM api_keys WHERE group_id IS NOT NULL")
	require.NotContains(t, sql, "UPDATE usage_logs")
	require.NotContains(t, sql, "SELECT id, group_id, 'ha'")
	require.Contains(t, sql, "failover IN ('none', 'account', 'group', 'ha')")
	for _, field := range []string{"requested_group_id BIGINT;", "served_group_id BIGINT;", "failover VARCHAR(16);", "attempt_count INTEGER;"} {
		require.Contains(t, sql, field)
	}
}
