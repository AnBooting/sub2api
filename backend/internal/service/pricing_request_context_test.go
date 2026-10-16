//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func requestPricingTestInt64(value int64) *int64 { return &value }

func TestRequestBillingContextOwnsCandidatesRatesAndReturnedValues(t *testing.T) {
	billing := newTestBillingService()
	one, zero := 1e-6, 0.0
	a := &Group{ID: 1, RateMultiplier: 8, ModelPricing: []ChannelModelPricing{{Models: []string{"model"}, InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
	b := cloneAPIKeyGroup(a)
	b.ID = 2
	key := &APIKey{GroupID: requestPricingTestInt64(1), Group: a, User: &User{ID: 4}, Groups: []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0, Group: a}, {GroupID: 2, Role: APIKeyGroupRoleNormal, Position: 1, Group: b}}}
	calls := 0
	deps := inflightEstimateDeps{billing: billing, userGroupRate: func(_ context.Context, _ int64, groupID int64, _ float64) float64 {
		calls++
		if groupID == 2 {
			return 0
		}
		return 1.2
	}}
	ctx, request, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "model", Kind: InflightEstimateToken})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, []int64{1, 2}, request.NormalGroupIDs())
	require.Equal(t, int64(1), *request.RequestedGroupID())
	originalStart, originalID := request.StartedAt(), request.RequestID()
	require.NotEmpty(t, originalID)
	require.False(t, originalStart.IsZero())
	ids := request.NormalGroupIDs()
	ids[0] = 999
	requested := request.RequestedGroupID()
	*requested = 999
	key.Groups[0].Group.RateMultiplier = 999
	*key.Groups[0].Group.ModelPricing[0].InputPrice = 99
	key.Groups[1].Group.ModelPricing[0].Models[0] = "different"
	for _, id := range []int64{1, 2} {
		scope, err := request.ScopeFor(id, "", "")
		require.NoError(t, err)
		quote, err := request.PricingFor(scope)
		require.NoError(t, err)
		require.NoError(t, quote.Availability())
		want := 1.2
		if id == 2 {
			want = 0
		}
		require.Equal(t, want, quote.EffectiveMultiplier())
		cost, err := quote.Calculate(CostInput{Model: "attacker", RateMultiplier: 1000, PricingAt: originalStart.Add(24 * time.Hour), Group: a, Tokens: UsageTokens{InputTokens: 1_000_000}})
		require.NoError(t, err)
		require.InDelta(t, want, cost.ActualCost, 1e-12)
		cost.PricingSnapshot.InputPricePerToken = 999
		again, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
		require.NoError(t, err)
		require.InDelta(t, want, again.ActualCost, 1e-12)
	}
	require.Equal(t, []int64{1, 2}, request.NormalGroupIDs())
	require.Equal(t, int64(1), *request.RequestedGroupID())
	_, same, err := deps.prepareRequestBilling(ctx, key, InflightEstimateRequest{Model: "changed"})
	require.NoError(t, err)
	require.Same(t, request, same)
	require.Equal(t, 2, calls, "retry cannot reread the user rate")
	require.Same(t, request, GatewayRequestBillingFromContext(context.WithoutCancel(ctx)))
	_, compatStart := WithGatewayTokenRequestPricing(ctx)
	require.Equal(t, originalStart, compatStart)
	require.Equal(t, originalID, same.RequestID())
}

// TestRefreezeRequestBillingProducesANewBundle 锁定 WS 每 turn 强制重冻语义：
// 已有 bundle 时 PrepareRequestBilling 幂等复用（HTTP failover 契约），而
// RefreezeRequestBilling 必须产出新的 bundle、新的 requestID 与新的定价时刻，
// 且新 bundle 的请求模型取自本次 turn 的模型。
func TestRefreezeRequestBillingProducesANewBundle(t *testing.T) {
	billing := newTestBillingService()
	one, zero := 1e-6, 0.0
	group := &Group{ID: 1, RateMultiplier: 2, ModelPricing: []ChannelModelPricing{
		{Models: []string{"first-model"}, InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero},
		{Models: []string{"second-model"}, InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero},
	}}
	key := &APIKey{GroupID: requestPricingTestInt64(1), Group: group}
	svc := &OpenAIGatewayService{billingService: billing}

	ctx, first, err := svc.PrepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "first-model"})
	require.NoError(t, err)
	require.NotNil(t, first)

	_, reused, err := svc.PrepareRequestBilling(ctx, key, InflightEstimateRequest{Model: "second-model"})
	require.NoError(t, err)
	require.Same(t, first, reused, "single-request freeze stays idempotent for HTTP failover")

	// 与幂等复用同一输入，但强制重冻必须换出一份新 bundle。
	refrozenCtx, second, err := svc.RefreezeRequestBilling(withoutGatewayRequestBilling(ctx), key, InflightEstimateRequest{Model: "second-model"})
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NotSame(t, first, second)
	require.NotEqual(t, first.RequestID(), second.RequestID())
	require.Same(t, second, GatewayRequestBillingFromContext(refrozenCtx), "重冻后的 ctx 必须携带新 bundle")
	require.Same(t, first, GatewayRequestBillingFromContext(ctx), "旧 ctx 的 bundle 不得被改写")

	scope, err := second.ScopeFor(1, "", "")
	require.NoError(t, err)
	require.Equal(t, "second-model", scope.BillingModel, "重冻后的 bundle 必须按本 turn 模型取价")
	_, staleErr := first.ScopeFor(1, "", "")
	require.NoError(t, staleErr, "旧 bundle 仍保持可用，只是不再被连接使用")
}

func TestRequestBillingContextFreezesFullNativeTierCalendarAndCatalog(t *testing.T) {
	billing := newNativeBillingTest(t)
	key := &APIKey{}
	deps := inflightEstimateDeps{billing: billing}
	_, request, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "deepseek-flash", Kind: InflightEstimateToken})
	require.NoError(t, err)
	scope, err := request.ScopeFor(0, "", "")
	require.NoError(t, err)
	quote, err := request.PricingFor(scope)
	require.NoError(t, err)
	period, version := quote.Period(), quote.Version()
	cost, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
	require.NoError(t, err)
	original := cost.ActualCost
	// Mutate every live data family after freeze; the old request cannot reread.
	card := billing.pricingService.nativeCNYCards["deepseekflash"]
	*card.Input = 999
	card.Schedule.Windows[0].HourRanges[0] = "00:00-24:00"
	*card.Schedule.Windows[0].Input = 999
	billing.pricingService.controlsMu.Lock()
	billing.pricingService.controls = &PricingControls{NativeCNYEnabled: false, DeepSeekHolidays: map[string][]string{"2026": {"2026-10-12"}}}
	billing.pricingService.controlsMu.Unlock()
	cost.PricingSnapshot.NativeCard.Schedule.Windows[0].HourRanges[0] = "invalid"
	cost.PricingSnapshot.ReasoningEffortMultipliers = map[string]float64{"high": 999}
	again, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
	require.NoError(t, err)
	require.Equal(t, original, again.ActualCost)
	require.Equal(t, period, quote.Period())
	require.Equal(t, version, quote.Version())
	require.Nil(t, request.RequestedGroupID())
	require.Equal(t, []int64{0}, request.NormalGroupIDs())
	// Start a separate request: its logical id/version are not inherited from an old turn.
	_, next, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "deepseek-flash"})
	require.NoError(t, err)
	require.NotEqual(t, request.RequestID(), next.RequestID())
	require.NotEqual(t, version, next.version)
	require.False(t, next.StartedAt().Before(request.StartedAt()))
}

func TestRequestBillingContextFreezesExactAccountMappingAndSource(t *testing.T) {
	billing := newTestBillingService()
	one, two, zero := 1e-6, 2e-6, 0.0
	cache := newEmptyChannelCache()
	cache.channelByGroupID[7] = &Channel{ID: 9, Status: StatusActive, BillingModelSource: BillingModelSourceUpstream}
	cache.groupPlatform[7] = PlatformOpenAI
	cache.mappingByGroupModel[channelModelKey{groupID: 7, platform: PlatformOpenAI, model: "requested"}] = "channel-target"
	for model, rate := range map[string]*float64{"account-a": &one, "account-b": &two, "channel-target": &one} {
		cache.pricingByGroupModel[channelModelKey{groupID: 7, platform: PlatformOpenAI, model: model}] = &ChannelModelPricing{InputPrice: rate, OutputPrice: rate, CacheReadPrice: &zero, CacheWritePrice: &zero}
	}
	channels := newTestChannelServiceWithCache(t, cache)
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1}
	key := &APIKey{GroupID: requestPricingTestInt64(7), Group: group}
	results := map[int64]requestAccountMapping{11: {Model: "account-a"}, 12: {Model: "account-b"}}
	deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(channels, billing), accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) { return results, nil }}
	_, request, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "requested"})
	require.NoError(t, err)
	mapping, err := request.MappingFor(7)
	require.NoError(t, err)
	require.Equal(t, "channel-target", mapping.MappedModel)
	require.Equal(t, int64(9), mapping.ChannelID)
	results[11] = requestAccountMapping{Model: "account-b"} // still in allowed set, but X's relationship stays A.
	cache.channelByGroupID[7].BillingModelSource = BillingModelSourceRequested
	cache.mappingByGroupModel[channelModelKey{groupID: 7, platform: PlatformOpenAI, model: "requested"}] = "changed-live"
	*cache.pricingByGroupModel[channelModelKey{groupID: 7, platform: PlatformOpenAI, model: "account-a"}].InputPrice = 999
	upstream, err := request.UpstreamModelFor(7, 11)
	require.NoError(t, err)
	require.Equal(t, "account-a", upstream)
	other, err := request.UpstreamModelFor(7, 12)
	require.NoError(t, err)
	require.Equal(t, "account-b", other)
	scope, err := request.ScopeFor(7, upstream, "")
	require.NoError(t, err)
	require.Equal(t, "account-a", scope.BillingModel)
	quote, err := request.PricingFor(scope)
	require.NoError(t, err)
	cost, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
	require.NoError(t, err)
	require.Equal(t, 1.0, cost.ActualCost)
	_, err = request.UpstreamModelFor(7, 999)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = request.ScopeFor(7, "not-frozen", "")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	scope.AccountID = 11
	_, err = request.PricingFor(scope)
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "route account id is not a customer-price account override")
}

func TestRequestBillingContextUnknownAndResponseModelFailClosed(t *testing.T) {
	billing := newTestBillingService()
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[7] = &Channel{ID: 9, Status: StatusActive, BillingModelSource: BillingModelSourceResponse}
	cache.groupPlatform[7] = PlatformOpenAI
	channels := newTestChannelServiceWithCache(t, cache)
	deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(channels, billing)}
	_, request, err := deps.prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(7), Group: group}, InflightEstimateRequest{Model: "claude-sonnet-4"})
	require.NoError(t, err)
	baseline, err := request.ScopeFor(7, "", "")
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4", baseline.BillingModel, "actual response model is not known in preflight")
	response, err := request.ScopeFor(7, "", "claude-opus-4.5")
	require.NoError(t, err)
	require.Equal(t, "claude-opus-4.5", response.BillingModel)
	_, err = request.ScopeFor(7, "", "gpt-completely-new-unknown")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = request.MappingFor(999)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = request.PricingFor(PriceScope{GroupID: 999, BillingModel: "claude-sonnet-4"})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	baseline.ChannelID = 123
	_, err = request.PricingFor(baseline)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	// Availability explicitly distinguishes an unknown price from an explicit zero.
	_, missing, err := (inflightEstimateDeps{billing: billing}).prepareRequestBilling(context.Background(), &APIKey{}, InflightEstimateRequest{Model: "unknown-model"})
	require.NoError(t, err)
	quote, err := missing.PricingFor(PriceScope{BillingModel: "unknown-model"})
	require.NoError(t, err)
	require.ErrorIs(t, quote.Availability(), ErrModelPricingUnavailable)
	_, err = quote.Calculate(CostInput{RateMultiplier: 0})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

// TestGatewayRequestPricingAccountSwitchIsFailClosed 锁定 recon §1.4-4：failover
// 换账号后，若实际账号与冻结执行事实不一致，账号模型解析必须报错（不能静默回退
// 到身份映射或零价），且换号必须重新绑定到新账号的冻结映射。
func TestGatewayRequestPricingAccountSwitchIsFailClosed(t *testing.T) {
	billing := newTestBillingService()
	one, zero := 1e-6, 0.0
	group := &Group{ID: 1, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"model"}, InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
	deps := inflightEstimateDeps{billing: billing, accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
		return map[int64]requestAccountMapping{11: {Model: "model-account-11"}, 12: {Model: "model-account-12"}}, nil
	}}
	ctx, request, err := deps.prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "model"})
	require.NoError(t, err)
	bound, err := WithGatewayRequestBillingAccount(ctx, 1, 11)
	require.NoError(t, err)
	model, _, err := frozenGatewayRequestAccountModel(bound, 11)
	require.NoError(t, err)
	require.Equal(t, "model-account-11", model)
	// 冻结执行事实是账号 11：按账号 12 结算必须 fail-closed，绝不静默用 12 的映射。
	_, _, err = frozenGatewayRequestAccountModel(bound, 12)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	// 换号后重新绑定新账号：解析出的是新账号自己的冻结映射。
	rebound, err := WithGatewayRequestBillingAccount(bound, 1, 12)
	require.NoError(t, err)
	model, _, err = frozenGatewayRequestAccountModel(rebound, 12)
	require.NoError(t, err)
	require.Equal(t, "model-account-12", model)
	_, _, err = frozenGatewayRequestAccountModel(rebound, 11)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.NotNil(t, request)
}

func TestRequestBillingContextMappingSnapshotErrorsAreNotIdentity(t *testing.T) {
	deps := inflightEstimateDeps{billing: newTestBillingService(), accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
		return nil, errors.New("scheduler snapshot unavailable")
	}}
	_, request, err := deps.prepareRequestBilling(context.Background(), &APIKey{}, InflightEstimateRequest{Model: "claude-sonnet-4"})
	require.Error(t, err)
	require.Nil(t, request)
}

func TestRequestBillingContextFullNativeLadderAndHAGettersAreOwned(t *testing.T) {
	billing := newNativeBillingTest(t)
	one, three, seven, zero := 1.0, 3.0, 7.0, 0.0
	card := &NativeCNYCard{NativeRates: NativeRates{Input: &one, Output: &one, CacheRead: &zero, CacheWrite: &zero}}
	tier := NativeTier{NativeRates: NativeRates{Input: &three, Output: &seven}}
	tier.Tier.Type, tier.Tier.Size = "context", 100
	card.Tiers = []NativeTier{tier}
	billing.pricingService.nativeCNYCards["glmimmutable"] = card
	at := time.Now()
	group := &Group{ID: 1, RateMultiplier: 1, LongContextPricingEnabled: true}
	ha := &Group{ID: 2, RateMultiplier: 1}
	key := &APIKey{GroupID: requestPricingTestInt64(1), Group: group, Groups: []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Group: group}, {GroupID: 2, Role: APIKeyGroupRoleHA, Group: ha, EnabledByUserAt: &at}}}
	_, request, err := (inflightEstimateDeps{billing: billing}).prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "glm-immutable"})
	require.NoError(t, err)
	has := request.HAGroupIDs()
	require.Equal(t, []int64{2}, has)
	has[0] = 999
	require.Equal(t, []int64{2}, request.HAGroupIDs())
	// Snapshotting configured HA does not execute or confer authorization.
	require.Equal(t, at, *key.Groups[1].EnabledByUserAt)
	scope, err := request.ScopeFor(1, "", "")
	require.NoError(t, err)
	quote, err := request.PricingFor(scope)
	require.NoError(t, err)
	low, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 100}})
	require.NoError(t, err)
	high, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 101}})
	require.NoError(t, err)
	require.InDelta(t, 100.0/1e6, low.TotalCost, 1e-12)
	require.InDelta(t, 101.0*3/1e6, high.TotalCost, 1e-12)
	*card.Tiers[0].Input = 999
	*high.PricingSnapshot.NativeCard.Tiers[0].Input = 777
	card.Tiers[0].Tier.Size = 0
	again, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 101}})
	require.NoError(t, err)
	require.Equal(t, high.TotalCost, again.TotalCost)
}

func TestRequestBillingContextOwnsCompleteUSDCatalogAndReturnedMaps(t *testing.T) {
	billing := newTestBillingService()
	billing.pricingService = NewPricingService(billing.cfg, nil)
	billing.pricingService.pricingData["catalog-request"] = &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, TokenPricePresence: completeTokenPricePresence()}
	billing.pricingService.pricingData["catalog-response"] = &LiteLLMModelPricing{InputCostPerToken: 3e-6, OutputCostPerToken: 4e-6, TokenPricePresence: completeTokenPricePresence()}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[1] = &Channel{ID: 7, Status: StatusActive, BillingModelSource: BillingModelSourceResponse}
	cache.groupPlatform[1] = PlatformOpenAI
	group := &Group{ID: 1, Platform: PlatformOpenAI, RateMultiplier: 1}
	channels := newTestChannelServiceWithCache(t, cache)
	deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(channels, billing)}
	_, request, err := deps.prepareRequestBilling(context.Background(), &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "catalog-request"})
	require.NoError(t, err)
	// Freeze the whole catalog, not just the requested model: the trusted response
	// model is only known later yet must use its request-start card.
	billing.pricingService.pricingData["catalog-response"].InputCostPerToken = 99
	delete(billing.pricingService.pricingData, "catalog-request")
	billing.pricingService.pricingData["late-model"] = &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 1e-6}
	scope, err := request.ScopeFor(1, "", "catalog-response")
	require.NoError(t, err)
	quote, err := request.PricingFor(scope)
	require.NoError(t, err)
	cost, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}})
	require.NoError(t, err)
	require.Equal(t, 3.0, cost.ActualCost)
	cost.PricingSnapshot.ReasoningEffortMultipliers = map[string]float64{"high": 999}
	again, err := quote.Calculate(CostInput{Tokens: UsageTokens{InputTokens: 1_000_000}, ReasoningEffort: "high"})
	require.NoError(t, err)
	require.Equal(t, 3.0, again.ActualCost)
	_, err = request.ScopeFor(1, "", "late-model")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}
