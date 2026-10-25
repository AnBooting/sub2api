//go:build unit

package service

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// deepseekPeakMultiplierAt：官方峰谷口径（2026-08-23 起生效）
// 高峰时段 01:00–04:00 与 06:00–10:00 UTC（半开区间，仅工作日）；
// 北京时间周六/周日全天低谷；高峰价 = 2× 低谷价。
// 2026-08-24 为周一（工作日），2026-08-22 周六、2026-08-23 周日。
// ---------------------------------------------------------------------------

func TestDeepseekPeakMultiplierAt(t *testing.T) {
	mon := func(hour, min int) time.Time { return time.Date(2026, 8, 24, hour, min, 0, 0, time.UTC) }
	sat := func(hour, min int) time.Time { return time.Date(2026, 8, 22, hour, min, 0, 0, time.UTC) }
	sun := func(hour, min int) time.Time { return time.Date(2026, 8, 23, hour, min, 0, 0, time.UTC) }

	tests := []struct {
		name string
		now  time.Time
		want float64
	}{
		// 工作日高峰窗口边界（半开区间）
		{"weekday 01:00 peak start", mon(1, 0), 2.0},
		{"weekday 03:59 peak upper bound", mon(3, 59), 2.0},
		{"weekday 04:00 peak end", mon(4, 0), 1.0},
		{"weekday 06:00 peak start", mon(6, 0), 2.0},
		{"weekday 09:59 peak upper bound", mon(9, 59), 2.0},
		{"weekday 10:00 peak end", mon(10, 0), 1.0},
		// 工作日低谷时段
		{"weekday 00:00 off-peak", mon(0, 0), 1.0},
		{"weekday 05:00 off-peak", mon(5, 0), 1.0},
		{"weekday 12:00 off-peak", mon(12, 0), 1.0},
		{"weekday 23:59 off-peak", mon(23, 59), 1.0},
		// 北京时间周末全天低谷（即使 UTC 处于高峰时段）
		{"saturday utc 02:00 beijing sat 10:00", sat(2, 0), 1.0},
		{"sunday utc 07:00 beijing sun 15:00", sun(7, 0), 1.0},
		// 北京时间与 UTC 跨日边界：UTC 周六 16:30 = 北京周日 00:30 → 周末低谷
		{"utc saturday 16:30 = beijing sunday 00:30", sat(16, 30), 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, deepseekPeakMultiplierWithCalendar(tt.now, defaultDeepseekCalendar))
		})
	}
}

func TestIsDeepSeekModel(t *testing.T) {
	deepseek := []string{
		"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp",
		"deepseek-chat", "deepseek-reasoner", "deepseek-v3-2-251201",
		"deepseek-coder", "deepseek-foo", "deepseek-v4-pro-0813",
		"DEEPSEEK-V4-PRO", " deepseek-v4-flash ",
	}
	for _, m := range deepseek {
		require.True(t, isDeepSeekModel(m), "model %q should be deepseek", m)
	}

	nonDeepseek := []string{
		"gpt-5.4", "claude-sonnet-4", "deepseekcoder", // 无连字符不算 deepseek- 前缀
		"", " deepseek", // 无连字符后缀
	}
	for _, m := range nonDeepseek {
		require.False(t, isDeepSeekModel(m), "model %q should not be deepseek", m)
	}
}

// ---------------------------------------------------------------------------
// 默认价卡（Source=LiteLLM）按官方峰谷倍率计费；分组/渠道自定义定价不叠加
// ---------------------------------------------------------------------------

func TestCalculateCostUnified_DeepseekDefaultCardPeakMultiplier(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	// 低谷成本（2026-09-10 官方新价）：1000*1.5e-7 + 500*6e-7 + 1000*3e-9 = 4.53e-4
	offPeakTotal := 1000*1.5e-7 + 500*6e-7 + 1000*3e-9

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 周一低谷
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-10)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), // 周一高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal*2, peak.TotalCost, 1e-10)
}

func TestCalculateCostUnified_DeepseekProDefaultCardPeakMultiplier(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	offPeakTotal := 1000*6.6e-7 + 500*1.98e-6 + 1000*2.2e-8

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-10)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 6, 30, 0, 0, time.UTC), // 周一高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal*2, peak.TotalCost, 1e-10)
}

func TestCalculateCostUnified_DeepseekVersionedNamePeakMultiplier(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	offPeakTotal := 1000*1.5e-7 + 500*6e-7 + 1000*3e-9

	offPeak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash-0731", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 周一低谷
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal, offPeak.TotalCost, 1e-10)

	peak, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-flash-0731", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC), // 周一高峰
	})
	require.NoError(t, err)
	require.InDelta(t, offPeakTotal*2, peak.TotalCost, 1e-10)
}

func TestCalculateCostUnified_DeepseekGroupPricingIsOperatorDefined(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	inputPrice := 1e-6
	outputPrice := 2e-6
	group := &Group{
		ID: 1, Name: "ds-group", Platform: PlatformDeepseek, Status: StatusActive,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"deepseek-v4-flash"}, BillingMode: BillingModeToken,
			InputPrice: &inputPrice, OutputPrice: &outputPrice,
		}},
	}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "deepseek-v4-flash", Group: group})
	require.Equal(t, PricingSourceGroup, resolved.Source)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	// 分组自定义价：1000*1e-6 + 500*2e-6 + 1000*3e-9（缓存读沿用官方 flash 价）
	groupTotal := 1000*1e-6 + 500*2e-6 + 1000*3e-9

	for _, pricingAt := range []time.Time{
		time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC), // 低谷
		time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC),  // 高峰
	} {
		cost, err := bs.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: "deepseek-v4-flash", Group: group,
			Tokens: tokens, RateMultiplier: 1.0, Resolver: resolver, PricingAt: pricingAt,
		})
		require.NoError(t, err)
		require.InDelta(t, groupTotal, cost.TotalCost, 1e-10,
			"分组自定义售价不自动叠加官方峰谷倍率（pricingAt=%v）", pricingAt)
	}
}

func TestCalculateCostUnified_NonDeepseekDefaultCardNotScaledByPeak(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	total := 1000*3e-6 + 500*15e-6 // claude-sonnet-4 fallback

	for _, pricingAt := range []time.Time{
		time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC),
	} {
		cost, err := bs.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: "claude-sonnet-4", Tokens: tokens,
			RateMultiplier: 1.0, Resolver: resolver, PricingAt: pricingAt,
		})
		require.NoError(t, err)
		require.InDelta(t, total, cost.TotalCost, 1e-10,
			"非 DeepSeek 模型不应受官方峰谷倍率影响（pricingAt=%v）", pricingAt)
	}
}

// 零值 PricingAt 回退当前时刻（生产契约：请求级 PricingAt → resolved.pricingAt →
// timezone.Now()，见 billing_service.go 的计费时点契约）。
//
// 这条用例**不能**再拿「零值的结果」和「另一个 now 的结果」比：两次调用之间可能
// 恰好跨过 DeepSeek 峰谷边界（北京时间 09:00-12:00 与 14:00-18:00），那样断言就
// 挂在墙上时钟上，同一份代码一天里有一部分时间是红的。
//
// 改法：
//  1. 用**固定时刻**分别算出峰、谷两个基准金额——与运行时刻无关的常量；
//  2. 零值路径解析到的时刻必然落在「当前时刻」所在的那一侧，所以它的金额必须是
//     上面两个基准金额之一（常量集合，任何时刻都成立）；
//  3. 若调用期间墙上时钟没有跨过峰谷边界（用 before/after 两次取时判定），零值
//     路径必须与「显式传入当前时刻」**完全一致**——这是「零值 = 现在」的精确锁定。
//
// 诚实的边界：峰谷倍率是**按小时**变化的，所以当「现在」处在低谷时，「回退到当前
// 时刻」与「回退到零值（公元 1 年）」算出的金额相同，这条用例在低谷时段区分不了
// 这两种实现。要把「零值 = 当前时刻」完全确定性地锁住，看
// billing_channel_time_pricing_test.go：那里用「覆盖全天的渠道分时窗口」把这条
// 契约变成任何时刻都成立。
func TestCalculateCostUnified_DeepseekPricingAtZeroFallsBackToNow(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}
	// V4.1-Flash 官方低谷价（2026-09-10 起）：1000*1.5e-7 + 500*6e-7
	offPeakTotal := 1000*1.5e-7 + 500*6e-7

	totalAt := func(pricingAt time.Time) float64 {
		cost, err := bs.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: "deepseek-v4-flash", Tokens: tokens,
			RateMultiplier: 1.0, Resolver: resolver, PricingAt: pricingAt,
		})
		require.NoError(t, err)
		return cost.TotalCost
	}

	// 固定时刻的峰/谷基准（2026-08-24 周一）：02:00 UTC 在峰，12:00 UTC 在谷。
	peakTotal := totalAt(time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC))
	offPeak := totalAt(time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))
	require.InDelta(t, offPeakTotal*2, peakTotal, 1e-10, "固定峰时刻应套用 ×2 峰谷倍率")
	require.InDelta(t, offPeakTotal, offPeak, 1e-10, "固定谷时刻不应套用峰谷倍率")

	before := timezone.Now()
	zeroTotal := totalAt(time.Time{}) // 零值 → 契约要求回退当前时刻
	after := timezone.Now()

	// 零值路径解析到的时刻落在 before 与 after 之间，峰谷侧与「现在」一致：金额必然
	// 是上面两个固定基准之一（常量集合，与运行时刻无关）。
	require.True(t,
		math.Abs(zeroTotal-peakTotal) <= 1e-10 || math.Abs(zeroTotal-offPeak) <= 1e-10,
		"零值 PricingAt 必须回退到当前时刻，金额应是固定峰/谷基准之一：actual=%v peak=%v offPeak=%v",
		zeroTotal, peakTotal, offPeak)

	// 墙上时钟没有跨过峰谷边界时（before 与 after 的金额相同），零值路径必须与
	// 「显式传入当前时刻」完全一致——这条断言就是「零值 = 现在」的精确锁定。
	beforeTotal := totalAt(before)
	afterTotal := totalAt(after)
	if math.Abs(beforeTotal-afterTotal) <= 1e-10 {
		require.InDelta(t, beforeTotal, zeroTotal, 1e-10,
			"零值 PricingAt 必须与显式传入当前时刻得到同一金额")
	}
}

// ---------------------------------------------------------------------------
// 官方价强制覆盖（远端旧价兜底）与未知 deepseek-* flash 兜底
// ---------------------------------------------------------------------------

func TestGetModelPricing_DeepseekForcesOfficialRatesOverJSON(t *testing.T) {
	// JSON 给任意价（模拟远端旧价/占位价），deepseek-* 必须被强制覆盖为官方低谷价。
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"deepseek-flash":               {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-v4-flash":            {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-v4-pro":              {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-v4-flash-vision-exp": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-chat":                {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
		"deepseek-reasoner":            {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 1e-8},
	}}
	bs := NewBillingService(&config.Config{}, pricingSvc)

	tests := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		// 2026-09-10 官方降价后：deepseek-flash（V4.1-Flash 新名）与旧名
		// deepseek-v4-flash 同按 Flash 新价。
		{"deepseek-flash", 1.5e-7, 6e-7, 3e-9},
		{"deepseek-v4-flash", 1.5e-7, 6e-7, 3e-9},
		{"deepseek-v4-flash-vision-exp", 1.5e-7, 6e-7, 3e-9},
		// 已停服的 chat/reasoner：即使 JSON 有旧条目也按 flash 价兜底。
		{"deepseek-chat", 1.5e-7, 6e-7, 3e-9},
		{"deepseek-reasoner", 1.5e-7, 6e-7, 3e-9},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			// flash 档三档价与 pro→Flash 切换无关，GetModelPricing 断言不随时间翻转。
			pricing, err := bs.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
			// 固定时点（切换点之后的 2026-10-01）复核：仍走 Flash 新价。
			atPricing, err := bs.getModelPricingAt(tt.model, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			require.InDelta(t, tt.input, atPricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, atPricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, atPricing.CacheReadPricePerToken, 1e-15)
			require.True(t, bs.HasIdentifiedTokenPricing(tt.model))
		})
	}

	// pro 档（含版本化名称）：断言经固定时点的 getModelPricingAt，不依赖墙上时钟。
	// 2026-08-01 早于切换点 2026-09-14 04:00 UTC → Pro 价。
	for _, model := range []string{"deepseek-v4-pro", "deepseek-v4-pro-0813"} {
		t.Run(model+"/before-cutoff", func(t *testing.T) {
			pricing, err := bs.getModelPricingAt(model, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			require.InDelta(t, 6.6e-7, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, 1.98e-6, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, 2.2e-8, pricing.CacheReadPricePerToken, 1e-15)
		})
	}

	// 半开边界钉死：2026-09-14 03:59:59 仍 Pro 价，04:00:00 整起 Flash 新价。
	proBefore, err := bs.getModelPricingAt("deepseek-v4-pro", time.Date(2026, 9, 14, 3, 59, 59, 0, time.UTC))
	require.NoError(t, err)
	require.InDelta(t, 6.6e-7, proBefore.InputPricePerToken, 1e-15)
	require.InDelta(t, 1.98e-6, proBefore.OutputPricePerToken, 1e-15)
	require.InDelta(t, 2.2e-8, proBefore.CacheReadPricePerToken, 1e-15)
	proAtCutoff, err := bs.getModelPricingAt("deepseek-v4-pro", time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.InDelta(t, 1.5e-7, proAtCutoff.InputPricePerToken, 1e-15)
	require.InDelta(t, 6e-7, proAtCutoff.OutputPricePerToken, 1e-15)
	require.InDelta(t, 3e-9, proAtCutoff.CacheReadPricePerToken, 1e-15)

	// 版本化名称（不在 JSON / fallbackPrices 精确表中）：按子串归档计价。
	// flash-0731 归 flash 档，三档价与切换无关，GetModelPricing 断言稳定。
	versioned := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{"deepseek-v4-flash-0731", 1.5e-7, 6e-7, 3e-9},
	}
	for _, tt := range versioned {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := bs.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, pricing.CacheReadPricePerToken, 1e-15)
		})
	}
}

func TestGetModelPricing_UnknownDeepseekMapsToFlash(t *testing.T) {
	// JSON 含 $0 占位条目（如旧 deepseek-v3-2-251201）：未知 deepseek-* 不再
	// fail-closed，统一按 flash 价兜底（1.5e-7/6e-7/3e-9），不得按 $0 计费。
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"deepseek-v3-2-251201": {InputCostPerToken: 0, OutputCostPerToken: 0},
	}}
	bs := NewBillingService(&config.Config{}, pricingSvc)

	for _, m := range []string{"deepseek-v3-2-251201", "deepseek-chat", "deepseek-reasoner", "deepseek-foo"} {
		t.Run(m, func(t *testing.T) {
			pricing, err := bs.GetModelPricing(m)
			require.NoError(t, err)
			require.InDelta(t, 1.5e-7, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, 6e-7, pricing.OutputPricePerToken, 1e-15)
			require.InDelta(t, 3e-9, pricing.CacheReadPricePerToken, 1e-15)
		})
	}
}

// ---------------------------------------------------------------------------
// 本地兜底 JSON：无 $0 占位条目，官方模型价格为官方低谷价
// ---------------------------------------------------------------------------

func TestDeepseekPricingFileMatchesOfficialRates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingSvc := &PricingService{}
	pricingData, err := pricingSvc.parsePricingData(data)
	require.NoError(t, err)

	_, ok := pricingData["deepseek-v3-2-251201"]
	require.False(t, ok, "deepseek-v3-2-251201（$0 占位条目）必须从价格表中移除")
	for _, discontinued := range []string{"deepseek-chat", "deepseek-reasoner"} {
		_, ok := pricingData[discontinued]
		require.False(t, ok, "%s 已停止服务，必须从价格表中移除", discontinued)
	}

	tests := []struct {
		model                    string
		input, output, cacheRead float64
	}{
		{"deepseek-flash", 1.5e-7, 6e-7, 3e-9},
		{"deepseek-v4-flash", 1.5e-7, 6e-7, 3e-9},
		{"deepseek-v4-flash-vision-exp", 1.5e-7, 6e-7, 3e-9},
		{"deepseek-v4-pro", 6.6e-7, 1.98e-6, 2.2e-8},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			entry, ok := pricingData[tt.model]
			require.True(t, ok, "model %s must exist in pricing file", tt.model)
			require.InDelta(t, tt.input, entry.InputCostPerToken, 1e-15)
			require.InDelta(t, tt.output, entry.OutputCostPerToken, 1e-15)
			require.InDelta(t, tt.cacheRead, entry.CacheReadInputTokenCost, 1e-15)
		})
	}
}

// ---------------------------------------------------------------------------
// 2026-09-10 官方降价：deepseek-flash（V4.1-Flash 新名）与旧名同价；
// 2026-09-14 04:00 UTC 起 deepseek-v4-pro 按上游路由改按 Flash 价计费
// ---------------------------------------------------------------------------

func TestCalculateCostUnified_DeepseekFlashAndLegacyFlashShareNewRates(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	// 2026-09-10 官方新低谷价：1000*1.5e-7 + 500*6e-7 + 1000*3e-9 = 4.53e-4
	offPeakTotal := 1000*1.5e-7 + 500*6e-7 + 1000*3e-9

	// deepseek-flash 与 deepseek-v4-flash 都取 Flash 新价。
	// 时点取切换日 2026-09-14（周一）12:00 UTC 低谷，峰谷倍率不影响断言。
	for _, model := range []string{"deepseek-flash", "deepseek-v4-flash"} {
		cost, err := bs.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: model, Tokens: tokens,
			RateMultiplier: 1.0, Resolver: resolver,
			PricingAt: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)
		require.InDelta(t, offPeakTotal, cost.TotalCost, 1e-10, "model %s must use new flash rates", model)
	}
}

func TestCalculateCostUnified_DeepseekProRoutesToFlashAtCutoff(t *testing.T) {
	bs := newTestBillingService()
	resolver := NewModelPricingResolver(nil, bs)

	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 1000}
	proTotal := 1000*6.6e-7 + 500*1.98e-6 + 1000*2.2e-8 // 切换前 Pro 价
	flashTotal := 1000*1.5e-7 + 500*6e-7 + 1000*3e-9    // 切换后 Flash 价

	// 切换时点之前（2026-09-13 周日，北京周末全天低谷）：仍按 Pro 价。
	before, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, proTotal, before.TotalCost, 1e-10,
		"deepseek-v4-pro must use Pro rates before the 2026-09-14 04:00 UTC cutoff")

	// 到达切换时点（2026-09-14 04:00 UTC 整，周一低谷窗口边界外）：按 Flash 价。
	after, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, flashTotal, after.TotalCost, 1e-10,
		"deepseek-v4-pro must use Flash rates at/after the 2026-09-14 04:00 UTC cutoff")

	// 版本化名称同口径：切换后 deepseek-v4-pro-0813 也按 Flash 价。
	versioned, err := bs.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "deepseek-v4-pro-0813", Tokens: tokens,
		RateMultiplier: 1.0, Resolver: resolver,
		PricingAt: time.Date(2026, 9, 14, 4, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.InDelta(t, flashTotal, versioned.TotalCost, 1e-10,
		"versioned pro names must also route to Flash rates after the cutoff")
}
