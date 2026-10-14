//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newHAExecutionFixture builds a public (non-exclusive) HA group so the K-2
// access rule is satisfied by the user authorization alone, mirroring PRD-04:
// the user does not need prior access to a public fallback group.
func newHAExecutionFixture() *executionFixture {
	a := int64(10)
	enabledAt := time.Now().Add(-time.Hour)
	f := &executionFixture{
		k: &executionKeyRepo{key: &APIKey{ID: 1, UserID: 2, Status: StatusActive, GroupID: &a,
			Groups: []APIKeyGroupLink{
				{GroupID: 10, Role: APIKeyGroupRoleNormal},
				{GroupID: 30, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &enabledAt},
			}}},
		u:   &executionUserRepo{user: &User{ID: 2, Status: StatusActive}},
		g:   &executionGroupRepo{group: &Group{ID: 30, Status: StatusActive}},
		sub: &executionSubRepo{},
	}
	f.svc = &APIKeyService{apiKeyRepo: f.k, userRepo: f.u, groupRepo: f.g, userSubRepo: f.sub}
	return f
}

func (f *executionFixture) authorizeHA(ids []int64) (*APIKey, *UserSubscription, error) {
	return f.svc.AuthorizeHAGroupExecution(context.Background(), 1, 2, 30, ids)
}

// PRD-04 AC-B2: an actively enabled, admin-configured HA group serves without
// requiring prior normal-group access.
func TestAuthorizeHAGroupExecutionAcceptsUserEnabledGroup(t *testing.T) {
	f := newHAExecutionFixture()
	key, sub, err := f.authorizeHA([]int64{30})
	require.NoError(t, err)
	require.Nil(t, sub, "public non-subscription HA group has no subscription")
	require.NotNil(t, key)
	require.Equal(t, int64(30), *key.GroupID)
	require.Equal(t, int64(30), key.Group.ID)
	require.True(t, key.Group.AccountGroups == nil, "loaded relations must not leak into execution")
}

// PRD-04 AC-B1/AC-B4: without the user's own enablement the group is never used.
func TestAuthorizeHAGroupExecutionRequiresUserEnablement(t *testing.T) {
	f := newHAExecutionFixture()
	f.k.key.Groups[1].EnabledByUserAt = nil
	_, _, err := f.authorizeHA([]int64{30})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}

// PRD-04 AC-B3: a group outside the frozen HA allow-set is rejected, and a normal
// role link can never be reached through the HA gate.
func TestAuthorizeHAGroupExecutionRejectsUnfrozenAndNormalRole(t *testing.T) {
	f := newHAExecutionFixture()
	_, _, err := f.authorizeHA([]int64{10, 20})
	require.ErrorIs(t, err, ErrGroupNotAllowed)

	f = newHAExecutionFixture()
	f.k.key.Groups[1].Role = APIKeyGroupRoleNormal
	_, _, err = f.authorizeHA([]int64{30})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}

// K-2: an exclusive HA group still requires its explicit grant.
func TestAuthorizeHAGroupExecutionKeepsExclusiveGrantRequirement(t *testing.T) {
	f := newHAExecutionFixture()
	f.g.group.IsExclusive = true
	_, _, err := f.authorizeHA([]int64{30})
	require.ErrorIs(t, err, ErrGroupNotAllowed)

	f.u.user.AllowedGroups = []int64{30}
	_, _, err = f.authorizeHA([]int64{30})
	require.NoError(t, err)
}

// A revoked or inactive target must fail closed even when the user enabled it.
func TestAuthorizeHAGroupExecutionFailsClosedOnRevokedTarget(t *testing.T) {
	f := newHAExecutionFixture()
	f.g.group.Status = "disabled"
	_, _, err := f.authorizeHA([]int64{30})
	require.Error(t, err)

	f = newHAExecutionFixture()
	f.u.user.Status = "disabled"
	_, _, err = f.authorizeHA([]int64{30})
	require.ErrorIs(t, err, ErrUserNotActive)

	f = newHAExecutionFixture()
	f.k.err = errors.New("db down")
	_, _, err = f.authorizeHA([]int64{30})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrGroupNotAllowed)
}

