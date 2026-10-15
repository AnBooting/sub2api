package migrate

import (
	"testing"

	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/stretchr/testify/require"
)

// Regression for the OnDelete drift found in review: the ent schema declared
// entsql.OnDelete(entsql.Cascade) on the child's edge.From, which ent ignores, so
// migrate/schema.go carried NO ACTION while migration 244 declares ON DELETE
// CASCADE. Test databases built from this descriptor therefore refused to delete an
// API key or a group that still had a link row.
//
// The annotation now lives on the parent's edge.To (api_key.go / group.go), which is
// the form ent actually honours. This test locks the contract in so a future
// regeneration cannot silently drop it again.
func TestAPIKeyGroupLinkForeignKeyOnDeleteActions(t *testing.T) {
	require.Equal(
		t,
		entschema.Cascade,
		findForeignKeyBySymbol(t, APIKeyGroupLinksTable, "api_key_group_links_api_keys_group_links").OnDelete,
		"api_key_group_links.api_key_id must cascade: migration 244 declares ON DELETE CASCADE",
	)
	require.Equal(
		t,
		entschema.Cascade,
		findForeignKeyBySymbol(t, APIKeyGroupLinksTable, "api_key_group_links_groups_api_key_links").OnDelete,
		"api_key_group_links.group_id must cascade: migration 244 declares ON DELETE CASCADE",
	)
}

// Regression for the nullability mismatch found in review: the ent schema declared
// video_price_plans Optional() while migration 247 creates it JSONB NOT NULL
// DEFAULT '{}'. That made ent emit a nullable column plus dead IsNil/NotNil
// predicates and a Clear method that would have run UPDATE ... SET
// video_price_plans = NULL and failed the not-null constraint.
//
// The field is now non-optional WITH a default, matching the migration and the
// sibling columns added in the same change. "Unconfigured" is the empty object, not
// SQL NULL.
func TestVideoPricePlansColumnIsNotNullable(t *testing.T) {
	col := findColumnByName(t, GroupsTable, "video_price_plans")
	require.NotNil(t, col, "groups.video_price_plans must exist")
	require.False(t, col.Nullable,
		"video_price_plans must be NOT NULL to match migration 247 and the fail-closed intent")
}

func findColumnByName(t *testing.T, table *entschema.Table, name string) *entschema.Column {
	t.Helper()

	for _, col := range table.Columns {
		if col.Name == name {
			return col
		}
	}

	require.Failf(t, "missing column", "table %s should include column %s", table.Name, name)
	return nil
}
