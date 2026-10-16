//go:build unit

// 外部独立审查（2026-10-16，head 5ea1055ea）发现的异步视频结算缺陷回归用例。
//
// 来源：审查报告 R1/R2/R3/R4，复现用例由审查方提供，此处保留为**永久回归**。
// 每条用例修复前都实测失败，修复后必须持续通过：
//   - TestPR3Review_ClaimIsNotDurableSettlement        → R1（Redis 占位 ≠ 持久落账）
//   - TestPR3Review_KeyRebindMustNotDiscardAcceptedVideo → R2（换组不得取消已受理结算）
//   - TestPR3Review_AsyncVideoMustRetainCreateTimePrice  → R3（按受理时报结算）
//   - TestPR3Review_OldSuccessfulVideoStillNeedsSettlement → R4（超窗口不得免费关闭）
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPR3Review_ClaimIsNotDurableSettlement(t *testing.T) {
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)
	// Client has acquired the Redis lock but has not yet persisted any debit.
	claimed, err := billing.ClaimBilled(context.Background(), videoTaskTestTaskKey, videoTaskTestUserID, videoTaskTestAPIKeyID)
	require.NoError(t, err)
	require.True(t, claimed)
	_, _, err = worker.RunDue(context.Background())
	require.NoError(t, err)
	// Client's DB transaction fails, so its error path releases the lock.
	require.NoError(t, billing.ReleaseBilled(context.Background(), videoTaskTestTaskKey, videoTaskTestUserID, videoTaskTestAPIKeyID))
	_, _, err = worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, billing.recordCount(), "worker must retry after the client's failed debit")
}

func TestPR3Review_KeyRebindMustNotDiscardAcceptedVideo(t *testing.T) {
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo)
	key := videoTaskTestAPIKey()
	newGroup := int64(4)
	key.GroupID = &newGroup
	key.Group = &Group{ID: newGroup, Platform: PlatformGrok}
	worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: key}
	_, _, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, billing.recordCount(), "already accepted task must settle against its original group")
}

func TestPR3Review_OldSuccessfulVideoStillNeedsSettlement(t *testing.T) {
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	seedVideoTask(t, repo, func(task *VideoTask) { task.CreatedAt = time.Now().Add(-25 * time.Hour) })
	_, _, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, billing.recordCount(), "restart after 24 hours must reconcile successful tasks")
}

type pr3ReviewRealBilling struct {
	*fakeVideoBilling
	gateway *OpenAIGatewayService
}

func (b *pr3ReviewRealBilling) RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error {
	return b.gateway.RecordUsage(ctx, input)
}

func TestPR3Review_AsyncVideoMustRetainCreateTimePrice(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: 1.5, VideoPrice720P: lockedMediaFloatPtr(0.07),
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"grok-imagine-video"}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: lockedMediaFloatPtr(0.07)}},
		}},
	}
	f := newLockedMediaFixture(t, group, "grok-imagine-video")
	repo := &fakeVideoTaskRepo{}
	billing := newFakeVideoBilling()
	billing.pending = videoTaskPendingSnapshot()
	querier := &fakeVideoStatusQuerier{state: videoTaskUpstreamSucceeded, body: grokDoneBody()}
	worker := newTestVideoTaskWorker(t, repo, billing, querier)
	worker.billing = &pr3ReviewRealBilling{fakeVideoBilling: billing, gateway: f.svc}
	worker.apiKeyRepo = &fakeVideoAPIKeyRepo{key: f.key}
	worker.accountRepo = &fakeVideoAccountRepo{account: f.account}
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.UserID = lockedMediaUserID
		task.APIKeyID = lockedMediaKeyID
		task.GroupID = lockedMediaGroupID
		accountID := lockedMediaAccountID
		task.AccountID = &accountID
	})
	// Accepted at 0.07/second, then administrator publishes a new price.
	group.VideoPrice720P = lockedMediaFloatPtr(0.7)
	group.ModelPricing[0].Intervals[0].PerRequestPrice = lockedMediaFloatPtr(0.7)
	_, _, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.NotNil(t, f.billingRepo.lastCmd)
	require.InDelta(t, 0.07*8*1.5, f.billingRepo.lastCmd.BalanceCost, 1e-12, "settlement must use the create-time quote")
}
