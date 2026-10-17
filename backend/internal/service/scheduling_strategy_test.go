//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func resetSchedulingStrategyCacheForTest() {
	schedulingStrategySF.Forget(SettingKeySchedulingStrategy)
	schedulingStrategyCache.Store(&cachedSchedulingStrategy{})
}

// newSettingServiceForSchedulingStrategyTest 构造测试用 SettingService。
//
// parseSettings 会把 Grok 运行时模型映射发布到 xai 包的进程级全局状态，
// 按既有测试做法保存并还原，避免污染同包其他用例。
func newSettingServiceForSchedulingStrategyTest(t *testing.T, seed map[string]string) *SettingService {
	t.Helper()
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })

	resetSchedulingStrategyCacheForTest()
	repo := newMockSettingRepo()
	for k, v := range seed {
		repo.data[k] = v
	}
	return NewSettingService(repo, &config.Config{})
}

// C-4/C-8 契约：全局策略缺省 balanced。
func TestParseSettings_SchedulingStrategyDefaultsToBalanced(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, nil)

	got := svc.parseSettings(map[string]string{})
	require.Equal(t, string(DefaultSchedulingStrategy), got.SchedulingStrategy)
	require.Equal(t, "balanced", got.SchedulingStrategy)
}

func TestParseSettings_SchedulingStrategyReadsStoredValue(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, nil)

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "stable", want: "stable"},
		{raw: "balanced", want: "balanced"},
		{raw: "latency", want: "latency"},
		{raw: " LATENCY ", want: "latency"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got := svc.parseSettings(map[string]string{SettingKeySchedulingStrategy: tc.raw})
			require.Equal(t, tc.want, got.SchedulingStrategy)
		})
	}
}

// 非法/空存量值回落默认，不静默当成某个具体策略。
func TestParseSettings_SchedulingStrategyInvalidFallsBackToDefault(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, nil)

	for _, raw := range []string{"", "   ", "fastest", "inherit", "STABLE-ISH", "balanced,latency"} {
		t.Run("raw="+raw, func(t *testing.T) {
			got := svc.parseSettings(map[string]string{SettingKeySchedulingStrategy: raw})
			require.Equal(t, "balanced", got.SchedulingStrategy)
		})
	}
}

func TestParseSchedulingStrategy_StrictWhitelist(t *testing.T) {
	for _, raw := range []string{"stable", "balanced", "latency", " Stable ", "LATENCY", "balanced "} {
		strategy, ok := ParseSchedulingStrategy(raw)
		require.True(t, ok, "raw=%q must be accepted", raw)
		require.Contains(t, []SchedulingStrategy{
			SchedulingStrategyStable,
			SchedulingStrategyBalanced,
			SchedulingStrategyLatency,
		}, strategy)
	}
	for _, raw := range []string{"", "  ", "inherit", "fast", "balan ced", "stable/balanced"} {
		_, ok := ParseSchedulingStrategy(raw)
		require.False(t, ok, "raw=%q must be rejected", raw)
	}
}

// 分组值解析允许 inherit 哨兵，但绝不接受全局策略里的 inherit。
func TestParseGroupSchedulingStrategy_AllowsInherit(t *testing.T) {
	for _, raw := range []string{"inherit", " INHERIT ", "stable", "balanced", "latency"} {
		value, ok := ParseGroupSchedulingStrategy(raw)
		require.True(t, ok, "raw=%q must be accepted", raw)
		require.Contains(t, []string{"inherit", "stable", "balanced", "latency"}, value)
	}
	for _, raw := range []string{"", "legacy", "global", "balanced/stable"} {
		_, ok := ParseGroupSchedulingStrategy(raw)
		require.False(t, ok, "raw=%q must be rejected", raw)
	}
}

// C-4 有效策略解析：分组非 inherit 覆盖全局，otherwise 随全局。
func TestResolveSchedulingStrategy(t *testing.T) {
	cases := []struct {
		name   string
		group  string
		global string
		want   SchedulingStrategy
	}{
		{name: "group inherit uses global", group: "inherit", global: "latency", want: SchedulingStrategyLatency},
		{name: "empty group uses global", group: "", global: "stable", want: SchedulingStrategyStable},
		{name: "invalid group falls back to global", group: "legacy", global: "latency", want: SchedulingStrategyLatency},
		{name: "group stable overrides latency global", group: "stable", global: "latency", want: SchedulingStrategyStable},
		{name: "group latency overrides stable global", group: "latency", global: "stable", want: SchedulingStrategyLatency},
		{name: "group balanced overrides invalid global", group: "balanced", global: "legacy", want: SchedulingStrategyBalanced},
		{name: "case and space normalised", group: "  STABLE ", global: "latency", want: SchedulingStrategyStable},
		{name: "both empty falls back to default", group: "", global: "", want: DefaultSchedulingStrategy},
		{name: "inherit with empty global falls back to default", group: "inherit", global: "", want: DefaultSchedulingStrategy},
		{name: "inherit with invalid global falls back to default", group: "inherit", global: "fastest", want: DefaultSchedulingStrategy},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResolveSchedulingStrategy(tc.group, tc.global))
		})
	}
}

// 访问器：nil service / nil repo / 键缺省 / 非法值 一律回落 balanced。
func TestSchedulingStrategy_AccessorFallbacks(t *testing.T) {
	ctx := context.Background()

	var nilSvc *SettingService
	require.Equal(t, DefaultSchedulingStrategy, nilSvc.SchedulingStrategy(ctx))

	emptySvc := &SettingService{}
	require.Equal(t, DefaultSchedulingStrategy, emptySvc.SchedulingStrategy(ctx))

	missing := newSettingServiceForSchedulingStrategyTest(t, nil)
	require.Equal(t, DefaultSchedulingStrategy, missing.SchedulingStrategy(ctx))

	invalid := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: "turbo",
	})
	require.Equal(t, DefaultSchedulingStrategy, invalid.SchedulingStrategy(ctx))
}

func TestSchedulingStrategy_AccessorReadsStoredValue(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: "latency",
	})

	require.Equal(t, SchedulingStrategyLatency, svc.SchedulingStrategy(context.Background()))
}

// EffectiveSchedulingStrategy 通过 SettingService 走缓存后的端到端解析。
func TestSettingService_EffectiveSchedulingStrategy(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: "stable",
	})
	ctx := context.Background()

	require.Equal(t, SchedulingStrategyStable, svc.EffectiveSchedulingStrategy(ctx, "inherit"))
	require.Equal(t, SchedulingStrategyLatency, svc.EffectiveSchedulingStrategy(ctx, "latency"))
	require.Equal(t, SchedulingStrategyBalanced, svc.EffectiveSchedulingStrategy(ctx, "balanced"))
	require.Equal(t, SchedulingStrategyStable, svc.EffectiveSchedulingStrategy(ctx, "bogus"))
}

func TestBuildSystemSettingsUpdates_PersistsSchedulingStrategy(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, nil)

	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{
		SchedulingStrategy: "latency",
	})
	require.NoError(t, err)
	require.Equal(t, "latency", updates[SettingKeySchedulingStrategy])
}

// service 层空字符串 = 「调用方未提供」，跳过写入；显式非法值拒绝而不是静默回落。
func TestBuildSystemSettingsUpdates_SchedulingStrategyValidation(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, nil)

	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{})
	require.NoError(t, err)
	require.NotContains(t, updates, SettingKeySchedulingStrategy)

	_, err = svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{SchedulingStrategy: "turbo"})
	require.Error(t, err)

	updates, err = svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{SchedulingStrategy: " Balanced "})
	require.NoError(t, err)
	require.Equal(t, "balanced", updates[SettingKeySchedulingStrategy])
}

// 写入后缓存立即生效（写透），不必等 TTL 过期或再读一次 DB。
func TestUpdateSettings_RefreshesSchedulingStrategyCache(t *testing.T) {
	resetSchedulingStrategyCacheForTest()
	defer resetSchedulingStrategyCacheForTest()
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{SchedulingStrategy: "latency"})
	require.NoError(t, err)
	require.Equal(t, "latency", repo.data[SettingKeySchedulingStrategy])

	// 写透后即使底层数据被改回旧值，访问器也应命中新写入的缓存值。
	repo.data[SettingKeySchedulingStrategy] = "stable"
	require.Equal(t, SchedulingStrategyLatency, svc.SchedulingStrategy(context.Background()))
}

// 部分 payload（未携带该键）不能把缓存写成空值，否则访问器要等到 TTL 才恢复。
func TestUpdateSettings_OmittedSchedulingStrategyDoesNotCacheEmpty(t *testing.T) {
	resetSchedulingStrategyCacheForTest()
	defer resetSchedulingStrategyCacheForTest()
	repo := newMockSettingRepo()
	repo.data[SettingKeySchedulingStrategy] = "stable"
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{FrontendURL: "https://example.test"})
	require.NoError(t, err)
	require.Equal(t, "stable", repo.data[SettingKeySchedulingStrategy])
	require.Equal(t, SchedulingStrategyStable, svc.SchedulingStrategy(context.Background()))
}

// 缓存回填：清空后用底层存储重新填充。
func TestSchedulingStrategy_AccessorBackfillsCache(t *testing.T) {
	svc := newSettingServiceForSchedulingStrategyTest(t, map[string]string{
		SettingKeySchedulingStrategy: "latency",
	})
	ctx := context.Background()

	require.Equal(t, SchedulingStrategyLatency, svc.SchedulingStrategy(ctx))
	cached, ok := schedulingStrategyCache.Load().(*cachedSchedulingStrategy)
	require.True(t, ok)
	require.NotNil(t, cached)
	require.Equal(t, SchedulingStrategyLatency, cached.strategy)
	require.Greater(t, cached.expiresAt, time.Now().UnixNano())

	resetSchedulingStrategyCacheForTest()
	require.Equal(t, SchedulingStrategyLatency, svc.SchedulingStrategy(ctx))
}

// 映射表以代码常量固化：三档策略齐全、取值互异、且不涉及并发/分类开关。
func TestSchedulingStrategyProfiles_CoverAllStrategies(t *testing.T) {
	require.Len(t, schedulingStrategyProfiles, 3)
	for _, strategy := range []SchedulingStrategy{
		SchedulingStrategyStable,
		SchedulingStrategyBalanced,
		SchedulingStrategyLatency,
	} {
		profile, ok := schedulingStrategyProfiles[strategy]
		require.True(t, ok, "missing profile for %s", strategy)
		require.Equal(t, strategy, profile.Strategy)
		require.Greater(t, profile.StickyReuseTTL, time.Duration(0))
		require.Greater(t, profile.StickyWait, time.Duration(0))
		require.Greater(t, profile.ErrorToleranceAttempts, 0)
	}

	stable := SchedulingStrategyProfileFor(SchedulingStrategyStable)
	balanced := SchedulingStrategyProfileFor(SchedulingStrategyBalanced)
	latency := SchedulingStrategyProfileFor(SchedulingStrategyLatency)

	// 稳定优先必须比响应速度优先更保守：更长粘性、更长等待、更多错误容忍。
	require.Greater(t, stable.StickyReuseTTL, latency.StickyReuseTTL)
	require.Greater(t, stable.StickyWait, latency.StickyWait)
	// ErrorToleranceAttempts 的方向：容忍多 = 晚换号 = 稳定优先，容忍少 = 早换号 =
	// 响应速度优先。因此 stable 最高、latency 最低（方向写反会把三档倒置）。
	require.Greater(t, stable.ErrorToleranceAttempts, latency.ErrorToleranceAttempts)
	require.False(t, stable.SwitchBackAfterRecovery)

	// balanced 位于两端之间（三档单调：stable > balanced > latency）。
	require.Less(t, balanced.StickyWait, stable.StickyWait)
	require.Greater(t, balanced.StickyWait, latency.StickyWait)
	require.Less(t, balanced.ErrorToleranceAttempts, stable.ErrorToleranceAttempts)
	require.Greater(t, balanced.ErrorToleranceAttempts, latency.ErrorToleranceAttempts)

	// 钉死三档具体数值，防止方向再次被反转。
	require.Equal(t, 3, stable.ErrorToleranceAttempts)
	require.Equal(t, 2, balanced.ErrorToleranceAttempts)
	require.Equal(t, 1, latency.ErrorToleranceAttempts)

	// 未知策略按默认表项返回完整参数，绝不产生零值。
	fallback := SchedulingStrategyProfileFor(SchedulingStrategy("turbo"))
	require.Equal(t, DefaultSchedulingStrategy, fallback.Strategy)
	require.Equal(t, balanced, fallback)
}

// 分组字段缓存与回填：auth cache 快照版本升级后，分组策略必须保真往返。
func TestAPIKeyAuthSnapshotSchedulingStrategyRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	groupID := int64(77)
	apiKey := &APIKey{
		ID:      1,
		UserID:  2,
		GroupID: &groupID,
		Name:    "scheduling-strategy-roundtrip",
		Status:  StatusActive,
		User: &User{
			ID:          2,
			Email:       "strategy@test.local",
			Status:      StatusActive,
			Concurrency: 5,
		},
		Group: &Group{
			ID:                 groupID,
			Name:               "latency-group",
			Platform:           PlatformOpenAI,
			Status:             StatusActive,
			Hydrated:           true,
			RateMultiplier:     1,
			SubscriptionType:   SubscriptionTypeStandard,
			SchedulingStrategy: "latency",
		},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.Equal(t, "latency", snapshot.Group.SchedulingStrategy)

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.Equal(t, "latency", materialized.Group.SchedulingStrategy)
}

// 旧版本快照（低于当前版本，缺字段）必须被淘汰回源，不得复用。
//
// 这里断言的是「版本严格向前推进」这一不变量，而不是某个历史字面量：每给分组快照
// 增加一个计费/路由字段都必须升版，否则旧缓存会带着缺失字段继续生效
// （scheduling_strategy 是 v26 引入的，video_price_plans 是 v27 引入的）。
func TestAPIKeyAuthSnapshotOldVersionRejectedAfterSchedulingStrategy(t *testing.T) {
	svc := &APIKeyService{}
	// scheduling_strategy 已并入快照；其引入版本是历史下界，当前版本必须不低于它。
	require.GreaterOrEqual(t, apiKeyAuthSnapshotVersion, 26,
		"scheduling_strategy 加入分组快照后版本必须升级")
	// video_price_plans 并入快照后必须再升一版，否则旧快照会让计划恒为空。
	require.GreaterOrEqual(t, apiKeyAuthSnapshotVersion, 27,
		"video_price_plans 加入分组快照后版本必须升级")

	// 任意低于当前版本的快照都必须回源，不能复用。
	_, used, err := svc.applyAuthCacheEntry("sk-stale", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: apiKeyAuthSnapshotVersion - 1},
	})
	require.NoError(t, err)
	require.False(t, used, "低于当前版本的快照必须回源")

	// v25 明确缺少 scheduling_strategy，同样必须回源。
	_, used, err = svc.applyAuthCacheEntry("sk-v25", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 25},
	})
	require.NoError(t, err)
	require.False(t, used, "v25 快照缺少 scheduling_strategy，必须回源")
}
