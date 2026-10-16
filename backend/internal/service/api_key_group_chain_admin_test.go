//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type adminChainTransactionStub struct {
	APIKeyRepository
	key     *APIKey
	updated bool
}

func (s *adminChainTransactionStub) WithGroupChainTransaction(ctx context.Context, id int64, expected *int64, fn func(context.Context, *APIKey) error) error {
	if expected == nil || *expected != s.key.GroupChainRevision {
		return ErrGroupChainConflict
	}
	copy := *s.key
	copy.Groups = CloneAPIKeyGroupLinks(s.key.Groups)
	return fn(ctx, &copy)
}
func (s *adminChainTransactionStub) GetByID(context.Context, int64) (*APIKey, error) {
	copy := *s.key
	copy.Groups = CloneAPIKeyGroupLinks(s.key.Groups)
	return &copy, nil
}

func (s *adminChainTransactionStub) Update(ctx context.Context, key *APIKey, fields APIKeyUpdateFields) error {
	if fields.Groups && (fields.ExpectedGroupChainRevision == nil || *fields.ExpectedGroupChainRevision != s.key.GroupChainRevision) {
		return ErrGroupChainConflict
	}
	desired := key.Groups
	normalOnly := fields.NormalGroupsOnly
	if fields.GroupID && !fields.Groups {
		desired = NormalAPIKeyGroupLinks(key.GroupID)
		normalOnly = true
	}
	key.GroupChainRevision = s.key.GroupChainRevision
	resolved, err := ResolveAPIKeyGroupChain(s.key.Groups, desired, normalOnly)
	if err != nil {
		return err
	}
	if !APIKeyGroupChainsEqual(s.key.Groups, resolved) {
		key.GroupChainRevision++
	}
	key.Groups = resolved
	key.GroupID = FirstNormalAPIKeyGroupID(resolved)
	s.updated = true
	s.key = key
	return nil
}

func TestAPIKeyGroupChainAdminCASPrecedesGrantAndUsageReset(t *testing.T) {
	repo := &adminChainTransactionStub{key: &APIKey{ID: 1, UserID: 7, Key: "k", GroupChainRevision: 2, Usage5h: 12}}
	users := &userRepoStubForGroupUpdate{}
	svc := &adminServiceImpl{apiKeyRepo: repo, userRepo: users, groupRepo: &groupRepoStubForGroupUpdate{group: &Group{ID: 9, Status: StatusActive, IsExclusive: true}}}
	stale := int64(1)
	_, err := svc.AdminUpdateAPIKeyGroups(context.Background(), 1, AdminUpdateAPIKeyGroupsInput{Groups: []APIKeyGroupLink{{GroupID: 9, Role: "normal"}}, ExpectedGroupChainRevision: &stale, ResetRateLimitUsage: true})
	require.ErrorIs(t, err, ErrGroupChainConflict)
	require.False(t, users.addGroupCalled)
	require.False(t, repo.updated)
	require.Equal(t, float64(12), repo.key.Usage5h)
	current := int64(2)
	_, err = svc.AdminUpdateAPIKeyGroups(context.Background(), 1, AdminUpdateAPIKeyGroupsInput{Groups: []APIKeyGroupLink{{GroupID: 9, Role: "normal"}}, ExpectedGroupChainRevision: &current})
	require.NoError(t, err)
	require.True(t, users.addGroupCalled)
	require.True(t, repo.updated)
	require.Equal(t, int64(3), repo.key.GroupChainRevision)
	require.Equal(t, float64(12), repo.key.Usage5h)
}

func TestAPIKeyGroupChainAdminNormalClearPreservesHAAndSubscriptionGate(t *testing.T) {
	at := time.Now().UTC()
	repo := &adminChainTransactionStub{key: &APIKey{ID: 1, UserID: 7, Key: "k", Groups: []APIKeyGroupLink{{GroupID: 1, Role: "normal"}, {GroupID: 9, Role: "ha", EnabledByUserAt: &at}}}}
	rev := int64(0)
	svc := &adminServiceImpl{apiKeyRepo: repo}
	_, err := svc.AdminUpdateAPIKeyGroups(context.Background(), 1, AdminUpdateAPIKeyGroupsInput{Groups: []APIKeyGroupLink{}, ExpectedGroupChainRevision: &rev})
	require.NoError(t, err)
	require.Nil(t, repo.key.GroupID)
	require.Len(t, repo.key.Groups, 1)
	require.Equal(t, "ha", repo.key.Groups[0].Role)
	require.True(t, repo.key.Groups[0].EnabledByUserAt.Equal(at))
	users := &userRepoStubForGroupUpdate{}
	repo.updated = false
	svc.userRepo = users
	svc.groupRepo = &groupRepoStubForGroupUpdate{group: &Group{ID: 8, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription, IsExclusive: true}}
	svc.userSubRepo = &userSubRepoStubForGroupUpdate{getActiveErr: ErrSubscriptionNotFound}
	rev = repo.key.GroupChainRevision
	_, err = svc.AdminUpdateAPIKeyGroups(context.Background(), 1, AdminUpdateAPIKeyGroupsInput{Groups: []APIKeyGroupLink{{GroupID: 8, Role: "normal"}}, ExpectedGroupChainRevision: &rev})
	require.Error(t, err)
	require.False(t, users.addGroupCalled)
	require.False(t, repo.updated)
}

func TestAPIKeyGroupChainAdminLegacySameHeadContractsNormalsAndKeepsHA(t *testing.T) {
	at := time.Now().UTC()
	head := int64(1)
	for _, tc := range []struct {
		name        string
		groupID     *int64
		requested   int64
		wantNormals int
	}{
		{"same-head", &head, 1, 1}, {"nil-head-clear", nil, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &adminChainTransactionStub{key: &APIKey{ID: 1, UserID: 7, Key: "k", GroupID: tc.groupID, GroupChainRevision: 3, Groups: []APIKeyGroupLink{{GroupID: 1, Role: "normal", Position: 0}, {GroupID: 2, Role: "normal", Position: 1}, {GroupID: 9, Role: "ha", Position: 0, EnabledByUserAt: &at}}}}
			svc := &adminServiceImpl{apiKeyRepo: repo, groupRepo: &groupRepoStubForGroupUpdate{group: &Group{ID: 1, Status: StatusActive}}}
			result, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, &tc.requested)
			require.NoError(t, err)
			require.True(t, repo.updated)
			require.Equal(t, int64(4), result.APIKey.GroupChainRevision)
			require.Len(t, result.APIKey.Groups, tc.wantNormals+1)
			require.Equal(t, "ha", result.APIKey.Groups[tc.wantNormals].Role)
			require.True(t, result.APIKey.Groups[tc.wantNormals].EnabledByUserAt.Equal(at))
		})
	}
}
