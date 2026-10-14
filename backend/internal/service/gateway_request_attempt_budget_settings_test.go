//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func resetGatewayRequestAttemptBudgetLimitsCacheForTest() {
	gatewayRequestAttemptBudgetLimitsSF.Forget(SettingKeyGatewayMaxAttemptsPerRequest)
	gatewayRequestAttemptBudgetLimitsSF.Forget(SettingKeyGatewayRequestBudgetSeconds)
	gatewayRequestAttemptBudgetLimitsCache.Store(&cachedGatewayRequestAttemptBudgetLimits{})
}

// newSettingServiceForGatewayBudgetTest 构造测试用 SettingService。
//
// parseSettings 会把 Grok 的运行时模型映射选项发布到 xai 包的**进程级全局状态**
// （setting_parse.go 的 SetRuntimeModelMappingOptions）。本文件的用例会调用
// parseSettings，若不还原，后续同包用例会读到被污染的全局映射：本文件名以 "g"
// 开头，排在 openai_model_mapping_test.go 之前，会让
// TestCanonicalOpenAIAccountSchedulingModelMatchesForwardSemantics 的
// "Grok OAuth does not inherit OpenAI Codex aliases" 子用例失败（gpt-5.6 → grok-4.6）。
// 这里按 account_wildcard_test.go / openai_messages_dispatch_test.go 的既有做法保存并还原。
func newSettingServiceForGatewayBudgetTest(t *testing.T, seed map[string]string) *SettingService {
	t.Helper()
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })

	resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	repo := newMockSettingRepo()
	for k, v := range seed {
		repo.data[k] = v
	}
	return NewSettingService(repo, &config.Config{})
}

// C-8 契约：缺省值 16 / 600。
func TestParseSettings_GatewayRequestAttemptBudgetLimits_Defaults(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, nil)

	got := svc.parseSettings(map[string]string{})
	require.Equal(t, DefaultRequestAttemptBudgetSends, got.GatewayMaxAttemptsPerRequest)
	require.Equal(t, 600, got.GatewayRequestBudgetSeconds)
	require.Equal(t, 16, got.GatewayMaxAttemptsPerRequest)
}

func TestParseSettings_GatewayRequestAttemptBudgetLimits_ReadsStoredValues(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, nil)

	got := svc.parseSettings(map[string]string{
		SettingKeyGatewayMaxAttemptsPerRequest: "32",
		SettingKeyGatewayRequestBudgetSeconds:  "900",
	})
	require.Equal(t, 32, got.GatewayMaxAttemptsPerRequest)
	require.Equal(t, 900, got.GatewayRequestBudgetSeconds)
}

// 解析失败或非正数都回退默认（fail-safe：不让 window=0 把全量发送配死）。
func TestParseSettings_GatewayRequestAttemptBudgetLimits_InvalidFallsBackToDefault(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, nil)

	cases := []struct {
		name string
		max  string
		win  string
	}{
		{name: "non-numeric", max: "abc", win: "xyz"},
		{name: "zero", max: "0", win: "0"},
		{name: "negative", max: "-5", win: "-600"},
		{name: "blank", max: "   ", win: "   "},
		{name: "float", max: "16.5", win: "600.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := svc.parseSettings(map[string]string{
				SettingKeyGatewayMaxAttemptsPerRequest: tc.max,
				SettingKeyGatewayRequestBudgetSeconds:  tc.win,
			})
			require.Equal(t, 16, got.GatewayMaxAttemptsPerRequest)
			require.Equal(t, 600, got.GatewayRequestBudgetSeconds)
		})
	}
}

func TestBuildSystemSettingsUpdates_PersistsGatewayRequestAttemptBudget(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, nil)

	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{
		GatewayMaxAttemptsPerRequest: 32,
		GatewayRequestBudgetSeconds:  900,
	})
	require.NoError(t, err)
	require.Equal(t, "32", updates[SettingKeyGatewayMaxAttemptsPerRequest])
	require.Equal(t, "900", updates[SettingKeyGatewayRequestBudgetSeconds])
}

// service 层零值 = 「调用方未提供」（见 OmittedSettingKeys 的既有约定），
// 因此非正/零值只跳过写入、不报错；「显式传非正值必须 400」在 handler 层用指针区分。
func TestBuildSystemSettingsUpdates_SkipsNonPositiveGatewayRequestAttemptBudget(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, nil)

	cases := []struct {
		name string
		in   SystemSettings
	}{
		{name: "max zero", in: SystemSettings{GatewayMaxAttemptsPerRequest: 0, GatewayRequestBudgetSeconds: 600}},
		{name: "max negative", in: SystemSettings{GatewayMaxAttemptsPerRequest: -1, GatewayRequestBudgetSeconds: 600}},
		{name: "window zero", in: SystemSettings{GatewayMaxAttemptsPerRequest: 16, GatewayRequestBudgetSeconds: 0}},
		{name: "window negative", in: SystemSettings{GatewayMaxAttemptsPerRequest: 16, GatewayRequestBudgetSeconds: -600}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			updates, err := svc.buildSystemSettingsUpdates(context.Background(), &in)
			require.NoError(t, err)
			if in.GatewayMaxAttemptsPerRequest > 0 {
				require.Equal(t, strconv.Itoa(in.GatewayMaxAttemptsPerRequest), updates[SettingKeyGatewayMaxAttemptsPerRequest])
			} else {
				require.NotContains(t, updates, SettingKeyGatewayMaxAttemptsPerRequest)
			}
			if in.GatewayRequestBudgetSeconds > 0 {
				require.Equal(t, strconv.Itoa(in.GatewayRequestBudgetSeconds), updates[SettingKeyGatewayRequestBudgetSeconds])
			} else {
				require.NotContains(t, updates, SettingKeyGatewayRequestBudgetSeconds)
			}
		})
	}
}

// 零值字段不写入仓库（保持既有「未提供 = 保持现值」契约，不把存量配置清零）。
func TestUpdateSettings_NonPositiveGatewayBudgetIsNotWritten(t *testing.T) {
	resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	defer resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	repo := newMockSettingRepo()
	repo.data[SettingKeyGatewayMaxAttemptsPerRequest] = "20"
	repo.data[SettingKeyGatewayRequestBudgetSeconds] = "300"
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		GatewayMaxAttemptsPerRequest: 0,
		GatewayRequestBudgetSeconds:  600,
	})
	require.NoError(t, err)
	require.Equal(t, "20", repo.data[SettingKeyGatewayMaxAttemptsPerRequest], "zero value must not overwrite the stored max")
	require.Equal(t, "600", repo.data[SettingKeyGatewayRequestBudgetSeconds])

	err = svc.UpdateSettings(context.Background(), &SystemSettings{
		GatewayMaxAttemptsPerRequest: 16,
		GatewayRequestBudgetSeconds:  0,
	})
	require.NoError(t, err)
	require.Equal(t, "16", repo.data[SettingKeyGatewayMaxAttemptsPerRequest])
	require.Equal(t, "600", repo.data[SettingKeyGatewayRequestBudgetSeconds], "zero value must not overwrite the stored window")
}

func TestUpdateSettings_PersistsGatewayRequestAttemptBudget(t *testing.T) {
	resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	defer resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		GatewayMaxAttemptsPerRequest: 24,
		GatewayRequestBudgetSeconds:  480,
	})
	require.NoError(t, err)
	require.Equal(t, "24", repo.updates[SettingKeyGatewayMaxAttemptsPerRequest])
	require.Equal(t, "480", repo.updates[SettingKeyGatewayRequestBudgetSeconds])
}

// 访问器：nil service / nil repo / 键缺省 / 非正数 一律回退 (16, 600s)。
func TestGatewayRequestAttemptBudgetLimits_AccessorFallbacks(t *testing.T) {
	ctx := context.Background()

	var nilSvc *SettingService
	maxSends, window := nilSvc.GatewayRequestAttemptBudgetLimits(ctx)
	require.Equal(t, 16, maxSends)
	require.Equal(t, 600*time.Second, window)

	emptySvc := &SettingService{}
	maxSends, window = emptySvc.GatewayRequestAttemptBudgetLimits(ctx)
	require.Equal(t, 16, maxSends)
	require.Equal(t, 600*time.Second, window)

	missing := newSettingServiceForGatewayBudgetTest(t, nil)
	maxSends, window = missing.GatewayRequestAttemptBudgetLimits(ctx)
	require.Equal(t, 16, maxSends)
	require.Equal(t, 600*time.Second, window)

	nonPositive := newSettingServiceForGatewayBudgetTest(t, map[string]string{
		SettingKeyGatewayMaxAttemptsPerRequest: "0",
		SettingKeyGatewayRequestBudgetSeconds:  "-1",
	})
	maxSends, window = nonPositive.GatewayRequestAttemptBudgetLimits(ctx)
	require.Equal(t, 16, maxSends)
	require.Equal(t, 600*time.Second, window)

	unparseable := newSettingServiceForGatewayBudgetTest(t, map[string]string{
		SettingKeyGatewayMaxAttemptsPerRequest: "not-a-number",
		SettingKeyGatewayRequestBudgetSeconds:  "not-a-number",
	})
	maxSends, window = unparseable.GatewayRequestAttemptBudgetLimits(ctx)
	require.Equal(t, 16, maxSends)
	require.Equal(t, 600*time.Second, window)
}

func TestGatewayRequestAttemptBudgetLimits_AccessorReadsStoredValues(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, map[string]string{
		SettingKeyGatewayMaxAttemptsPerRequest: "32",
		SettingKeyGatewayRequestBudgetSeconds:  "900",
	})

	maxSends, window := svc.GatewayRequestAttemptBudgetLimits(context.Background())
	require.Equal(t, 32, maxSends)
	require.Equal(t, 900*time.Second, window)
}

// 只配了一个键时，另一个键保持默认（而不是整体回退）。
func TestGatewayRequestAttemptBudgetLimits_AccessorPartialConfig(t *testing.T) {
	svc := newSettingServiceForGatewayBudgetTest(t, map[string]string{
		SettingKeyGatewayMaxAttemptsPerRequest: "32",
	})

	maxSends, window := svc.GatewayRequestAttemptBudgetLimits(context.Background())
	require.Equal(t, 32, maxSends)
	require.Equal(t, 600*time.Second, window)
}

// 写入后缓存立即生效（写透），不必等 TTL 过期或再读一次 DB。
func TestUpdateSettings_RefreshesGatewayRequestAttemptBudgetLimitsCache(t *testing.T) {
	resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	defer resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		GatewayMaxAttemptsPerRequest: 24,
		GatewayRequestBudgetSeconds:  480,
	})
	require.NoError(t, err)

	// 写透后即使底层数据被改回默认，访问器也应命中新写入的缓存值。
	repo.data[SettingKeyGatewayMaxAttemptsPerRequest] = "16"
	repo.data[SettingKeyGatewayRequestBudgetSeconds] = "600"

	maxSends, window := svc.GatewayRequestAttemptBudgetLimits(context.Background())
	require.Equal(t, 24, maxSends)
	require.Equal(t, 480*time.Second, window)
}

// 部分 payload（未携带这两个键）不能把缓存写成 0/0，否则访问器会拿到非法预算。
func TestUpdateSettings_OmittedGatewayBudgetDoesNotCacheZeroLimits(t *testing.T) {
	resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	defer resetGatewayRequestAttemptBudgetLimitsCacheForTest()
	repo := newMockSettingRepo()
	repo.data[SettingKeyGatewayMaxAttemptsPerRequest] = "20"
	repo.data[SettingKeyGatewayRequestBudgetSeconds] = "300"
	svc := NewSettingService(repo, &config.Config{})

	// 直接调用服务层且不带这两个键：零值跳过写入，也不会把缓存写成 0/0。
	err := svc.UpdateSettings(context.Background(), &SystemSettings{FrontendURL: "https://example.test"})
	require.NoError(t, err)
	require.Equal(t, "20", repo.data[SettingKeyGatewayMaxAttemptsPerRequest])
	require.Equal(t, "300", repo.data[SettingKeyGatewayRequestBudgetSeconds])

	maxSends, window := svc.GatewayRequestAttemptBudgetLimits(context.Background())
	require.Equal(t, 20, maxSends)
	require.Equal(t, 300*time.Second, window)
}
