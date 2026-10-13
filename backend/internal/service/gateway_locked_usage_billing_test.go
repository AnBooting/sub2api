//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// sharedLockedDedupLedger 用一个 requestID 去重表模拟生产 UsageBillingRepository.Apply
// 的幂等语义（usage_logs ON CONFLICT 同源）。
type sharedLockedDedupLedger struct {
	UsageBillingRepository
	seen   map[string]bool
	calls  int
	last   *UsageBillingCommand
	amount float64
}

func (l *sharedLockedDedupLedger) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	l.calls++
	if l.seen[cmd.RequestID] {
		l.last = nil
		return &UsageBillingApplyResult{Applied: false}, nil
	}
	l.seen[cmd.RequestID] = true
	copy := *cmd
	l.last = &copy
	l.amount += cmd.BalanceCost
	return &UsageBillingApplyResult{Applied: true}, nil
}

// sharedLockedFixture 组装共享网关锁价消费的最小依赖：一个非 composite 分组、
// 一个 Anthropic APIKey 账号，以及冻结 bundle 所需的计费/映射快照。
type sharedLockedFixture struct {
	svc       *GatewayService
	logs      *openAIRecordUsageLogRepoStub
	ledger    *openAIRecordUsageBillingRepoStub
	group     *Group
	account   *Account
	key       *APIKey
	billing   *BillingService
	frozenIn  float64
	frozenOut float64
}

// newSharedLockedFixture 构造分组价（每 token 输入/输出）与账号映射。
// groupPricing==false 时分组不带任何价格（用于缺价 case）。
func newSharedLockedFixture(t *testing.T, groupPricing bool, cacheReadNil bool) *sharedLockedFixture {
	t.Helper()
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &Group{ID: 71, Platform: PlatformAnthropic, Status: StatusActive, RateMultiplier: 1}
	if groupPricing {
		pricing := ChannelModelPricing{Models: []string{"client-model", "claude-sonnet-4"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}
		if cacheReadNil {
			pricing.CacheReadPrice = nil
		}
		group.ModelPricing = []ChannelModelPricing{pricing}
	}
	account := &Account{ID: 81, Name: "shared-1", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Concurrency: 1, Schedulable: true,
		Credentials: map[string]any{"api_key": "fixture-not-a-secret"}}
	key := &APIKey{ID: 91, UserID: 92, GroupID: requestPricingTestInt64(71), Group: group, User: &User{ID: 92, Status: StatusActive}}
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	ledger := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	billing := newTestBillingService()
	svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
	svc.billingService = billing
	svc.resolver = NewModelPricingResolver(nil, billing)
	return &sharedLockedFixture{svc: svc, logs: logs, ledger: ledger, group: group, account: account, key: key, billing: billing, frozenIn: input, frozenOut: output}
}

// lockedContext 冻结 bundle（账号模型的冻结值固定为 frozenModel）并绑定账号。
func (f *sharedLockedFixture) lockedContext(t *testing.T, frozenModel string) context.Context {
	t.Helper()
	deps := inflightEstimateDeps{
		billing:  f.billing,
		resolver: NewModelPricingResolver(nil, f.billing),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{f.account.ID: {Model: frozenModel}}, nil
		},
	}
	ctx, _, err := deps.prepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model", Kind: InflightEstimateToken})
	require.NoError(t, err)
	bound, err := WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
	require.NoError(t, err)
	return bound
}

func (f *sharedLockedFixture) input(result *ForwardResult) *RecordUsageInput {
	return &RecordUsageInput{
		Result:  result,
		APIKey:  f.key,
		User:    f.key.User,
		Account: f.account,
	}
}

func sharedLockedResult(upstreamModel string) *ForwardResult {
	return &ForwardResult{
		RequestID:     "upstream-volatile-1",
		Model:         "client-model",
		UpstreamModel: upstreamModel,
		Usage:         ClaudeUsage{InputTokens: 1000, OutputTokens: 500},
		Duration:      time.Second,
	}
}

// TestSharedLockedUsageUsesFrozenGroupPrice 锁定：locked ctx 下按冻结分组价计费，
// 且扣费键固定为 bundle 的 RequestID（不受客户端/上游 request id 影响）。
func TestSharedLockedUsageUsesFrozenGroupPrice(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	ctx = context.WithValue(ctx, ctxkey.RequestID, "client-volatile")
	err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
	require.NoError(t, err)
	require.NotNil(t, f.ledger.lastCmd)
	want := 1000*f.frozenIn + 500*f.frozenOut
	require.InDelta(t, want, f.ledger.lastCmd.BalanceCost, 1e-15)
	require.Equal(t, GatewayRequestBillingFromContext(ctx).RequestID(), f.ledger.lastCmd.RequestID)
	require.NotNil(t, f.logs.lastLog)
	require.Equal(t, GatewayRequestBillingFromContext(ctx).RequestID(), f.logs.lastLog.RequestID)
	// 冻结 scope 的计费模型是"渠道/账号映射后的客户基线模型"，本 fixture 无渠道映射，
	// 因此就是请求模型本身；账号级上游模型只用于执行事实核对，不参与客户取价。
	require.Equal(t, "client-model", f.logs.lastLog.Model)
}

// lockedContextUnvalidated 冻结 bundle 但跳过绑定校验（WithGatewayRequestBillingAccount
// 会预检价格可用性，缺价 case 用不了）：直接注入账号执行事实，覆盖消费侧自身的
// fail-closed 行为。
func (f *sharedLockedFixture) lockedContextUnvalidated(t *testing.T, frozenModel string) context.Context {
	t.Helper()
	deps := inflightEstimateDeps{
		billing:  f.billing,
		resolver: NewModelPricingResolver(nil, f.billing),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{f.account.ID: {Model: frozenModel}}, nil
		},
	}
	ctx, request, err := deps.prepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model", Kind: InflightEstimateToken})
	require.NoError(t, err)
	ctx = withGatewayRequestBilling(ctx, request)
	return context.WithValue(ctx, gatewayRequestBillingAccountCtxKey{}, gatewayRequestBillingAccount{groupID: f.group.ID, accountID: f.account.ID})
}

// TestSharedLockedUsageIgnoresLivePriceMutation 锁定 freeze 后实时分组价与倍率变更
// 不影响本请求：计费只读冻结快照。
func TestSharedLockedUsageIgnoresLivePriceMutation(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	*f.group.ModelPricing[0].InputPrice = 99
	*f.group.ModelPricing[0].OutputPrice = 99
	f.group.RateMultiplier = 99
	err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
	require.NoError(t, err)
	require.NotNil(t, f.ledger.lastCmd)
	require.InDelta(t, 1000*f.frozenIn+500*f.frozenOut, f.ledger.lastCmd.BalanceCost, 1e-15)
}

// TestSharedLockedUsageRejectsUpstreamModelMismatch 锁定：实际上游模型与冻结账号模型
// 不一致时 fail-closed，不扣费、不落账。
func TestSharedLockedUsageRejectsUpstreamModelMismatch(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-opus-4.5")))
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
	require.Nil(t, f.logs.lastLog)
}

// TestSharedLockedUsageRejectsUnboundAccount 锁定：ctx 有 bundle 但账号事实未绑定
// 或与入账账号不一致时必须拒绝。
func TestSharedLockedUsageRejectsUnboundAccount(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	deps := inflightEstimateDeps{
		billing:  f.billing,
		resolver: NewModelPricingResolver(nil, f.billing),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{f.account.ID: {Model: "claude-sonnet-4"}}, nil
		},
	}
	ctx, _, err := deps.prepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	err = f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)

	other := *f.account
	other.ID = 999
	err = f.svc.RecordUsage(f.lockedContext(t, "claude-sonnet-4"), f.input(sharedLockedResult("claude-sonnet-4")))
	require.NoError(t, err, "sanity: matching account bills")
	f.ledger.lastCmd = nil
	err = f.svc.RecordUsage(f.lockedContext(t, "claude-sonnet-4"), &RecordUsageInput{Result: sharedLockedResult("claude-sonnet-4"), APIKey: f.key, User: f.key.User, Account: &other})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
}

// TestSharedLockedUsageRejectsMediaShapes 锁定纯 token 守卫：图片 / 搜索叠加 / 语音
// 在共享锁价分支一律拒绝，不静默按 token 价错计。
func TestSharedLockedUsageRejectsMediaShapes(t *testing.T) {
	cases := []struct {
		name string
		put  func(*ForwardResult)
	}{
		{name: "image", put: func(r *ForwardResult) { r.ImageCount = 2 }},
		{name: "search", put: func(r *ForwardResult) { r.SearchCount = 3 }},
		{name: "audio", put: func(r *ForwardResult) { r.AudioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: 2} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSharedLockedFixture(t, true, false)
			ctx := f.lockedContext(t, "claude-sonnet-4")
			result := sharedLockedResult("claude-sonnet-4")
			tc.put(result)
			err := f.svc.RecordUsage(ctx, f.input(result))
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Nil(t, f.ledger.lastCmd)
			require.Nil(t, f.logs.lastLog)
		})
	}
}

// TestSharedLockedUsageRequestIDIsIdempotentAcrossAttempts 锁定：同一 bundle 重复入账
// （failover 重试 / worker 重放）沿用同一 requestID，repo 侧幂等只扣一次。
func TestSharedLockedUsageRequestIDIsIdempotentAcrossAttempts(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	dedup := &sharedLockedDedupLedger{seen: map[string]bool{}}
	f.svc.usageBillingRepo = dedup
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4"))))
	require.Equal(t, 1, len(dedup.seen))
	want := 1000*f.frozenIn + 500*f.frozenOut
	require.InDelta(t, want, dedup.amount, 1e-15)
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4"))))
	require.Len(t, dedup.seen, 1, "both attempts must collapse onto one frozen requestID")
	require.Equal(t, 2, dedup.calls)
	require.InDelta(t, want, dedup.amount, 1e-15, "second attempt must not charge again")
}

// TestSharedLockedUsageMissingPriceFailsClosed 锁定：冻结价缺失（分组无价或四维不全）
// 必须拒绝而不是零价放行；显式四维零价则放行。
func TestSharedLockedUsageMissingPriceFailsClosed(t *testing.T) {
	t.Run("no_group_price", func(t *testing.T) {
		f := newSharedLockedFixture(t, false, false)
		ctx := f.lockedContextUnvalidated(t, "claude-sonnet-4")
		err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Nil(t, f.ledger.lastCmd)
	})
	t.Run("missing_cache_read_dimension", func(t *testing.T) {
		f := newSharedLockedFixture(t, true, true)
		ctx := f.lockedContextUnvalidated(t, "claude-sonnet-4")
		err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
		require.Nil(t, f.ledger.lastCmd)
	})
	t.Run("binding_rejects_missing_price_before_forward", func(t *testing.T) {
		f := newSharedLockedFixture(t, false, false)
		deps := inflightEstimateDeps{
			billing:  f.billing,
			resolver: NewModelPricingResolver(nil, f.billing),
			accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
				return map[int64]requestAccountMapping{f.account.ID: {Model: "claude-sonnet-4"}}, nil
			},
		}
		ctx, _, err := deps.prepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
		require.NoError(t, err)
		_, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, "binding is the pre-forward fail-closed gate for missing prices")
	})
	t.Run("explicit_four_zero_prices_allowed", func(t *testing.T) {
		f := newSharedLockedFixture(t, false, false)
		zero := 0.0
		f.group.ModelPricing = []ChannelModelPricing{{Models: []string{"client-model", "claude-sonnet-4"}, InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}}
		ctx := f.lockedContext(t, "claude-sonnet-4")
		err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
		require.NoError(t, err, "explicit zeros are a complete price card, not missing pricing")
		require.NotNil(t, f.ledger.lastCmd)
		require.Zero(t, f.ledger.lastCmd.BalanceCost)
	})
}

// TestUnlockedSharedUsageKeepsLegacyBehavior 锁定：ctx 无 bundle 时旧行为完全不变——
// live 分组价 + 缺失兜底，不因新增锁定分支而改变。
func TestUnlockedSharedUsageKeepsLegacyBehavior(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	// 旧路径的倍率优先级是 用户专属 > 分组默认 > 系统默认，且读的是记录时刻的 live
	// 分组价/倍率：这里把分组倍率改成 1.5，证明无 bundle 时既有行为完全不变（不会被
	// 新增锁定分支影响）。system default 只在分组倍率为 0 时才生效。
	f.svc.cfg = &config.Config{}
	f.svc.cfg.Default.RateMultiplier = 1.1
	f.group.RateMultiplier = 1.5
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "legacy-local")
	err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
	require.NoError(t, err)
	require.NotNil(t, f.ledger.lastCmd)
	want := (1000*f.frozenIn + 500*f.frozenOut) * 1.5
	require.InDelta(t, want, f.ledger.lastCmd.BalanceCost, 1e-12, "legacy path keeps live group multiplier")
	require.Equal(t, "client:legacy-local", f.ledger.lastCmd.RequestID, "legacy requestID derivation unchanged")

	t.Run("unpriced_model_still_uses_legacy_builtin_fallback", func(t *testing.T) {
		// 旧路径对已知模型名仍走内置兜底价（claude-sonnet-4 = $3/$15 per MTok），
		// 这是既有行为；锁价路径在同一缺价场景下是 fail-closed（见缺失价格用例）。
		g := newSharedLockedFixture(t, false, false)
		require.NoError(t, g.svc.RecordUsage(context.Background(), g.input(sharedLockedResult("claude-sonnet-4"))))
		require.NotNil(t, g.ledger.lastCmd)
		require.InDelta(t, 1000*3e-6+500*15e-6, g.ledger.lastCmd.BalanceCost, 1e-12)
	})
}

// TestLockedSharedUsagePropagatesToWorkerContext 锁定：worker 上下文转移保留 bundle
// 与账号事实，异步落账仍走 locked 分支。
func TestLockedSharedUsagePropagatesToWorkerContext(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	worker := TransferGatewayRequestPricingContext(context.Background(), context.WithoutCancel(ctx))
	require.Same(t, GatewayRequestBillingFromContext(ctx), GatewayRequestBillingFromContext(worker))
	require.NoError(t, f.svc.RecordUsage(worker, f.input(sharedLockedResult("claude-sonnet-4"))))
	require.NotNil(t, f.ledger.lastCmd)
	require.InDelta(t, 1000*f.frozenIn+500*f.frozenOut, f.ledger.lastCmd.BalanceCost, 1e-15)
}

// TestLockedSharedUsageEmptyUpstreamModelRejected 锁定：UpstreamModel 为空（Forward 未
// 上报）时不得绕过模型核对——空串不等于任何冻结模型。
func TestLockedSharedUsageEmptyUpstreamModelRejected(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	result := sharedLockedResult("")
	err := f.svc.RecordUsage(ctx, f.input(result))
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, f.ledger.lastCmd)
}

// TestLockedSharedUsageBillingRepoFailureSurfaces 锁定：扣费仓储失败必须回传错误
// （与 legacy 分支一致），而不是静默吞掉。
func TestLockedSharedUsageBillingRepoFailureSurfaces(t *testing.T) {
	f := newSharedLockedFixture(t, true, false)
	f.ledger.err = errors.New("billing repo down")
	ctx := f.lockedContext(t, "claude-sonnet-4")
	err := f.svc.RecordUsage(ctx, f.input(sharedLockedResult("claude-sonnet-4")))
	require.Error(t, err)
	require.NotNil(t, f.logs.lastLog)
	require.Zero(t, f.logs.lastLog.ActualCost, "failed billing must zero the usage log cost")
}
