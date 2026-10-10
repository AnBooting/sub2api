package repository

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikeygrouplink"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAPIKeyGroupChainRepositoryPostgresCASLocksBeforeCallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newAPIKeyRepositoryWithSQL(client, db)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT .* FROM "api_keys" .* FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "group_chain_revision", "group_id"}).AddRow(int64(1), int64(2), nil))
	mock.ExpectQuery(`SELECT .* FROM "api_key_group_links"`).WillReturnRows(sqlmock.NewRows([]string{"id", "api_key_id", "group_id", "role", "position", "enabled_by_user_at"}))
	mock.ExpectRollback()
	called := false
	expected := int64(1)
	err = repo.WithGroupChainTransaction(context.Background(), 1, &expected, func(context.Context, *service.APIKey) error { called = true; return nil })
	require.ErrorIs(t, err, service.ErrGroupChainConflict)
	require.False(t, called, "stale edit must not grant or reset usage")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAPIKeyGroupChainRepositoryRoundTripCASAndUsageIsolation(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "chain-roundtrip@test.com")
	a, err := client.Group.Create().SetName("chain-a").Save(ctx)
	require.NoError(t, err)
	b, err := client.Group.Create().SetName("chain-b").Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Key: "sk-chain-roundtrip", Name: "chain", Status: service.StatusActive, Groups: []service.APIKeyGroupLink{{GroupID: a.ID, Role: "normal", Position: 0}, {GroupID: b.ID, Role: "normal", Position: 1}}, Quota: 100}
	require.NoError(t, repo.Create(ctx, key))
	require.Equal(t, a.ID, *key.GroupID)
	first, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Len(t, first.Groups, 2)
	require.NotNil(t, first.Groups[1].Group)
	require.Zero(t, first.GroupChainRevision)
	stale := *first
	_, err = repo.IncrementQuotaUsed(ctx, key.ID, 23)
	require.NoError(t, err)
	rev := first.GroupChainRevision
	first.Groups = []service.APIKeyGroupLink{{GroupID: b.ID, Role: "normal", Position: 0}, {GroupID: a.ID, Role: "normal", Position: 1}}
	require.NoError(t, repo.Update(ctx, first, service.APIKeyUpdateFields{Groups: true, ExpectedGroupChainRevision: &rev}))
	require.Equal(t, int64(1), first.GroupChainRevision)
	stale.Name = "must-not-apply"
	stale.Groups = []service.APIKeyGroupLink{}
	require.ErrorIs(t, repo.Update(ctx, &stale, service.APIKeyUpdateFields{Name: true, Groups: true, ExpectedGroupChainRevision: &rev}), service.ErrGroupChainConflict)
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "chain", got.Name)
	require.Equal(t, b.ID, *got.GroupID)
	require.InDelta(t, 23, got.QuotaUsed, 1e-9)
	rev = got.GroupChainRevision
	require.NoError(t, repo.Update(ctx, got, service.APIKeyUpdateFields{Groups: true, ExpectedGroupChainRevision: &rev}))
	require.Equal(t, rev, got.GroupChainRevision, "unchanged chain must not increment revision")
	require.ErrorIs(t, repo.Update(ctx, got, service.APIKeyUpdateFields{Groups: true}), service.ErrGroupChainConflict)
}

func TestAPIKeyGroupChainRepositoryLegacyPreservesDatabaseHAAndRejectsReopen(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "chain-ha@test.com")
	a, err := client.Group.Create().SetName("chain-ha-a").Save(ctx)
	require.NoError(t, err)
	b, err := client.Group.Create().SetName("chain-ha-b").Save(ctx)
	require.NoError(t, err)
	ha, err := client.Group.Create().SetName("chain-ha-authorized").Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Key: "sk-chain-ha", Name: "HA", Status: service.StatusActive, GroupID: &a.ID}
	require.NoError(t, repo.Create(ctx, key))
	at := time.Now().UTC().Truncate(time.Second)
	_, err = client.APIKeyGroupLink.Create().SetAPIKeyID(key.ID).SetGroupID(ha.ID).SetRole(apikeygrouplink.RoleHa).SetPosition(0).SetEnabledByUserAt(at).Save(ctx)
	require.NoError(t, err)
	stale, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	// A legacy writer with no HA in its stale object must preserve the locked database authorization.
	key.GroupID = &b.ID
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{GroupID: true}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, got.Groups, 2)
	require.True(t, got.Groups[1].EnabledByUserAt.Equal(at))
	key.GroupID = nil
	require.NoError(t, repo.Update(ctx, key, service.APIKeyUpdateFields{GroupID: true}))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Nil(t, got.GroupID)
	require.Len(t, got.Groups, 1)
	require.Equal(t, "ha", got.Groups[0].Role)
	rev := got.GroupChainRevision
	got.Groups = []service.APIKeyGroupLink{}
	require.NoError(t, repo.Update(ctx, got, service.APIKeyUpdateFields{Groups: true, ExpectedGroupChainRevision: &rev}))
	oldRev := stale.GroupChainRevision
	require.ErrorIs(t, repo.Update(ctx, stale, service.APIKeyUpdateFields{Groups: true, ExpectedGroupChainRevision: &oldRev}), service.ErrGroupChainConflict)
	currentRev := got.GroupChainRevision
	require.Error(t, repo.Update(ctx, stale, service.APIKeyUpdateFields{Groups: true, ExpectedGroupChainRevision: &currentRev}), "even a current revision cannot forge task5 HA enablement")
	empty, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Empty(t, empty.Groups)
}

func TestAPIKeyGroupChainRepositoryAllRolesClearMigrateAndDeletedTombstone(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "chain-allroles@test.com")
	a, err := client.Group.Create().SetName("allroles-a").Save(ctx)
	require.NoError(t, err)
	b, err := client.Group.Create().SetName("allroles-b").Save(ctx)
	require.NoError(t, err)
	c, err := client.Group.Create().SetName("allroles-c").Save(ctx)
	require.NoError(t, err)
	ha, err := client.Group.Create().SetName("allroles-ha").Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Name: "allroles", Key: "sk-allroles", Status: service.StatusActive, Groups: []service.APIKeyGroupLink{{GroupID: a.ID, Role: "normal", Position: 0}, {GroupID: b.ID, Role: "normal", Position: 1}}, Quota: 100}
	require.NoError(t, repo.Create(ctx, key))
	at := time.Now().UTC()
	_, err = client.APIKeyGroupLink.Create().SetAPIKeyID(key.ID).SetGroupID(ha.ID).SetRole(apikeygrouplink.RoleHa).SetPosition(0).SetEnabledByUserAt(at).Save(ctx)
	require.NoError(t, err)
	for _, groupID := range []int64{a.ID, b.ID, ha.ID} {
		keys, err := repo.ListKeysByGroupID(ctx, groupID)
		require.NoError(t, err)
		require.Equal(t, []string{key.Key}, keys)
		n, err := repo.CountByGroupID(ctx, groupID)
		require.NoError(t, err)
		require.Equal(t, int64(1), n)
	}
	n, err := repo.UpdateGroupIDByUserAndGroup(ctx, user.ID, b.ID, c.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, c.ID, got.Groups[1].GroupID)
	require.Equal(t, int64(1), got.GroupChainRevision)
	require.Len(t, got.Groups, 3)
	n, err = repo.ClearGroupIDByGroupID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, c.ID, *got.GroupID)
	require.Equal(t, 0, got.Groups[0].Position)
	require.Equal(t, int64(2), got.GroupChainRevision)
	n, err = repo.ClearGroupIDByGroupID(ctx, ha.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	require.Equal(t, int64(3), got.GroupChainRevision)
	// Soft-deleting a group retains the explicit Key configuration. It cannot silently rebind.
	require.NoError(t, client.Group.DeleteOneID(c.ID).Exec(ctx))
	tombstone, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, c.ID, *tombstone.GroupID)
	require.Nil(t, tombstone.Group)
	require.Equal(t, int64(3), tombstone.GroupChainRevision)
	require.Len(t, tombstone.Groups, 1)
	require.Nil(t, tombstone.Groups[0].Group)
	keys, err := repo.ListKeysByGroupID(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, []string{key.Key}, keys, "deleted group must still reverse-map keys for invalidation")
}

func TestAPIKeyGroupChainRepositoryLegacySameHeadAndNilHeadRepair(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "legacy-samehead@test.com")
	a, err := client.Group.Create().SetName("legacy-head-a").Save(ctx)
	require.NoError(t, err)
	b, err := client.Group.Create().SetName("legacy-head-b").Save(ctx)
	require.NoError(t, err)
	ha, err := client.Group.Create().SetName("legacy-head-ha").Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Name: "legacy-head", Key: "sk-legacy-head", Status: service.StatusActive, Groups: []service.APIKeyGroupLink{{GroupID: a.ID, Role: "normal", Position: 0}, {GroupID: b.ID, Role: "normal", Position: 1}}}
	require.NoError(t, repo.Create(ctx, key))
	at := time.Now().UTC()
	_, err = client.APIKeyGroupLink.Create().SetAPIKeyID(key.ID).SetGroupID(ha.ID).SetRole(apikeygrouplink.RoleHa).SetPosition(0).SetEnabledByUserAt(at).Save(ctx)
	require.NoError(t, err)
	same := &service.APIKey{ID: key.ID, GroupID: &a.ID}
	require.NoError(t, repo.Update(ctx, same, service.APIKeyUpdateFields{GroupID: true}))
	require.Equal(t, int64(1), same.GroupChainRevision)
	require.Len(t, same.Groups, 2)
	require.Equal(t, "ha", same.Groups[1].Role)
	require.NoError(t, repo.Update(ctx, same, service.APIKeyUpdateFields{GroupID: true}))
	require.Equal(t, int64(1), same.GroupChainRevision, "identical actual chain remains idempotent")
	// Old-writer drift must not make explicit clear a no-op.
	_, err = client.APIKey.UpdateOneID(key.ID).ClearGroupID().Save(ctx)
	require.NoError(t, err)
	clear := &service.APIKey{ID: key.ID, GroupID: nil}
	require.NoError(t, repo.Update(ctx, clear, service.APIKeyUpdateFields{GroupID: true}))
	require.Equal(t, int64(2), clear.GroupChainRevision)
	require.Len(t, clear.Groups, 1)
	require.Equal(t, "ha", clear.Groups[0].Role)
	require.Nil(t, clear.GroupID)
	require.True(t, clear.Groups[0].EnabledByUserAt.Equal(at))
}

func TestAPIKeyGroupChainAdminRepositoryDTOResponseMatchesGET(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "admin-response@test.com")
	a, err := client.Group.Create().SetName("response-A").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	b, err := client.Group.Create().SetName("response-B").SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	ha, err := client.Group.Create().SetName("response-HA").Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Name: "response", Key: "sk-admin-response", Status: service.StatusActive, GroupID: &a.ID, Quota: 100}
	require.NoError(t, repo.Create(ctx, key))
	at := time.Now().UTC()
	_, err = client.APIKeyGroupLink.Create().SetAPIKeyID(key.ID).SetGroupID(ha.ID).SetRole(apikeygrouplink.RoleHa).SetPosition(0).SetEnabledByUserAt(at).Save(ctx)
	require.NoError(t, err)
	// Real service+real SQLite repository+real DTO, not a response-only stub.
	groupRepo := NewAdminGroupRepository(client, repo.sql.(*sql.DB))
	admin := service.NewAdminService(nil, nil, groupRepo, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err = repo.IncrementQuotaUsed(ctx, key.ID, 31)
	require.NoError(t, err)
	revision := int64(0)
	result, err := admin.AdminUpdateAPIKeyGroups(ctx, key.ID, service.AdminUpdateAPIKeyGroupsInput{Groups: []service.APIKeyGroupLink{{GroupID: b.ID, Role: "normal"}}, ExpectedGroupChainRevision: &revision})
	require.NoError(t, err)
	response := dto.APIKeyFromService(result.APIKey)
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	fresh := dto.APIKeyFromService(got)
	require.Equal(t, b.ID, *response.GroupID)
	require.NotNil(t, response.Group)
	require.Equal(t, b.ID, response.Group.ID)
	require.Equal(t, "response-B", response.Group.Name)
	require.Equal(t, fresh.Group, response.Group)
	require.Equal(t, fresh.Groups, response.Groups)
	require.Equal(t, int64(1), response.GroupChainRevision)
	require.InDelta(t, 31, got.QuotaUsed, 1e-9)
	revision = result.APIKey.GroupChainRevision
	result, err = admin.AdminUpdateAPIKeyGroups(ctx, key.ID, service.AdminUpdateAPIKeyGroupsInput{Groups: []service.APIKeyGroupLink{}, ExpectedGroupChainRevision: &revision})
	require.NoError(t, err)
	response = dto.APIKeyFromService(result.APIKey)
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	fresh = dto.APIKeyFromService(got)
	require.Nil(t, response.GroupID)
	require.Nil(t, response.Group)
	require.Nil(t, fresh.Group)
	require.Len(t, response.Groups, 1)
	require.Equal(t, "ha", response.Groups[0].Role)
	require.True(t, response.Groups[0].EnabledByUserAt.Equal(at))
	require.Equal(t, fresh.Groups, response.Groups)
	require.Equal(t, int64(2), response.GroupChainRevision)
	require.InDelta(t, 31, got.QuotaUsed, 1e-9)
}
