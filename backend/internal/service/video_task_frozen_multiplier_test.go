//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// R3 补充回归：倍率也必须按受理时冻结值结算。
//
// 主回归（TestPR3Review_AsyncVideoMustRetainCreateTimePrice）里实时倍率与冻结倍率
// 恰好相同，因此它只能证明「报价被冻结」，**证明不了倍率被冻结**——把冻结倍率的
// 消费点回退成实时解析，主回归仍然全绿（实测）。本用例刻意让两者不同：
// 受理时倍率 1.5，结算前分组倍率被改成 9.0；只有消费冻结值才能得到 0.84。
func TestVideoTaskFrozenMultiplierBeatsLiveMultiplier(t *testing.T) {
	group := &Group{
		ID: lockedMediaGroupID, Platform: PlatformOpenAI, Status: StatusActive,
		RateMultiplier: 9.0,
		VideoPrice720P: lockedMediaFloatPtr(0.07),
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
	worker.groupRepo = &fakeVideoGroupRepo{group: group}
	seedVideoTask(t, repo, func(task *VideoTask) {
		task.UserID = lockedMediaUserID
		task.APIKeyID = lockedMediaKeyID
		task.GroupID = lockedMediaGroupID
		accountID := lockedMediaAccountID
		task.AccountID = &accountID
	})

	_, _, err := worker.RunDue(context.Background())
	require.NoError(t, err)
	require.NotNil(t, f.billingRepo.lastCmd)
	// 受理时冻结倍率 1.5（见 videoTaskPendingSnapshot），实时分组倍率是 9.0。
	require.InDelta(t, 0.07*8*1.5, f.billingRepo.lastCmd.BalanceCost, 1e-12,
		"倍率必须按受理时冻结值结算，改倍率不得影响已提交任务")
	require.Greater(t, math.Abs(f.billingRepo.lastCmd.BalanceCost-0.07*8*9.0), 1e-9,
		"不得使用结算时的实时分组倍率")
}
