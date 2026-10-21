//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestVideoQuoteTariffPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name                                      string
		plan, groupCard, modelSlot, flat, channel bool
		mode                                      BillingMode
		want                                      float64
		source                                    string
	}{
		{name: "plan", plan: true, groupCard: true, modelSlot: true, flat: true, channel: true, want: 3.2, source: "plan"},
		{name: "group card", groupCard: true, modelSlot: true, flat: true, channel: true, want: 4.8, source: PricingSourceGroup},
		{name: "model slot", modelSlot: true, flat: true, channel: true, want: 6.4, source: "legacy_slot"},
		{name: "flat", flat: true, channel: true, want: 8, source: "legacy_slot"},
		{name: "channel second", channel: true, mode: BillingModeVideo, want: 9.6, source: PricingSourceChannel},
		{name: "channel request", channel: true, mode: BillingModePerRequest, want: 1.2, source: PricingSourceChannel},
		{name: "channel image", channel: true, mode: BillingModeImage, want: 1.2, source: PricingSourceChannel},
		{name: "bundled", want: 0.56, source: "bundled_default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := "grok-imagine-video"
			group := &Group{ID: 42, RateMultiplier: 1}
			if tc.plan {
				group.VideoPricePlans = map[string]domain.VideoPricePlan{model: {Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{"720p": 0.4}}}
			}
			if tc.groupCard {
				group.ModelPricing = []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeVideo, PerRequestPrice: videoPlanPricePtr(0.6)}}
			}
			if tc.modelSlot {
				group.VideoModelPrices = map[string]map[string]float64{model: {"720p": 0.8}}
			}
			if tc.flat {
				group.VideoPrice720P = videoPlanPricePtr(1)
			}
			svc := videoPlanTestService()
			var channel *ChannelService
			if tc.channel {
				mode := tc.mode
				if mode == "" {
					mode = BillingModeVideo
				}
				cache := newEmptyChannelCache()
				card := &ChannelModelPricing{Models: []string{model}, BillingMode: mode, PerRequestPrice: videoPlanPricePtr(1.2)}
				cache.pricingByGroupModel[channelModelKey{groupID: group.ID, platform: PlatformGrok, model: model}] = card
				cache.groupPlatform[group.ID] = PlatformGrok
				cache.channelByGroupID[group.ID] = &Channel{Status: StatusActive}
				cache.loadedAt = time.Now()
				channel = &ChannelService{}
				channel.cache.Store(cache)
			}
			svc.resolver = NewModelPricingResolver(channel, svc.billingService)
			key := &APIKey{UserID: 10, GroupID: &group.ID, Group: group}
			result := &OpenAIForwardResult{Model: model, BillingModel: model, VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 8}
			live, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{model}, 1, 1, 1, 1, UsageTokens{}, "", nil, false, time.Time{})
			require.NoError(t, err)
			quote, err := svc.FreezeVideoBilling(context.Background(), key, model, "720p", false)
			require.NoError(t, err)
			require.Equal(t, tc.source, quote.FrozenVideoPriceSource)
			require.InDelta(t, tc.want, live.ActualCost, 1e-12)
			data, err := json.Marshal(quote)
			require.NoError(t, err)
			var restored GrokVideoPendingBilling
			require.NoError(t, json.Unmarshal(data, &restored))
			group.VideoPricePlans = nil
			group.ModelPricing = nil
			group.VideoModelPrices = nil
			group.VideoPrice720P = videoPlanPricePtr(99)
			restored.ApplyToVideoResult(result)
			cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{model}, 9, 9, 9, 9, UsageTokens{}, "", nil, false, time.Time{})
			require.NoError(t, err)
			require.InDelta(t, tc.want, cost.ActualCost, 1e-12)
		})
	}
}

func TestVideoQuotePlanOwnsPricesAndInputVideoFact(t *testing.T) {
	group := &Group{ID: 42, RateMultiplier: 1, VideoPricePlans: seedanceTokenPlan(4, 3)}
	svc := videoPlanTestService()
	key := &APIKey{Group: group, UserID: 10}
	quote, err := svc.FreezeVideoBilling(context.Background(), key, "doubao-seedance", "720p", true)
	require.NoError(t, err)
	cell := group.VideoPricePlans[VideoPriceFamilySeedance].PerTokenPrices["720p"]
	*cell.WithInputVideo = 99
	result := &OpenAIForwardResult{Model: "doubao-seedance", BillingModel: "doubao-seedance", VideoResolution: "720p", Usage: OpenAIUsage{OutputTokens: 1_000_000}}
	quote.ApplyToVideoResult(result)
	cost, err := svc.calculateFrozenVideoCost(context.Background(), result, UsageTokens{OutputTokens: 1_000_000}, "", nil)
	require.NoError(t, err)
	require.InDelta(t, 3, cost.ActualCost, 1e-12)
}

func TestVideoQuoteMissingUsageCannotCompleteAtZero(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plan    *domain.VideoPricePlan
		result  OpenAIForwardResult
		wantErr bool
	}{
		{name: "missing tier", plan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{"720p": 0.5}}, result: OpenAIForwardResult{VideoResolution: "1080p", VideoDurationSeconds: 8}, wantErr: true},
		{name: "missing seconds", plan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{"720p": 0.5}}, result: OpenAIForwardResult{VideoResolution: "720p"}, wantErr: true},
		{name: "missing tokens", plan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerToken, PerTokenPrices: map[string]domain.VideoTokenPrice{"720p": {WithoutInputVideo: videoPlanPricePtr(3)}}}, result: OpenAIForwardResult{VideoResolution: "720p"}, wantErr: true},
		{name: "invalid mode", plan: &domain.VideoPricePlan{Mode: "broken"}, wantErr: true},
		{name: "lost frozen tariff", wantErr: true},
		{name: "explicit zero", plan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPricePtr(0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &Group{ID: lockedMediaGroupID, Status: StatusActive, Platform: PlatformOpenAI, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"doubao-seedance"}, BillingMode: BillingModeVideo, PerRequestPrice: videoPlanPricePtr(1)}}}
			f := newLockedMediaFixture(t, group, "doubao-seedance")
			result := tc.result
			result.Model = "doubao-seedance"
			result.BillingModel = result.Model
			result.FrozenVideoPlan = tc.plan
			result.FrozenVideoPriceSource = "plan"
			result.RequestID = "missing-usage-" + tc.name
			err := f.recordWithoutBundle(t, &result)
			if tc.wantErr {
				require.Error(t, err)
				require.Nil(t, f.billingRepo.lastCmd)
				require.Nil(t, f.usageRepo.lastLog)
			} else {
				require.NoError(t, err)
				require.NotNil(t, f.usageRepo.lastLog)
				require.Zero(t, f.usageRepo.lastLog.ActualCost)
			}
		})
	}
}

func TestVideoQuoteCardOwnsPricesCurrencyAndMode(t *testing.T) {
	group := &Group{ID: 42, RateMultiplier: 1.5, ModelPricing: []ChannelModelPricing{{Models: []string{"grok-imagine-video"}, Currency: "CNY", BillingMode: BillingModeVideo, Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: videoPlanPricePtr(0.7)}}}}}
	svc := videoPlanTestService()
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	key := &APIKey{Group: group}
	quote, err := svc.FreezeVideoBilling(context.Background(), key, "grok-imagine-video", "720p", false)
	require.NoError(t, err)
	*group.ModelPricing[0].Intervals[0].PerRequestPrice = 99
	group.ModelPricing[0].Currency = "USD"
	data, err := json.Marshal(quote)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &quote))
	result := &OpenAIForwardResult{BillingModel: "grok-imagine-video", VideoResolution: "720p", VideoDurationSeconds: 8, VideoCount: 1}
	quote.ApplyToVideoResult(result)
	cost, err := svc.calculateFrozenVideoCost(context.Background(), result, UsageTokens{}, "", nil)
	require.NoError(t, err)
	require.InDelta(t, 8.4, cost.ActualCost, 1e-12)
	require.Equal(t, "CNY", cost.PricingCurrency)
	result.FrozenVideoPricing = &FrozenVideoPricing{Mode: "broken"}
	_, err = svc.calculateFrozenVideoCost(context.Background(), result, UsageTokens{}, "", nil)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestVideoQuoteTokenCardBeatsPlanAndSurvivesJSON(t *testing.T) {
	model := "grok-imagine-video"
	group := &Group{ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 2, VideoRateIndependent: true, VideoRateMultiplier: 9,
		VideoPricePlans: map[string]domain.VideoPricePlan{model: {Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{"720p": 99}}},
		ModelPricing:    []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, Currency: "CNY", InputPrice: videoPlanPricePtr(3), OutputPrice: videoPlanPricePtr(5), CacheReadPrice: videoPlanPricePtr(0), CacheWritePrice: videoPlanPricePtr(0)}},
	}
	f := newLockedMediaFixture(t, group, model)
	f.svc.resolver = NewModelPricingResolver(nil, f.svc.billingService)
	userRate := 0.5
	f.svc.userGroupRateResolver = newUserGroupRateResolver(&userGroupRateRepoHotpathStub{rate: &userRate}, nil, time.Minute, nil, "")
	result := &OpenAIForwardResult{RequestID: "token-live", Model: model, BillingModel: model, VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 8, Usage: OpenAIUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}}
	require.NoError(t, f.recordWithoutBundle(t, result))
	before := f.usageRepo.lastLog.ActualCost
	quote, err := f.svc.FreezeVideoBilling(context.Background(), f.key, model, "720p", false)
	require.NoError(t, err)
	require.Nil(t, quote.FrozenVideoPlan)
	require.NotNil(t, quote.FrozenVideoPricing)
	data, err := json.Marshal(quote)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &quote))
	group.ModelPricing = nil
	group.RateMultiplier = 20
	quote.ApplyToVideoResult(result)
	result.RequestID = "token-frozen"
	require.NoError(t, f.recordWithoutBundle(t, result))
	require.InDelta(t, before, f.usageRepo.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 0.5, f.usageRepo.lastLog.RateMultiplier, 1e-12)
	var snapshot struct {
		Currency string `json:"currency"`
	}
	require.NoError(t, json.Unmarshal(f.usageRepo.lastLog.PricingSnapshot, &snapshot))
	require.Equal(t, "CNY", snapshot.Currency)
}

func TestVideoQuoteWorkerFallsBackToTaskSnapshotAndRetriesMissingUsage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint("missing_tokens=", missing), func(t *testing.T) {
			group := &Group{ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"doubao-seedance"}, BillingMode: BillingModeVideo, PerRequestPrice: videoPlanPricePtr(1)}}}
			f := newLockedMediaFixture(t, group, "doubao-seedance")
			pending := &GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance", VideoResolution: "720p", HasInputVideo: true, FrozenVideoPlan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerToken, PerTokenPrices: map[string]domain.VideoTokenPrice{"720p": {WithInputVideo: videoPlanPricePtr(3), WithoutInputVideo: videoPlanPricePtr(4)}}}, FrozenVideoMultiplier: videoPlanPricePtr(0.5)}
			snapshot, err := json.Marshal(pending)
			require.NoError(t, err)
			repo := &fakeVideoTaskRepo{}
			billing := newFakeVideoBilling()
			body := []byte(`{"status":"succeeded","model":"doubao-seedance","usage":{"completion_tokens":1000000}}`)
			if missing {
				body = []byte(`{"status":"succeeded","model":"doubao-seedance"}`)
			}
			query := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: body}
			worker := newTestVideoTaskWorker(t, repo, billing, query)
			worker.billing = &pr3ReviewRealBilling{fakeVideoBilling: billing, gateway: f.svc}
			worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: f.key}
			worker.accountRepo = &fakeVideoAccountRepo{account: f.account}
			seedVideoTask(t, repo, func(task *VideoTask) {
				task.Platform = VideoTaskPlatformSeedance
				task.Model = "doubao-seedance"
				task.BillingModel = task.Model
				task.UserID = lockedMediaUserID
				task.APIKeyID = lockedMediaKeyID
				task.GroupID = lockedMediaGroupID
				task.AccountID = &f.account.ID
				task.BillingSnapshot = snapshot
				task.CreatedAt = time.Now().Add(-25 * time.Hour)
			})
			_, _, err = worker.RunDue(context.Background())
			require.NoError(t, err)
			if missing {
				require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey))
				require.Nil(t, f.billingRepo.lastCmd)
			} else {
				require.Equal(t, VideoTaskStatusCompleted, repo.statusOf(videoTaskTestTaskKey))
				require.InDelta(t, 1.5, f.billingRepo.lastCmd.BalanceCost, 1e-12)
			}
		})
	}
}

func TestVideoQuoteGrokTokenWorkerRequiresAndPreservesUsage(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			model := "grok-imagine-video"
			group := &Group{ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{model}, BillingMode: BillingModeToken, InputPrice: videoPlanPricePtr(0), OutputPrice: videoPlanPricePtr(0.000005), CacheReadPrice: videoPlanPricePtr(0), CacheWritePrice: videoPlanPricePtr(0)}}}
			f := newLockedMediaFixture(t, group, model)
			f.svc.resolver = NewModelPricingResolver(nil, f.svc.billingService)
			quote, err := f.svc.FreezeVideoBilling(context.Background(), f.key, model, "720p", false)
			require.NoError(t, err)
			quote.Model = model
			quote.BillingModel = model
			quote.VideoResolution = "720p"
			snapshot, err := json.Marshal(quote)
			require.NoError(t, err)
			repo := &fakeVideoTaskRepo{}
			billing := newFakeVideoBilling()
			body := []byte(`{"status":"done","video":{"url":"https://example.com/video.mp4"},"usage":{"completion_tokens":1000000}}`)
			if missing {
				body = []byte(`{"status":"done","video":{"url":"https://example.com/video.mp4"}}`)
			}
			worker := newTestVideoTaskWorker(t, repo, billing, &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: body})
			worker.billing = &pr3ReviewRealBilling{fakeVideoBilling: billing, gateway: f.svc}
			worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: f.key}
			worker.accountRepo = &fakeVideoAccountRepo{account: f.account}
			seedVideoTask(t, repo, func(task *VideoTask) {
				task.Model = model
				task.BillingModel = model
				task.UserID = lockedMediaUserID
				task.APIKeyID = lockedMediaKeyID
				task.GroupID = lockedMediaGroupID
				task.AccountID = &f.account.ID
				task.BillingSnapshot = snapshot
			})
			_, _, err = worker.RunDue(context.Background())
			require.NoError(t, err)
			if missing {
				require.Equal(t, VideoTaskStatusProcessing, repo.statusOf(videoTaskTestTaskKey))
				require.Nil(t, f.billingRepo.lastCmd)
			} else {
				require.Equal(t, VideoTaskStatusCompleted, repo.statusOf(videoTaskTestTaskKey))
				require.InDelta(t, 5, f.billingRepo.lastCmd.BalanceCost, 1e-12)
			}
		})
	}
}

func TestVideoQuoteContentCompletionPreservesTokenUsage(t *testing.T) {
	upstream := &grokMediaContentUpstreamStub{responses: []*http.Response{
		grokMediaContentStatusResponse(`{"status":"done","video":{"url":"https://relay.example/v1/videos/task-1/content"},"usage":{"prompt_tokens":10,"completion_tokens":20}}`),
		{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("video"))},
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodGet, "https://api.example/v1/videos/task-1/content", nil)
	result, err := svc.ForwardGrokMedia(context.Background(), c, grokMediaContentTestAccount(), GrokMediaEndpointVideoContent, "task-1", nil, "")
	require.NoError(t, err)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 20, result.Usage.OutputTokens)
}

func TestVideoQuoteChannelRetainsTimeAndReasoningMultipliers(t *testing.T) {
	model := "grok-imagine-video"
	group := &Group{ID: 42, RateMultiplier: 1.5}
	at := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	card := &ChannelModelPricing{Models: []string{model}, Currency: "USD", BillingMode: BillingModeToken, InputPrice: videoPlanPricePtr(0), OutputPrice: videoPlanPricePtr(0.0000005), CacheReadPrice: videoPlanPricePtr(0), CacheWritePrice: videoPlanPricePtr(0), ReasoningEffortMultipliers: map[string]float64{"high": 3}, TimePricing: &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "09:00", EndTime: "11:00", Multiplier: 2}}}}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.groupPlatform[42] = PlatformGrok
	cache.channelByGroupID[42] = &Channel{Status: StatusActive}
	cache.pricingByGroupModel[channelModelKey{groupID: 42, platform: PlatformGrok, model: model}] = card
	channel := &ChannelService{}
	channel.cache.Store(cache)
	svc := videoPlanTestService()
	svc.resolver = NewModelPricingResolver(channel, svc.billingService)
	ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, at)
	quote, err := svc.FreezeVideoBilling(ctx, &APIKey{Group: group}, model, "720p", false)
	require.NoError(t, err)
	card.ReasoningEffortMultipliers["high"] = 99
	card.TimePricing.Periods[0].Multiplier = 99
	data, err := json.Marshal(quote)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &quote))
	result := &OpenAIForwardResult{Model: model, BillingModel: model, VideoResolution: "720p", VideoDurationSeconds: 8, VideoCount: 1, ReasoningEffort: optionalTrimmedStringPtr("high")}
	result.Usage.OutputTokens = 1_000_000
	quote.ApplyToVideoResult(result)
	cost, err := svc.calculateFrozenVideoCost(context.Background(), result, UsageTokens{OutputTokens: 1_000_000}, "", nil)
	require.NoError(t, err)
	require.InDelta(t, 4.5, cost.ActualCost, 1e-12)
	require.Equal(t, "USD", cost.PricingCurrency)
	require.Equal(t, string(BillingModeToken), cost.BillingMode)
}

func TestVideoQuoteSeedanceLedgerRetainsFrozenMultiplier(t *testing.T) {
	group := &Group{ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 2, VideoPricePlans: map[string]domain.VideoPricePlan{VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPricePtr(0.5)}}, ModelPricing: []ChannelModelPricing{{Models: []string{"doubao-seedance"}, BillingMode: BillingModeVideo, PerRequestPrice: videoPlanPricePtr(1)}}}
	f := newLockedMediaFixture(t, group, "doubao-seedance")
	userRate := 0.5
	f.svc.userGroupRateResolver = newUserGroupRateResolver(&userGroupRateRepoHotpathStub{rate: &userRate}, nil, time.Minute, nil, "")
	quote, err := f.svc.FreezeVideoBilling(context.Background(), f.key, "doubao-seedance", "720p", false)
	require.NoError(t, err)
	group.RateMultiplier = 9
	liveRate := 3.0
	f.svc.userGroupRateResolver = newUserGroupRateResolver(&userGroupRateRepoHotpathStub{rate: &liveRate}, nil, time.Minute, nil, "")
	result := &OpenAIForwardResult{RequestID: "seedance-ledger", Model: "doubao-seedance", BillingModel: "doubao-seedance"}
	quote.ApplyToVideoResult(result)
	require.NoError(t, f.recordWithoutBundle(t, result))
	require.InDelta(t, 0.25, f.billingRepo.lastCmd.BalanceCost, 1e-12)
	require.InDelta(t, 0.5, f.usageRepo.lastLog.RateMultiplier, 1e-12)
}
