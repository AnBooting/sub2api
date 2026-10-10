package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAPIKeyGroupChainAuthSnapshotAllRolesVersionAndIsolation(t *testing.T) {
	at := time.Now().UTC()
	price := 0.2
	id := int64(1)
	group := &Group{ID: 1, Name: "normal", Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 2, PeakRateEnabled: true, PeakRateMultiplier: 3, ImagePrice1K: &price, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"model"}}, ModelRouting: map[string][]int64{"model": {4}}, ModelPricing: []ChannelModelPricing{{Models: []string{"model"}, InputPrice: &price}}}
	ha := cloneAPIKeyGroup(group)
	ha.ID = 9
	ha.Name = "HA"
	key := &APIKey{ID: 1, UserID: 7, Key: "k", GroupID: &id, Group: group, GroupChainRevision: 5, User: &User{ID: 7, Status: StatusActive, AllowedGroups: []int64{9}}, Groups: []APIKeyGroupLink{{GroupID: 1, Role: "normal", Group: group}, {GroupID: 9, Role: "ha", EnabledByUserAt: &at, Group: ha}}}
	key.User.BalanceNotifyExtraEmails = []NotifyEmailEntry{{Email: "owned@example.test", Verified: true}}
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.Equal(t, 25, snapshot.Version)
	require.Equal(t, int64(5), snapshot.GroupChainRevision)
	require.Len(t, snapshot.Groups, 2)
	// The source objects, one auth request and the next auth request must not alias.
	group.ModelRouting["model"][0] = 99
	group.ModelAllowlist.Models[0] = "changed"
	key.User.BalanceNotifyExtraEmails[0].Email = "source-mutated@example.test"
	got := svc.snapshotToAPIKey("k", snapshot)
	require.Equal(t, int64(4), got.Groups[0].Group.ModelRouting["model"][0])
	require.Equal(t, "model", got.Groups[0].Group.ModelAllowlist.Models[0])
	require.True(t, got.Groups[1].Group.Hydrated)
	got.Groups[1].Group.ModelPricing[0].Models[0] = "changed"
	*got.Groups[1].Group.ModelPricing[0].InputPrice = 99
	*got.Groups[1].EnabledByUserAt = at.Add(time.Hour)
	got.User.AllowedGroups[0] = 42
	require.Equal(t, "owned@example.test", got.User.BalanceNotifyExtraEmails[0].Email)
	got.User.BalanceNotifyExtraEmails[0].Email = "request-mutated@example.test"
	got.User.BalanceNotifyExtraEmails[0].Disabled = true
	next := svc.snapshotToAPIKey("k", snapshot)
	require.Equal(t, "model", next.Groups[1].Group.ModelPricing[0].Models[0])
	require.Equal(t, 0.2, *next.Groups[1].Group.ModelPricing[0].InputPrice)
	require.True(t, next.Groups[1].EnabledByUserAt.Equal(at))
	require.Equal(t, int64(9), next.User.AllowedGroups[0])
	require.Equal(t, "owned@example.test", next.User.BalanceNotifyExtraEmails[0].Email)
	require.False(t, next.User.BalanceNotifyExtraEmails[0].Disabled)
	require.True(t, next.User.BalanceNotifyExtraEmails[0].Verified)
	require.Equal(t, float64(3), next.Groups[1].Group.PeakRateMultiplier)
	stale := *snapshot
	stale.Version = 24
	value, ok, err := svc.applyAuthCacheEntry("k", &APIKeyAuthCacheEntry{Snapshot: &stale})
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, value)
}
