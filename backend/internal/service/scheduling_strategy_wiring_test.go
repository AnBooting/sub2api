//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// R5 回归：三档调度策略必须**真正影响调度参数**，而不只是存储与展示。
//
// 背景（外部独立审查 2026-10-16，R5/P2）：SchedulingStrategyProfile 的字段此前
// 没有任何生产调用点，站长切换策略后实际的粘性 TTL 与等待上限完全不变。
//
// 这些用例绑定的是**生产入口**（SettingService.EffectiveSchedulingStrategyProfile
// 与 GatewayService.effectiveSchedulingProfile），不是把 profile 查表逻辑在测试里
// 重写一遍——后者改坏生产代码仍会全绿。

// groupCtxWithStrategy 构造一个**通过 IsGroupContextValid** 的分组上下文。
// 该判定要求 Hydrated + Platform + Status，缺一项分组覆盖值就会被忽略。
func groupCtxWithStrategy(strategy string) context.Context {
	return context.WithValue(context.Background(), ctxkey.Group, &Group{
		ID:                 4242,
		Hydrated:           true,
		Platform:           PlatformOpenAI,
		Status:             StatusActive,
		SchedulingStrategy: strategy,
	})
}

// 全局策略决定粘性 TTL 与等待上限。
func TestEffectiveSchedulingStrategyProfile_GlobalStrategyDrivesParameters(t *testing.T) {
	for _, tc := range []struct {
		strategy    string
		wantTTL     time.Duration
		wantWait    time.Duration
	}{
		{strategy: string(SchedulingStrategyStable), wantTTL: schedulingStableStickyReuseTTL, wantWait: schedulingStableStickyWait},
		{strategy: string(SchedulingStrategyBalanced), wantTTL: schedulingBalancedStickyReuseTTL, wantWait: schedulingBalancedStickyWait},
		{strategy: string(SchedulingStrategyLatency), wantTTL: schedulingLatencyStickyReuseTTL, wantWait: schedulingLatencyStickyWait},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			svc := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
				SettingKeySchedulingStrategy: tc.strategy,
			})
			profile := svc.EffectiveSchedulingStrategyProfile(context.Background(), "")
			require.Equal(t, SchedulingStrategy(tc.strategy), profile.Strategy)
			require.Equal(t, tc.wantTTL, profile.StickyReuseTTL)
			require.Equal(t, tc.wantWait, profile.StickyWait)
		})
	}
}

// latency 必须比 stable 更短（否则策略方向被写反，稳定性倒置）。
func TestEffectiveSchedulingStrategyProfile_LatencyIsShorterThanStable(t *testing.T) {
	stable := SchedulingStrategyProfileFor(SchedulingStrategyStable)
	latency := SchedulingStrategyProfileFor(SchedulingStrategyLatency)
	require.Less(t, latency.StickyReuseTTL, stable.StickyReuseTTL)
	require.Less(t, latency.StickyWait, stable.StickyWait)
	// 错误容忍方向：稳定优先容忍更多（更晚换号），响应优先容忍最少。
	require.Greater(t, stable.ErrorToleranceAttempts, latency.ErrorToleranceAttempts)
}

// 分组覆盖值优先于全局值（PRD-02 AC-B7：未覆盖的组随全局，覆盖的组用自身策略）。
func TestEffectiveSchedulingStrategyProfile_GroupOverrideBeatsGlobal(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: string(SchedulingStrategyStable),
	})

	// 全局是 stable，分组覆盖成 latency -> 取 latency 的参数。
	profile := svc.EffectiveSchedulingStrategyProfile(groupCtxWithStrategy(string(SchedulingStrategyLatency)), "")
	require.Equal(t, SchedulingStrategyLatency, profile.Strategy)
	require.Equal(t, schedulingLatencyStickyReuseTTL, profile.StickyReuseTTL)

	// 分组是 inherit -> 随全局 stable。
	inherited := svc.EffectiveSchedulingStrategyProfile(groupCtxWithStrategy(SchedulingStrategyInherit), "")
	require.Equal(t, SchedulingStrategyStable, inherited.Strategy)
	require.Equal(t, schedulingStableStickyReuseTTL, inherited.StickyReuseTTL)
}

// 缺失/非法/无 ctx 一律回落 balanced，且不 panic（热路径兜底）。
func TestEffectiveSchedulingStrategyProfile_FallsBackToBalanced(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: "not-a-strategy",
	})

	for name, ctx := range map[string]context.Context{
		"nil-ctx":        nil,
		"no-group":       context.Background(),
		"invalid-group":  groupCtxWithStrategy("bogus"),
		"empty-strategy": groupCtxWithStrategy(""),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				profile := svc.EffectiveSchedulingStrategyProfile(ctx, "")
				require.Equal(t, DefaultSchedulingStrategy, profile.Strategy)
				require.Equal(t, schedulingBalancedStickyReuseTTL, profile.StickyReuseTTL)
			})
		})
	}

	// nil SettingService 也必须可用。
	var nilSvc *SettingService
	require.NotPanics(t, func() {
		profile := nilSvc.EffectiveSchedulingStrategyProfile(context.Background(), "")
		require.Equal(t, schedulingBalancedStickyReuseTTL, profile.StickyReuseTTL)
	})
}

// 运维配置 sticky_session_wait_timeout 仍是安全上限：显式调小必须生效，
// 且不得被策略档位放大（方向安全）。这是把等待改为策略驱动后必须保留的边界。
func TestEffectiveStickyWaitKeepsOperatorCeiling(t *testing.T) {
	stable := SchedulingStrategyProfileFor(SchedulingStrategyStable)

	// 未配置 / 配置大于策略：用策略值。
	require.Equal(t, stable.StickyWait, effectiveStickyWait(stable, config.GatewaySchedulingConfig{}))
	require.Equal(t, stable.StickyWait,
		effectiveStickyWait(stable, config.GatewaySchedulingConfig{StickySessionWaitTimeout: time.Hour}))

	// 配置小于策略：运维上限优先，策略不得放大。
	tight := 3 * time.Second
	require.Equal(t, tight,
		effectiveStickyWait(stable, config.GatewaySchedulingConfig{StickySessionWaitTimeout: tight}))

	// 方向检查：结果永不大于配置上限（当配置存在时）。
	for _, strategy := range []SchedulingStrategy{
		SchedulingStrategyStable, SchedulingStrategyBalanced, SchedulingStrategyLatency,
	} {
		profile := SchedulingStrategyProfileFor(strategy)
		got := effectiveStickyWait(profile, config.GatewaySchedulingConfig{StickySessionWaitTimeout: tight})
		require.LessOrEqual(t, got, tight)
	}
}

// GatewayService.effectiveSchedulingProfile 在无 SettingService 时回落默认，
// 有 SettingService 时消费全局策略（证明生产入口真的接到了策略表）。
func TestGatewayServiceEffectiveSchedulingProfileConsumesStrategy(t *testing.T) {
	svc := &GatewayService{}
	require.Equal(t, schedulingBalancedStickyReuseTTL, svc.effectiveSchedulingProfile(context.Background()).StickyReuseTTL,
		"无 SettingService 时回落 balanced")

	svc.settingService = newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: string(SchedulingStrategyLatency),
	})
	require.Equal(t, schedulingLatencyStickyReuseTTL, svc.effectiveSchedulingProfile(context.Background()).StickyReuseTTL,
		"生产入口必须消费全局策略，而不是固定常量")
}

// recordingGatewayCache records the TTL passed to sticky bind, so tests can prove
// the production entry uses the profile value instead of reimplementing lookup logic.
type recordingGatewayCache struct {
	GatewayCache
	bindTTL time.Duration
}

func (c *recordingGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	c.bindTTL = ttl
	return nil
}

func TestGatewayBindStickySession_UsesEffectiveProfileTTL(t *testing.T) {
	for _, tc := range []struct {
		strategy string
		wantTTL  time.Duration
	}{
		{strategy: string(SchedulingStrategyLatency), wantTTL: schedulingLatencyStickyReuseTTL},
		{strategy: string(SchedulingStrategyStable), wantTTL: schedulingStableStickyReuseTTL},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			cache := &recordingGatewayCache{}
			svc := &GatewayService{cache: cache}
			svc.settingService = newSettingServiceForSchedulingStrategyTest(t, map[string]string{
				SettingKeySchedulingStrategy: tc.strategy,
			})
			require.NoError(t, svc.BindStickySession(context.Background(), nil, "session", 1))
			require.Equal(t, tc.wantTTL, cache.bindTTL)
		})
	}
}

func TestGatewayBindStickySession_GroupOverrideUsesProfileTTL(t *testing.T) {
	cache := &recordingGatewayCache{}
	svc := &GatewayService{cache: cache}
	svc.settingService = newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: string(SchedulingStrategyStable),
	})
	require.NoError(t, svc.BindStickySession(groupCtxWithStrategy(string(SchedulingStrategyLatency)), nil, "session", 1))
	require.Equal(t, schedulingLatencyStickyReuseTTL, cache.bindTTL)
}


func TestOpenAIStickyAffinityTTL_ProfileAndExplicitConfig(t *testing.T) {
	base := &OpenAIGatewayService{}
	base.settingService = newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: string(SchedulingStrategyLatency),
	})
	require.Equal(t, schedulingLatencyStickyReuseTTL, base.openAIStickyAffinityTTL(context.Background()))

	withCfg := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIWS: config.GatewayOpenAIWSConfig{StickySessionTTLSeconds: 180}}}}
	withCfg.settingService = newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: string(SchedulingStrategyStable),
	})
	require.Equal(t, 180*time.Second, withCfg.openAIStickyAffinityTTL(context.Background()))
}
