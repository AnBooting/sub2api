//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestReReview_FreezeRetainsConfiguredGrokTariff(t *testing.T) {
	group := &Group{ID: 42, Platform: PlatformOpenAI, RateMultiplier: 1.5, VideoPrice720P: videoPlanPricePtr(0.7)}
	key := &APIKey{ID: 1, GroupID: &group.ID, Group: group}
	svc := videoPlanTestService()
	result := &OpenAIForwardResult{Model: "grok-imagine-video", BillingModel: "grok-imagine-video", VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 8}
	before, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, key, result, group.RateMultiplier)
	require.NoError(t, err)
	require.InDelta(t, 8.4, before.ActualCost, 1e-12)
	quote, err := svc.FreezeVideoBilling(context.Background(), key, result.BillingModel, result.VideoResolution, false)
	require.NoError(t, err)
	quote.ApplyToVideoResult(result)
	mult, source := quote.FrozenVideoMultiplier, quote.FrozenVideoPriceSource
	after, err := svc.calculateOpenAIVideoCost(context.Background(), result.BillingModel, key, result, *mult)
	require.NoError(t, err)
	t.Logf("unchanged configuration: live=%v frozen=%v source=%s", before.ActualCost, after.ActualCost, source)
	require.InDelta(t, before.ActualCost, after.ActualCost, 1e-12, "freezing must preserve configured Grok price")
}
func TestReReview_FreezeRetainsUserGroupRate(t *testing.T) {
	price := 0.5
	group := &Group{ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 2,
		VideoPricePlans: map[string]domain.VideoPricePlan{VideoPriceFamilyGrokImagineVideo: {Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{"720p": price}}},
		ModelPricing:    []ChannelModelPricing{{Models: []string{"grok-imagine-video"}, BillingMode: BillingModeVideo, Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: &price}}}},
	}
	f := newLockedMediaFixture(t, group, "grok-imagine-video")
	userRate := 0.5
	f.svc.userGroupRateResolver = newUserGroupRateResolver(&userGroupRateRepoHotpathStub{rate: &userRate}, nil, time.Minute, nil, "")
	result := &OpenAIForwardResult{RequestID: "review-user-rate-before", Model: "grok-imagine-video", BillingModel: "grok-imagine-video", VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 8}
	require.NoError(t, f.recordWithoutBundle(t, result))
	before := f.billingRepo.lastCmd.BalanceCost
	result.RequestID = "review-user-rate-after"
	// Use the same user-aware capture method as the real create handler.
	quote, err := f.svc.FreezeVideoBilling(context.Background(), f.key, result.BillingModel, result.VideoResolution, false)
	require.NoError(t, err)
	quote.ApplyToVideoResult(result)
	require.NoError(t, f.recordWithoutBundle(t, result))
	after := f.billingRepo.lastCmd.BalanceCost
	t.Logf("user override=%v group default=%v: live=%v frozen=%v", userRate, group.RateMultiplier, before, after)
	require.InDelta(t, before, after, 1e-12, "freezing must retain the user's effective rate")
}
func TestReReview_SeedanceFrozenQuoteSurvivesPlanRemoval(t *testing.T) {
	group := &Group{ID: 42, Platform: PlatformOpenAI, RateMultiplier: 1, VideoPricePlans: map[string]domain.VideoPricePlan{
		VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPricePtr(0.5)},
	}}
	key := &APIKey{ID: 1, GroupID: &group.ID, Group: group}
	svc := videoPlanTestService()
	quote, err := svc.FreezeVideoBilling(context.Background(), key, "doubao-seedance", "", false)
	require.NoError(t, err)
	result := &OpenAIForwardResult{Model: "doubao-seedance", BillingModel: "doubao-seedance"}
	quote.ApplyToVideoResult(result)
	cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"doubao-seedance"}, 1, 1, 1, 1, UsageTokens{}, "", boolPtr(false), time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 0.5, cost.ActualCost, 1e-12)
	group.VideoPricePlans = nil
	cost, err = svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"doubao-seedance"}, 1, 1, 1, 1, UsageTokens{}, "", boolPtr(false), time.Time{})
	t.Logf("after deleting live plan: cost=%+v err=%v", cost, err)
	require.NoError(t, err, "accepted frozen quote must remain billable after live plan removal")
	require.InDelta(t, 0.5, cost.ActualCost, 1e-12)
}
func TestReReview_SeedanceFrozenRequestModeSurvivesLiveTokenMode(t *testing.T) {
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = &GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance", FrozenVideoPlan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPricePtr(0.5)}}
	query := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: []byte(`{"status":"succeeded","model":"doubao-seedance"}`)}
	worker := newTestVideoTaskWorker(t, repo, billing, query)
	key := videoTaskTestAPIKey()
	key.Group.VideoPricePlans = seedanceTokenPlan(4, 3)
	worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: key}
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.Platform = VideoTaskPlatformSeedance
		task.Model = "doubao-seedance"
		task.BillingModel = "doubao-seedance"
	})
	_, _, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, billing.recordCount(), "accepted per_request task must not require tokens because the live plan changed")
}

func TestReReview_WorkerMustNotCloseFrozenSeedanceAtZeroAfterPlanRemoval(t *testing.T) {
	price := 0.5
	group := &Group{ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1,
		VideoPricePlans: map[string]domain.VideoPricePlan{VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: &price}},
		ModelPricing:    []ChannelModelPricing{{Models: []string{"doubao-seedance"}, BillingMode: BillingModeVideo, PerRequestPrice: &price}},
	}
	f := newLockedMediaFixture(t, group, "doubao-seedance")
	quote, err := f.svc.FreezeVideoBilling(context.Background(), f.key, "doubao-seedance", "", false)
	require.NoError(t, err)
	plan, mult := quote.FrozenVideoPlan, quote.FrozenVideoMultiplier
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = &GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance", FrozenVideoPlan: plan, FrozenVideoMultiplier: mult}
	query := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: []byte(`{"status":"succeeded","model":"doubao-seedance","usage":{"completion_tokens":12345}}`)}
	worker := newTestVideoTaskWorker(t, repo, billing, query)
	worker.billing = &pr3ReviewRealBilling{fakeVideoBilling: billing, gateway: f.svc}
	worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: f.key}
	worker.accountRepo = &fakeVideoAccountRepo{account: f.account}
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.UserID = lockedMediaUserID
		task.APIKeyID = lockedMediaKeyID
		task.GroupID = lockedMediaGroupID
		task.AccountID = &f.account.ID
		task.Platform = VideoTaskPlatformSeedance
		task.Model = "doubao-seedance"
		task.BillingModel = "doubao-seedance"
	})
	group.VideoPricePlans = nil
	group.ModelPricing = nil
	_, _, err = worker.RunDue(context.Background())
	require.NoError(t, err)
	require.NotNil(t, f.usageRepo.lastLog)
	t.Logf("task status=%s actual charge=%v frozen per-request price=%v", repo.statusOf(videoTaskTestTaskKey), f.usageRepo.lastLog.ActualCost, price)
	require.InDelta(t, price, f.usageRepo.lastLog.ActualCost, 1e-12, "completed task must be charged at its frozen quote")
}
