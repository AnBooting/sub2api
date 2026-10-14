//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type executionKeyRepo struct {
	APIKeyRepository
	key   *APIKey
	err   error
	calls int
}

func (r *executionKeyRepo) GetByID(context.Context, int64) (*APIKey, error) {
	r.calls++
	return r.key, r.err
}

type executionUserRepo struct {
	UserRepository
	user *User
	err  error
}

func (r *executionUserRepo) GetByID(context.Context, int64) (*User, error) { return r.user, r.err }

type executionGroupRepo struct {
	GroupRepository
	group *Group
	err   error
}

func (r *executionGroupRepo) GetByID(context.Context, int64) (*Group, error) { return r.group, r.err }

type executionSubRepo struct {
	UserSubscriptionRepository
	sub             *UserSubscription
	err             error
	userID, groupID int64
	calls           int
}

func (r *executionSubRepo) GetActiveByUserIDAndGroupID(_ context.Context, u, g int64) (*UserSubscription, error) {
	r.calls++
	r.userID = u
	r.groupID = g
	return r.sub, r.err
}

type executionFixture struct {
	svc *APIKeyService
	k   *executionKeyRepo
	u   *executionUserRepo
	g   *executionGroupRepo
	sub *executionSubRepo
}

func newExecutionFixture() *executionFixture {
	a := int64(10)
	override := 7
	f := &executionFixture{
		k:   &executionKeyRepo{key: &APIKey{ID: 1, UserID: 2, Status: StatusActive, GroupID: &a, Groups: []APIKeyGroupLink{{GroupID: 10, Role: APIKeyGroupRoleNormal}, {GroupID: 20, Role: APIKeyGroupRoleNormal, Position: 1}}}},
		u:   &executionUserRepo{user: &User{ID: 2, Status: StatusActive, AllowedGroups: []int64{20}, UserGroupRPMOverride: &override}},
		g:   &executionGroupRepo{group: &Group{ID: 20, Status: StatusActive, IsExclusive: true}},
		sub: &executionSubRepo{sub: &UserSubscription{ID: 30, UserID: 2, GroupID: 20, Status: SubscriptionStatusActive, StartsAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour)}},
	}
	f.svc = &APIKeyService{apiKeyRepo: f.k, userRepo: f.u, groupRepo: f.g, userSubRepo: f.sub}
	return f
}
func (f *executionFixture) authorize() (*APIKey, *UserSubscription, error) {
	return f.svc.AuthorizeNormalGroupExecution(context.Background(), 1, 2, 20, []int64{10, 20})
}

func TestAuthorizeNormalGroupExecutionRejectsRevokedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*executionFixture)
		want   error
	}{
		{"owner", func(f *executionFixture) { f.k.key.UserID = 9 }, ErrInsufficientPerms},
		{"disabled key", func(f *executionFixture) { f.k.key.Status = StatusAPIKeyDisabled }, nil},
		{"expired key", func(f *executionFixture) { v := time.Now().Add(-time.Hour); f.k.key.ExpiresAt = &v }, ErrAPIKeyExpired},
		{"quota", func(f *executionFixture) { f.k.key.Quota = 1; f.k.key.QuotaUsed = 1 }, ErrAPIKeyQuotaExhausted},
		{"removed link", func(f *executionFixture) { f.k.key.Groups = f.k.key.Groups[:1] }, ErrGroupNotAllowed},
		{"HA only no legacy fallback", func(f *executionFixture) {
			*f.k.key.GroupID = 20
			f.k.key.Groups = []APIKeyGroupLink{{GroupID: 20, Role: APIKeyGroupRoleHA}}
		}, ErrGroupNotAllowed},
		{"user disabled", func(f *executionFixture) { f.u.user.Status = "disabled" }, ErrUserNotActive},
		{"user deleted", func(f *executionFixture) { v := time.Now(); f.u.user.DeletedAt = &v }, ErrUserNotActive},
		{"group disabled", func(f *executionFixture) { f.g.group.Status = "disabled" }, nil},
		{"exclusive revoked", func(f *executionFixture) { f.u.user.AllowedGroups = nil }, ErrGroupNotAllowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newExecutionFixture()
			tc.change(f)
			k, s, err := f.authorize()
			require.Error(t, err)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			}
			require.Nil(t, k)
			require.Nil(t, s)
		})
	}
}
func TestAuthorizeNormalGroupExecutionFrozenSetAndLegacy(t *testing.T) {
	f := newExecutionFixture()
	_, _, err := f.svc.AuthorizeNormalGroupExecution(context.Background(), 1, 2, 20, []int64{10})
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	require.Zero(t, f.k.calls)
	f.k.key.Groups = nil
	*f.k.key.GroupID = 20
	k, sub, err := f.authorize()
	require.NoError(t, err)
	require.Equal(t, int64(20), *k.GroupID)
	require.Nil(t, sub)
	f.g.group.IsExclusive = false
	f.u.user.AllowedGroups = nil
	f.u.user.RestrictPublicGroups = true
	_, _, err = f.authorize()
	require.NoError(t, err)
	*f.k.key.GroupID = 10
	_, _, err = f.authorize()
	require.ErrorIs(t, err, ErrGroupNotAllowed)
}
func TestAuthorizeNormalGroupExecutionSubscriptionIsolation(t *testing.T) {
	f := newExecutionFixture()
	f.g.group.SubscriptionType = SubscriptionTypeSubscription
	f.u.user.AllowedGroups = nil
	f.k.key.User = &User{Subscriptions: []UserSubscription{{ID: 99, UserID: 2, GroupID: 10}}}
	k, sub, err := f.authorize()
	require.NoError(t, err)
	require.Equal(t, int64(20), *k.GroupID)
	require.Equal(t, int64(30), sub.ID)
	require.Equal(t, int64(20), f.sub.groupID)
	require.Equal(t, int64(2), f.sub.userID)
	f.g.group.SubscriptionType = "standard"
	f.g.group.IsExclusive = false
	_, sub, err = f.authorize()
	require.NoError(t, err)
	require.Nil(t, sub)
	require.Equal(t, 1, f.sub.calls)
	for _, change := range []func(*UserSubscription){func(s *UserSubscription) { s.GroupID = 10 }, func(s *UserSubscription) { s.UserID = 9 }, func(s *UserSubscription) { s.Status = SubscriptionStatusSuspended }, func(s *UserSubscription) { s.ExpiresAt = time.Now().Add(-time.Hour) }, func(s *UserSubscription) { s.StartsAt = time.Now().Add(time.Hour) }} {
		f := newExecutionFixture()
		f.g.group.SubscriptionType = SubscriptionTypeSubscription
		change(f.sub.sub)
		k, sub, err := f.authorize()
		require.ErrorIs(t, err, ErrSubscriptionInvalid)
		require.Nil(t, k)
		require.Nil(t, sub)
	}
}
func TestAuthorizeNormalGroupExecutionFailsClosedOnRepositoryErrors(t *testing.T) {
	sentinel := errors.New("database unavailable")
	for _, which := range []string{"key", "user", "group", "subscription"} {
		t.Run(which, func(t *testing.T) {
			f := newExecutionFixture()
			f.g.group.SubscriptionType = SubscriptionTypeSubscription
			switch which {
			case "key":
				f.k.err = sentinel
			case "user":
				f.u.err = sentinel
			case "group":
				f.g.err = sentinel
			case "subscription":
				f.sub.err = sentinel
			}
			k, s, err := f.authorize()
			require.ErrorIs(t, err, sentinel)
			require.Nil(t, k)
			require.Nil(t, s)
		})
	}
	for _, which := range []string{"key", "user", "group", "subscription"} {
		t.Run("nil "+which, func(t *testing.T) {
			f := newExecutionFixture()
			f.g.group.SubscriptionType = SubscriptionTypeSubscription
			switch which {
			case "key":
				f.svc.apiKeyRepo = nil
			case "user":
				f.svc.userRepo = nil
			case "group":
				f.svc.groupRepo = nil
			case "subscription":
				f.svc.userSubRepo = nil
			}
			_, _, err := f.authorize()
			require.ErrorIs(t, err, ErrBillingServiceUnavailable)
		})
	}
	f := newExecutionFixture()
	f.g.group.SubscriptionType = SubscriptionTypeSubscription
	f.sub.sub = nil
	_, _, err := f.authorize()
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
}
func TestAuthorizeNormalGroupExecutionOwnsResultAndPreservesFrozenBundle(t *testing.T) {
	f := newExecutionFixture()
	f.g.group.SubscriptionType = SubscriptionTypeSubscription
	now := time.Now()
	limit := 3.0
	f.sub.sub.DailyWindowStart = &now
	f.g.group.DailyLimitUSD = &limit
	f.g.group.ModelRouting = map[string][]int64{"m": {8}}
	f.k.key.IPWhitelist = []string{"127.0.0.1"}
	f.k.key.Groups[1].Group = f.g.group
	bundle := &RequestBillingContext{requestID: "original", normalGroupIDs: []int64{10, 20}}
	ctx := withGatewayRequestBilling(context.Background(), bundle)
	k, sub, err := f.svc.AuthorizeNormalGroupExecution(ctx, 1, 2, 20, bundle.NormalGroupIDs())
	require.NoError(t, err)
	require.Same(t, bundle, GatewayRequestBillingFromContext(ctx))
	require.Equal(t, []int64{10, 20}, bundle.NormalGroupIDs())
	require.NotSame(t, f.k.key, k)
	require.NotSame(t, f.u.user, k.User)
	require.NotSame(t, f.g.group, k.Group)
	require.NotSame(t, f.sub.sub, sub)
	require.Nil(t, k.User.UserGroupRPMOverride)
	require.Equal(t, 7, *f.u.user.UserGroupRPMOverride)
	*k.GroupID = 999
	k.User.AllowedGroups[0] = 999
	*k.Group.DailyLimitUSD = 99
	k.Group.ModelRouting["m"][0] = 99
	k.IPWhitelist[0] = "bad"
	*sub.DailyWindowStart = time.Time{}
	k.Groups[1].Group.ModelRouting["m"][0] = 77
	require.Equal(t, int64(10), *f.k.key.GroupID)
	require.Equal(t, int64(20), f.u.user.AllowedGroups[0])
	require.Equal(t, 3.0, *f.g.group.DailyLimitUSD)
	require.Equal(t, int64(8), f.g.group.ModelRouting["m"][0])
	require.Equal(t, "127.0.0.1", f.k.key.IPWhitelist[0])
	require.Equal(t, now, *f.sub.sub.DailyWindowStart)
}
