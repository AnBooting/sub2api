//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// TestParsePricingData_KeepsTokenPricePresenceAndCacheStoragePrice 覆盖 0.2.17 上游同步的
// 定价结构体合并：上游新增的 cache_storage_cost_per_token_per_hour 与本 fork 的
// token_price_presence（缺价判定：区分「显式 0 价」与「价格缺失」）必须同时保留。
// 任一侧被丢掉，本用例即失败。
func TestParsePricingData_KeepsTokenPricePresenceAndCacheStoragePrice(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"merged-model": {
			"litellm_provider": "gemini",
			"mode": "chat",
			"input_cost_per_token": 0.00000075,
			"output_cost_per_token": 0.00000375,
			"cache_read_input_token_cost": 0.000000075,
			"cache_storage_cost_per_token_per_hour": 0.0000005,
			"token_price_presence": {"known": true, "input": false, "output": true, "cache_read": true, "cache_write": true}
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["merged-model"]
	require.NotNil(t, pricing)

	// 上游侧：显式缓存存储单价（每 token 每小时）必须落进价卡。
	require.InDelta(t, 0.0000005, pricing.CacheStorageCostPerTokenPerHour, 1e-15)

	// 本 fork 侧：定价覆盖里的 token_price_presence 必须生效——input 被显式标注为缺失，
	// 解析结果就不能再宣称 input 有价（否则缺价放行护栏会被绕过）。
	require.True(t, pricing.TokenPricePresence.Known)
	require.False(t, pricing.TokenPricePresence.Input)
	require.True(t, pricing.TokenPricePresence.Output)
}

// TestRecordUsageMultipliers_KeepsRequestPricingAt 锁住合并后的计价时刻口径：
// live 路径把请求级定价时刻（锁价快照优先）交给上游抽出的 multiplier helper，
// helper 不得用当前时刻覆盖它——否则跨峰谷/锁价请求会按记录时刻重新定价。
func TestRecordUsageMultipliers_KeepsRequestPricingAt(t *testing.T) {
	svc := &GatewayService{}
	pricingAt := time.Date(2026, 3, 2, 15, 30, 0, 0, time.UTC)

	multiplier, imageMultiplier, gotAt := svc.recordUsageMultipliers(context.Background(), &APIKey{}, nil, pricingAt)

	require.Equal(t, pricingAt, gotAt)
	require.InDelta(t, 1.0, multiplier, 1e-12)
	require.InDelta(t, 1.0, imageMultiplier, 1e-12)
}

// TestResponseModelBillingDeclaration_SuppressedByCacheStorageOnly 锁住上游 0.2.17 的
// response_model 采纳门槛：显式缓存存储费（延长有效期）不是一次模型请求，
// 不得按上游自报的响应模型改价。合并后的 live 路径把 CacheStorageTokenHours
// 计入该门槛，锁价路径则由冻结快照自行处理。
func TestResponseModelBillingDeclaration_SuppressedByCacheStorageOnly(t *testing.T) {
	require.Empty(t, responseModelBillingDeclaration(
		BillingModelSourceResponse, "gemini-3.8-flash", false, true))
	require.Equal(t, "gemini-3.8-flash", responseModelBillingDeclaration(
		BillingModelSourceResponse, "gemini-3.8-flash", false, false))
}

// TestBillingServiceCacheStoragePricePerTokenHour_UsesMergedPricing 确认合并后的价卡
// 能被计费服务读到缓存存储单价（上游新增字段 → 合并后的价卡 → 计费口径全链路打通）。
func TestBillingServiceCacheStoragePricePerTokenHour_UsesMergedPricing(t *testing.T) {
	pricing := &PricingService{}
	data, err := pricing.parsePricingData([]byte(`{"merged-cache-storage-model":{"litellm_provider":"gemini",
		"input_cost_per_token":0.00000075,"output_cost_per_token":0.00000375,
		"cache_storage_cost_per_token_per_hour":0.0000007}}`))
	require.NoError(t, err)
	pricing.pricingData = data
	billing := NewBillingService(&config.Config{}, pricing)

	require.InDelta(t, 0.0000007, billing.CacheStoragePricePerTokenHour("merged-cache-storage-model"), 1e-15)
}

// TestGatewayUsageHasNonTokenDimensions_CacheStorageSuppressesResponseModel 锁住合并后
// live 路径的 response_model 采纳门槛：显式缓存存储费（延长有效期）同样属于非 token 维度，
// 必须抑制「按上游自报模型改价」。上游 0.2.17 起把该维度计入门槛，本 fork 的合并保留。
func TestGatewayUsageHasNonTokenDimensions_CacheStorageSuppressesResponseModel(t *testing.T) {
	require.False(t, gatewayUsageHasNonTokenDimensions(&ForwardResult{}))
	require.True(t, gatewayUsageHasNonTokenDimensions(&ForwardResult{CacheStorageTokenHours: 1}))
	require.True(t, gatewayUsageHasNonTokenDimensions(&ForwardResult{ImageCount: 1}))
	require.True(t, gatewayUsageHasNonTokenDimensions(&ForwardResult{SearchCount: 1}))
}
