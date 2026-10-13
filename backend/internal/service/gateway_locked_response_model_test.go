//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// newLockedResponseSourceFixture 构造一个 BillingModelSource=response 的锁价请求：
// 渠道显式开启按响应模型计费，分组价里同时存在便宜的基线模型与昂贵的响应模型。
type lockedResponseSourceFixture struct {
	svc       *GatewayService
	logs      *openAIRecordUsageLogRepoStub
	ledger    *openAIRecordUsageBillingRepoStub
	group     *Group
	account   *Account
	key       *APIKey
	ctx       context.Context
	request   *RequestBillingContext
	baseline  string
	expensive string
}

func newLockedResponseSourceFixture(t *testing.T, baselineIn, expensiveIn float64) *lockedResponseSourceFixture {
	t.Helper()
	zero := 0.0
	group := &Group{ID: 77, Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{
			{Models: []string{"client-model"}, InputPrice: &baselineIn, OutputPrice: &baselineIn, CacheReadPrice: &zero, CacheWritePrice: &zero},
			{Models: []string{"claude-opus-4.5"}, InputPrice: &expensiveIn, OutputPrice: &expensiveIn, CacheReadPrice: &zero, CacheWritePrice: &zero},
		}}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[group.ID] = &Channel{ID: 9, Status: StatusActive, BillingModelSource: BillingModelSourceResponse}
	cache.groupPlatform[group.ID] = PlatformAnthropic
	channels := newTestChannelServiceWithCache(t, cache)
	billing := newTestBillingService()
	deps := inflightEstimateDeps{
		billing:  billing,
		resolver: NewModelPricingResolver(channels, billing),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{81: {Model: "client-model"}}, nil
		},
	}
	ctx, request, err := deps.prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(group.ID), Group: group}, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	// 绑定实际账号：锁价消费侧要求 ctx 上有账号执行事实。
	bound, err := WithGatewayRequestBillingAccount(ctx, group.ID, 81)
	require.NoError(t, err)

	account := &Account{ID: 81, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1, Schedulable: true}
	key := &APIKey{ID: 91, UserID: 92, GroupID: requestPricingTestInt64(group.ID), Group: group, User: &User{ID: 92, Status: StatusActive}}
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	ledger := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	svc.billingService = billing
	svc.resolver = NewModelPricingResolver(channels, billing)
	return &lockedResponseSourceFixture{svc: svc, logs: logs, ledger: ledger, group: group, account: account, key: key, ctx: bound, request: request, baseline: "client-model", expensive: "claude-opus-4.5"}
}

// TestLockedResponseSourceRejectsConflictingResponseModel 锁定 F4 回归之一：response_model
// 计费源下，上游在流中自报了两个互相矛盾的模型时，锁价分支必须 fail-closed，而不是挑
// 其中一个（更贵的那个）去计价。
func TestLockedResponseSourceRejectsConflictingResponseModel(t *testing.T) {
	f := newLockedResponseSourceFixture(t, 1e-6, 1e-5)
	result := &ForwardResult{
		Model:                         "client-model",
		UpstreamModel:                 "client-model",
		UpstreamResponseModel:         "claude-opus-4.5",
		UpstreamResponseModelConflict: true,
		Usage:                         ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
	}
	_, _, _, err := f.svc.calculateLockedSharedGatewayUsageCost(f.ctx, f.request, result, f.account)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Contains(t, err.Error(), "conflicting upstream response model")
}

// TestLockedResponseSourceCannotRaiseCost 锁定 F4 回归之二：response_model 不得比冻结基线
// 更贵。上游自报一个更贵的模型时，必须沿用基线价（与 legacy
// responseModelBillingAdoptable 的"不得更贵"不变式一致）。
func TestLockedResponseSourceCannotRaiseCost(t *testing.T) {
	f := newLockedResponseSourceFixture(t, 1e-6, 1e-5)
	result := &ForwardResult{
		Model:                 "client-model",
		UpstreamModel:         "client-model",
		UpstreamResponseModel: "claude-opus-4.5",
		Usage:                 ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
	}
	cost, quote, scope, err := f.svc.calculateLockedSharedGatewayUsageCost(f.ctx, f.request, result, f.account)
	require.NoError(t, err)
	baseline := 1000*1e-6 + 500*1e-6
	require.InDelta(t, baseline, cost.TotalCost, 1e-15, "response model must not raise the cost above the frozen baseline")
	require.Equal(t, f.baseline, scope.BillingModel)
	require.NotNil(t, quote)
}

// TestLockedResponseSourceCannotZeroOutCost 锁定 F4 回归之三：response_model 不得把一笔
// 本应计费的请求归零（价格表里存在显式 0 条目，不能靠自报模型名白嫖）。
func TestLockedResponseSourceCannotZeroOutCost(t *testing.T) {
	f := newLockedResponseSourceFixture(t, 1e-6, 0)
	result := &ForwardResult{
		Model:                 "client-model",
		UpstreamModel:         "client-model",
		UpstreamResponseModel: "claude-opus-4.5",
		Usage:                 ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
	}
	cost, _, scope, err := f.svc.calculateLockedSharedGatewayUsageCost(f.ctx, f.request, result, f.account)
	require.NoError(t, err)
	require.Greater(t, cost.TotalCost, 0.0, "response model must not zero out a billable request")
	require.Equal(t, f.baseline, scope.BillingModel)
}

// TestLockedResponseSourceAcceptsCheaperResponseModel 锁定 F4 的正向面：更便宜的响应模型
// 可以采纳（少收是允许的），并且走的是响应模型的冻结价。
func TestLockedResponseSourceAcceptsCheaperResponseModel(t *testing.T) {
	f := newLockedResponseSourceFixture(t, 1e-5, 1e-6)
	result := &ForwardResult{
		Model:                 "client-model",
		UpstreamModel:         "client-model",
		UpstreamResponseModel: "claude-opus-4.5",
		Usage:                 ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
	}
	cost, _, scope, err := f.svc.calculateLockedSharedGatewayUsageCost(f.ctx, f.request, result, f.account)
	require.NoError(t, err)
	require.Equal(t, f.expensive, scope.BillingModel, "a cheaper response model is adopted")
	require.InDelta(t, 1000*1e-6+500*1e-6, cost.TotalCost, 1e-15)
}
