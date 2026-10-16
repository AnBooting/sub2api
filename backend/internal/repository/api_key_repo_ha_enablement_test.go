package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// haGroupRepoStub 只覆写 GetByID，其余分组仓储方法不需要。
type haGroupRepoStub struct {
	service.GroupRepository
	group *service.Group
}

func (s *haGroupRepoStub) GetByID(context.Context, int64) (*service.Group, error) {
	clone := *s.group
	return &clone, nil
}

// 用户主动开启/关闭走真实 SQLite 仓储 + 真实服务方法（含嵌套事务与缓存失效路径）：
// 开启后 role=ha 且 enabled_by_user_at 非空，关闭后置 NULL，每次实际变更递增 revision。
func TestAPIKeyGroupServiceUserAuthorizedHAEnablementRoundTrip(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "ha-user@test.com")
	normal, err := client.Group.Create().SetName("ha-normal").Save(ctx)
	require.NoError(t, err)
	ha, err := client.Group.Create().SetName("ha-candidate").SetAllowHighAvailability(true).Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{UserID: user.ID, Key: "sk-ha-user", Name: "HA", Status: service.StatusActive, Groups: []service.APIKeyGroupLink{{GroupID: normal.ID, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
	require.NoError(t, repo.Create(ctx, key))

	groupRepo := &haGroupRepoStub{group: &service.Group{ID: ha.ID, Name: "ha-candidate", Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 2.5, AllowHighAvailability: true}}
	svc := service.NewAPIKeyService(repo, nil, groupRepo, nil, nil, nil, nil)

	enabled, err := svc.SetAPIKeyHAGroupEnabled(ctx, user.ID, key.ID, ha.ID, true)
	require.NoError(t, err)
	require.Equal(t, 2.5, enabled.RateMultiplier)
	require.Equal(t, int64(1), enabled.GroupChainRevision)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, got.Groups, 2)
	require.Equal(t, service.APIKeyGroupRoleHA, got.Groups[1].Role)
	require.NotNil(t, got.Groups[1].EnabledByUserAt, "enablement must persist enabled_by_user_at")

	// 重复开启幂等：不递增版本。
	again, err := svc.SetAPIKeyHAGroupEnabled(ctx, user.ID, key.ID, ha.ID, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), again.GroupChainRevision, "repeated enable must be idempotent")

	disabled, err := svc.SetAPIKeyHAGroupEnabled(ctx, user.ID, key.ID, ha.ID, false)
	require.NoError(t, err)
	require.Equal(t, int64(2), disabled.GroupChainRevision, "disablement must increment revision")

	closed, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, closed.Groups, 2)
	require.Equal(t, service.APIKeyGroupRoleHA, closed.Groups[1].Role)
	require.Nil(t, closed.Groups[1].EnabledByUserAt, "disablement must clear enabled_by_user_at")
}

// 非本人 Key 与未标记兜底的组在真实仓储上都不落库。
func TestAPIKeyGroupServiceHAEnablementRejectionsDoNotWrite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "ha-reject@test.com")
	other := mustCreateAPIKeyRepoUser(t, ctx, client, "ha-reject-other@test.com")
	normal, err := client.Group.Create().SetName("ha-reject-normal").Save(ctx)
	require.NoError(t, err)
	unmarked, err := client.Group.Create().SetName("ha-reject-unmarked").Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{UserID: user.ID, Key: "sk-ha-reject", Name: "HA", Status: service.StatusActive, Groups: []service.APIKeyGroupLink{{GroupID: normal.ID, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
	require.NoError(t, repo.Create(ctx, key))

	groupRepo := &haGroupRepoStub{group: &service.Group{ID: unmarked.ID, Status: service.StatusActive, AllowHighAvailability: false}}
	svc := service.NewAPIKeyService(repo, nil, groupRepo, nil, nil, nil, nil)

	// 非本人 Key。
	_, err = svc.SetAPIKeyHAGroupEnabled(ctx, other.ID, key.ID, unmarked.ID, true)
	require.Error(t, err)
	// 未标记兜底。
	_, err = svc.SetAPIKeyHAGroupEnabled(ctx, user.ID, key.ID, unmarked.ID, true)
	require.Error(t, err)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	require.Zero(t, got.GroupChainRevision)
}

// 管理员路径：不得创建 role=ha，也不得借此丢掉既有 HA 授权。
//
// 契约（11-共享契约.md:75、13-接口文档.md:33）：管理员 groups 只允许 normal，
// 数据库保留既有 HA。旧实现允许管理员写入 HA，并在同一次提交里把未声明的既有 HA
// 静默删除——而 enabled_by_user_at 只存在于那些行里，删掉无法重建。
func TestAPIKeyGroupChainRepositoryAdminHADeclarationPreservesUserEnablement(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "ha-admin@test.com")
	normal, err := client.Group.Create().SetName("ha-admin-normal").Save(ctx)
	require.NoError(t, err)
	ha, err := client.Group.Create().SetName("ha-admin-candidate").SetAllowHighAvailability(true).Save(ctx)
	require.NoError(t, err)
	other, err := client.Group.Create().SetName("ha-admin-other").SetAllowHighAvailability(true).Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{UserID: user.ID, Key: "sk-ha-admin", Name: "HA", Status: service.StatusActive, Groups: []service.APIKeyGroupLink{{GroupID: normal.ID, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
	require.NoError(t, repo.Create(ctx, key))

	// The user enables one HA group through the user path (server-generated timestamp).
	enabledAt := time.Now().UTC().Truncate(time.Millisecond)
	rev := key.GroupChainRevision
	enabled := *key
	enabled.Groups = []service.APIKeyGroupLink{
		{GroupID: normal.ID, Role: service.APIKeyGroupRoleNormal, Position: 0},
		{GroupID: other.ID, Role: service.APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &enabledAt},
	}
	require.NoError(t, repo.Update(ctx, &enabled, service.APIKeyUpdateFields{Groups: true, AuthoritativeHAGroups: true, ExpectedGroupChainRevision: &rev}))

	// An admin then edits the normal chain and also submits a (different) HA entry.
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	rev = got.GroupChainRevision
	admin := *got
	admin.Groups = []service.APIKeyGroupLink{
		{GroupID: normal.ID, Role: service.APIKeyGroupRoleNormal, Position: 0},
		{GroupID: ha.ID, Role: service.APIKeyGroupRoleHA, Position: 0},
	}
	require.NoError(t, repo.Update(ctx, &admin, service.APIKeyUpdateFields{Groups: true, AllowHADeclaration: true, ExpectedGroupChainRevision: &rev}))

	final, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	var haIDs []int64
	for _, link := range final.Groups {
		if link.Role == service.APIKeyGroupRoleHA {
			haIDs = append(haIDs, link.GroupID)
		}
	}
	require.Equal(t, []int64{other.ID}, haIDs,
		"the user's HA authorization must survive an admin normal-chain edit, and no new HA may be created")
	for _, link := range final.Groups {
		if link.Role == service.APIKeyGroupRoleHA {
			require.NotNil(t, link.EnabledByUserAt)
		}
	}
}
