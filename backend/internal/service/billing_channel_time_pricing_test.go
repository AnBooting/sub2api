//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 计费时点契约（backend/internal/service/billing_service.go 的 CalculateCostUnified）：
//
//	请求级 CostInput.PricingAt → resolved.pricingAt → timezone.Now()
//
// 零值 PricingAt 的语义是「没有指定计费时刻，按当前时刻计费」，**不能**拿它表达
// 「某个固定时刻落在渠道分时窗口之外」——那会把断言绑到墙上时钟：渠道分时窗口是
// 09:00-12:00（Asia/Shanghai），于是这类断言每天有 3 小时必然红。
//
// 下面两个用例把契约的每一级回退都钉在不依赖挂钟的构造上：
//   - 全天窗口让「当前时刻」必然落在窗口内，期望值因此恒定；
//   - resolved.pricingAt 直接给一个明确的窗口外时刻，期望值同样恒定。
//
// 任何「只在某个时间段才绿」的断言都不允许出现在这一组用例里。

// channelTimeAllDayResolvedForTest 构造覆盖全天的渠道分时窗口：
// 00:00-12:00 与 12:00-00:00（结束时间 00:00 按 24:00 解析），倍率 2。
// 无论几点运行都落在窗口内。
func channelTimeAllDayResolvedForTest(base *ModelPricing) *ResolvedPricing {
	resolved := channelTimeResolvedForTest(base, nil)
	resolved.channelPricing.TimePricing.Periods = []ChannelTimePricingPeriod{
		{StartTime: "00:00", EndTime: "12:00", Multiplier: 2},
		{StartTime: "12:00", EndTime: "00:00", Multiplier: 2},
	}
	return resolved
}

// 全天窗口必须真的覆盖 24 小时——否则「全天」只是注释里的说法，
// 上面那条用例就会重新变成时点依赖。逐小时断言把这条前提钉住。
func TestChannelTimeAllDayWindowCoversEveryHour(t *testing.T) {
	resolved := channelTimeAllDayResolvedForTest(&ModelPricing{InputPricePerToken: 0.001})
	timePricing := resolved.channelPricing.TimePricing
	location, err := loadChannelTimePricingLocation("Asia/Shanghai")
	require.NoError(t, err)
	for hour := 0; hour < 24; hour++ {
		at := time.Date(2026, 8, 17, hour, 30, 0, 0, location)
		require.Equal(t, 2.0, timePricing.MultiplierAt(at),
			"hour=%d 必须落在全天窗口内，否则「零值回退当前时刻」的断言会随时刻变化", hour)
	}
}

// 零值 PricingAt + 零值 resolved.pricingAt → 回退 timezone.Now()。
//
// 全天窗口下必须套用 ×2：若计费时点仍停在零值，ChannelTimePricing.MultiplierAt
// 会直接返回 1.0，这条断言因此能真正区分「回退到当前时刻」与「时点始终为零」。
func TestCalculateCostUnified_ChannelTimePricingZeroPricingAtFallsBackToNow(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	resolved := channelTimeAllDayResolvedForTest(&ModelPricing{InputPricePerToken: 0.001})

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "model",
		Tokens:         UsageTokens{InputTokens: 1000},
		RateMultiplier: 1.0,
		Resolver:       &ModelPricingResolver{},
		Resolved:       resolved,
		PricingAt:      time.Time{}, // 零值：契约要求回退到当前时刻
	})
	require.NoError(t, err)
	require.InDelta(t, 2.0, cost.TotalCost, 1e-12,
		"零值 PricingAt 回退当前时刻后必然落在全天窗口内，必须套用 ×2")
	require.InDelta(t, 2.0, cost.ActualCost, 1e-12, "分时倍率同样作用于实付金额")
}

// 零值 PricingAt + 非零 resolved.pricingAt → 回退到 resolved.pricingAt。
//
// resolved.pricingAt 给一个明确落在 09:00-12:00（Asia/Shanghai）窗口外的时刻，
// 因此断言恒定，不依赖运行时刻。
func TestCalculateCostUnified_ChannelTimePricingZeroPricingAtFallsBackToResolvedPricingAt(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	resolved := channelTimeResolvedForTest(&ModelPricing{InputPricePerToken: 0.001}, nil)
	// 2026-08-17 05:00 UTC = 2026-08-17 13:00（Asia/Shanghai），在 09:00-12:00 窗口外。
	resolved.pricingAt = time.Date(2026, 8, 17, 5, 0, 0, 0, time.UTC)

	cost, err := billing.CalculateCostUnified(CostInput{
		Ctx:            context.Background(),
		Model:          "model",
		Tokens:         UsageTokens{InputTokens: 1000},
		RateMultiplier: 1.0,
		Resolver:       &ModelPricingResolver{},
		Resolved:       resolved,
		PricingAt:      time.Time{}, // 零值：契约要求回退到 resolved.pricingAt
	})
	require.NoError(t, err)
	require.InDelta(t, 1.0, cost.TotalCost, 1e-12,
		"零值 PricingAt 应回退到 resolved.pricingAt（窗口外，不套用倍率）")
}
