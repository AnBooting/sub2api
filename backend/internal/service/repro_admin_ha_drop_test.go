//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Regression for the admin HA data-loss defect.
//
// Contract (project-docs/prd/11-共享契约.md:75 and 13-接口文档.md:33): the admin
// "groups" array only replaces the NORMAL chain and the database keeps existing HA;
// an admin request carrying an HA entry must be rejected. The old resolver instead
// switched to "desired wins for HA" as soon as any HA link was declared, so every
// other HA link — including ones the user had enabled — was silently deleted, and
// the deleted rows could not be reconstructed because enabled_by_user_at (the only
// record of a user's own enablement) lived solely in them.
//
// Reproduced before the fix: submitting one HA link left only that link and dropped
// the two the user had enabled.
func TestAdminChainResolutionNeverDropsUserAuthorizations(t *testing.T) {
	at := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	current := []APIKeyGroupLink{
		{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0},
		{GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &at},
		{GroupID: 11, Role: APIKeyGroupRoleHA, Position: 1, EnabledByUserAt: &at},
	}
	// An admin edits the normal chain and (in the old code) also declared one HA.
	desired := []APIKeyGroupLink{
		{GroupID: 2, Role: APIKeyGroupRoleNormal, Position: 0},
		{GroupID: 10, Role: APIKeyGroupRoleHA, Position: 0},
	}

	got, err := ResolveAdminAPIKeyGroupChain(current, desired)
	require.NoError(t, err)

	var normalIDs, haIDs []int64
	for _, link := range got {
		switch link.Role {
		case APIKeyGroupRoleNormal:
			normalIDs = append(normalIDs, link.GroupID)
		case APIKeyGroupRoleHA:
			haIDs = append(haIDs, link.GroupID)
		}
	}
	require.Equal(t, []int64{2}, normalIDs, "the admin edit replaces the normal chain")
	require.Equal(t, []int64{9, 11}, haIDs,
		"both user-enabled HA authorizations must survive; the declared HA must not be created")
	for _, link := range got {
		if link.Role == APIKeyGroupRoleHA {
			require.NotNil(t, link.EnabledByUserAt)
			require.True(t, link.EnabledByUserAt.Equal(at),
				"the user's original enablement timestamp must be preserved")
		}
	}
}

// The admin endpoint must reject HA entries outright rather than silently ignoring
// them: a caller that believes it enabled a fallback needs a 400, not a no-op.
func TestAdminChainValidationRejectsHAEntries(t *testing.T) {
	ha := []APIKeyGroupLink{
		{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0},
		{GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0},
	}
	expected := int64(0)
	err := ValidateAPIKeyGroupChainInput(nil, false, &ha, false, true, false, &expected)
	require.Error(t, err, "an admin groups array carrying HA must be rejected")
	require.Contains(t, err.Error(), "HA_ENTRY_NOT_ALLOWED")

	// Normal-only admin updates keep working.
	normalOnly := []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}}
	require.NoError(t, ValidateAPIKeyGroupChainInput(nil, false, &normalOnly, false, true, false, &expected))

	// The user path is unchanged: it may carry HA because enablement is a user action.
	require.NoError(t, ValidateAPIKeyGroupChainInput(nil, false, &ha, false, false, false, &expected))
}
