
//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// rev-d2 D-3(b): a public group must be reported as accessible even when the user
// record could not be loaded. haGroupAccessSatisfied originally checked user==nil
// BEFORE the public-group exemption, so a nil user marked every public group as
// access_granted=false; the UI then disabled the toggle and said a grant was needed,
// which is exactly the misleading state FE-02 exists to remove (and contradicts K-3:
// a public group needs no prior authorization).
func TestHAGroupAccessSatisfiedExemptsPublicGroupWithoutUser(t *testing.T) {
	public := &Group{ID: 10, Platform: PlatformOpenAI, Status: StatusActive}
	require.True(t, haGroupAccessSatisfied(nil, public),
		"a public group needs no grant, so a missing user must not mark it inaccessible")

	// A subscription group is authorized by the subscription, also without a user load.
	sub := &Group{ID: 11, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: "monthly"}
	require.True(t, haGroupAccessSatisfied(nil, sub))

	// An exclusive group with no user loaded must fail closed.
	exclusive := &Group{ID: 12, Platform: PlatformOpenAI, Status: StatusActive, IsExclusive: true}
	require.False(t, haGroupAccessSatisfied(nil, exclusive),
		"an exclusive group must fail closed when the grant cannot be verified")

	// And an exclusive group WITH a grant is accessible.
	granted := &User{ID: 7, Status: StatusActive, AllowedGroups: []int64{12}}
	require.True(t, haGroupAccessSatisfied(granted, exclusive))
}

// rev-d2 D-3(a): the candidate list must not call GetByID when no exclusive candidate
// exists. Before the fix it loaded the user unconditionally, adding a failure surface
// (500/503) to a list that needs no user at all.
func TestListHAGroupCandidatesNeedsNoUserWhenNothingIsExclusive(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	// nil userRepo: if the implementation still tried to load a user this would panic
	// or fail, which is the point of the assertion.
	svc := NewAPIKeyService(&haChainRepoStub{key: key}, nil,
		&haGroupRepoStub{groups: []Group{*haTestGroup(10, true), *haTestGroup(11, true)}}, nil, nil, nil, nil)

	got, err := svc.ListAPIKeyHAGroupCandidates(context.Background(), 7, 1)
	require.NoError(t, err, "a list with no exclusive candidate must not need a user load")
	require.Len(t, got, 2)
	for _, c := range got {
		require.True(t, c.AccessGranted, "public candidates must be accessible without a user load")
	}
}
