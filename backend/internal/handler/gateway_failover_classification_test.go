//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Regression for the missing HA attribution (PRD-04 BE-05: "记录实际服务组和兜底原因";
// C-6 failover is none / account / group / ha).
//
// RoutingFailoverHA was defined, validated, allowed by the migration CHECK and
// rendered by the frontend, but no attribution site could produce it: every one
// classified a served group that differed from the requested one as RoutingFailoverGroup.
// A user could therefore not tell an authorized HA fallback from an ordinary group
// failover, which is exactly what BE-05 requires the record to distinguish.
func TestClassifyGroupFailover(t *testing.T) {
	const requested, normalB, haGroup = int64(10), int64(20), int64(30)
	haIDs := []int64{haGroup, 31}

	for _, tc := range []struct {
		name       string
		requested  int64
		served     int64
		different  bool
		want       string
	}{
		{"same group same account", requested, requested, false, service.RoutingFailoverNone},
		{"same group different account", requested, requested, true, service.RoutingFailoverAccount},
		{"different normal group", requested, normalB, false, service.RoutingFailoverGroup},
		{"different normal group, new account", requested, normalB, true, service.RoutingFailoverGroup},
		{"served the authorized HA candidate", requested, haGroup, false, service.RoutingFailoverHA},
		{"served the second HA candidate", requested, 31, false, service.RoutingFailoverHA},
		// HA wins over group even when the account also changed, otherwise the
		// fallback would be recorded as an ordinary group switch.
		{"HA candidate with a new account", requested, haGroup, true, service.RoutingFailoverHA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyGroupFailover(tc.requested, tc.served, haIDs, tc.different))
		})
	}

	// Without HA candidates the classifier must behave exactly as before, so the
	// change cannot alter existing non-HA routing records.
	require.Equal(t, service.RoutingFailoverGroup,
		classifyGroupFailover(requested, normalB, nil, false))
	require.Equal(t, service.RoutingFailoverNone,
		classifyGroupFailover(requested, requested, nil, false))

	// A group id that is not among the authorized HA candidates is not HA, even if
	// it differs from the requested group.
	require.Equal(t, service.RoutingFailoverGroup,
		classifyGroupFailover(requested, 99, haIDs, false))
}
