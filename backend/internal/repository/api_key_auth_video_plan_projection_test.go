// 外部独立审查（2026-10-16）发现的鉴权投影缺陷回归用例（R6）。
//
// GetByKeyForAuth 的 WithGroup.Select 漏选 group.FieldVideoPricePlans，导致只配置
// 新方案（per_request/per_second/per_token）的站，Seedance 创建稳定 503
// pricing_unavailable。修复前实测失败，修复后必须持续通过。
package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPR3Review_AuthProjectionRetainsVideoPlans(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "pr3-plan@test.com")
	price := 3.0
	group, err := client.Group.Create().SetName("seedance-plan").SetPlatform(service.PlatformOpenAI).
		SetVideoPricePlans(map[string]domain.VideoPricePlan{
			service.VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: &price},
		}).Save(ctx)
	require.NoError(t, err)
	key := &service.APIKey{UserID: user.ID, Key: "sk-pr3-plan", Name: "plan", Status: service.StatusActive, GroupID: &group.ID}
	require.NoError(t, repo.Create(ctx, key))
	full, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.True(t, service.GroupHasSeedanceVideoPrice(full.Group), "stored plan is valid")
	auth, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.True(t, service.GroupHasSeedanceVideoPrice(auth.Group), "gateway auth projection must retain the valid video plan")
}
