//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// geminiCachedContentWriteResult 显式缓存写侧的用量事实：缓存 token 按输入费计，
// 外加整个有效期的存储量（token·小时）。
func geminiCachedContentWriteResult(upstreamModel string, tokens int, storageHours float64) *ForwardResult {
	result := sharedLockedResult(upstreamModel)
	result.Usage = ClaudeUsage{InputTokens: tokens}
	result.CacheStorageTokenHours = storageHours
	return result
}

// TestGeminiCachedContentWriteSideUsesSameFrozenPriceVersionAsReadSide 写侧（显式缓存
// 创建/续期的缓存输入费 + 存储费）与读侧（生成请求读缓存）必须用**同一份冻结价卡**
// 与**同一倍率**：两笔用量行的价格快照逐字节相同，且两者都按冻结倍率放大。
//
// 这条不变量成立，管理员在 create 与 generate 之间改价表/改倍率时，才不会出现
// 同一笔缓存业务的两套 price_version。
func TestGeminiCachedContentWriteSideUsesSameFrozenPriceVersionAsReadSide(t *testing.T) {
	f := newLockedCacheStorageFixture(t, true)
	// 冻结之前设定读侧（cache_read）单价与分组倍率：写读两侧都必须按这个倍率放大。
	*f.group.ModelPricing[0].CacheReadPrice = 3e-7
	f.group.RateMultiplier = 2
	ctx := f.lockedContext(t, "claude-sonnet-4")
	bundle := GatewayRequestBillingFromContext(ctx)
	require.NotNil(t, bundle)

	// 写侧报价：显式缓存创建（缓存输入费 + 存储费），按冻结价卡与冻结倍率。
	writeAmount, err := f.svc.QuoteGeminiCachedContentLockedCost(ctx, bundle, f.account,
		"client-model", "claude-sonnet-4", 1000, lockedCacheStorageTokenHours)
	require.NoError(t, err)
	wantWrite := (1000*f.frozenIn + lockedCacheStorageTokenHours*lockedCacheStoragePrice) * 2
	require.InDelta(t, wantWrite, writeAmount, 1e-15, "写侧金额 = 冻结输入费 + 冻结存储费，再乘冻结倍率")

	// 写侧记账：与写侧报价同源 —— 预留金额必须与最终扣费金额一致。
	writeResult := geminiCachedContentWriteResult("claude-sonnet-4", 1000, lockedCacheStorageTokenHours)
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(writeResult)))
	require.NotNil(t, f.ledger.lastCmd)
	require.InDelta(t, writeAmount, f.ledger.lastCmd.BalanceCost, 1e-15, "预留口径必须与记账口径一致")
	require.NotNil(t, f.logs.lastLog)
	writeLog := f.logs.lastLog
	require.NotEmpty(t, writeLog.PricingSnapshot)

	// 读侧：同一冻结价卡下的 cache_read 消费。
	readResult := sharedLockedResult("claude-sonnet-4")
	readResult.Usage = ClaudeUsage{CacheReadInputTokens: 1000}
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(readResult)))
	require.NotNil(t, f.ledger.lastCmd)
	require.InDelta(t, 1000*3e-7*2, f.ledger.lastCmd.BalanceCost, 1e-15, "读侧按冻结 cache_read 单价与同一倍率计费")
	require.NotNil(t, f.logs.lastLog)
	readLog := f.logs.lastLog

	// 价格快照里既有价卡字段也有本次用量的金额字段（金额必然不同），因此只比对
	// 价卡部分：写读两侧必须是同一份冻结价卡（同一 price_version、同一倍率）。
	var writeCard, readCard map[string]any
	require.NoError(t, json.Unmarshal(writeLog.PricingSnapshot, &writeCard))
	require.NoError(t, json.Unmarshal(readLog.PricingSnapshot, &readCard))
	for _, key := range []string{
		"model", "input_per_token", "output_per_token",
		"cache_read_per_token", "cache_write_1h_per_token", "group_multiplier",
	} {
		require.Equal(t, writeCard[key], readCard[key], "写读必须同一 price_version（%s）", key)
	}
}

// TestGeminiCachedContentWriteSideIgnoresLivePriceMutation 冻结之后管理员改价表
// （改存储单价）或改分组倍率，都不能改变已冻结的写侧金额：写侧与读侧都读冻结快照。
func TestGeminiCachedContentWriteSideIgnoresLivePriceMutation(t *testing.T) {
	f := newLockedCacheStorageFixture(t, true)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	bundle := GatewayRequestBillingFromContext(ctx)
	require.NotNil(t, bundle)

	// 冻结之后：实时价卡改价 + 分组价改价 + 分组倍率改。
	f.billing.pricingService.pricingData["client-model"].CacheStorageCostPerTokenPerHour = 0.0000009
	*f.group.ModelPricing[0].InputPrice = 99
	f.group.RateMultiplier = 99

	amount, err := f.svc.QuoteGeminiCachedContentLockedCost(ctx, bundle, f.account,
		"client-model", "claude-sonnet-4", 1000, lockedCacheStorageTokenHours)
	require.NoError(t, err)
	require.InDelta(t, 1000*f.frozenIn+lockedCacheStorageTokenHours*lockedCacheStoragePrice, amount, 1e-15,
		"已冻结的写侧金额不受冻结后的改价/改倍率影响")

	// 记账侧同样按冻结价卡走，金额与报价一致。
	writeResult := geminiCachedContentWriteResult("claude-sonnet-4", 1000, lockedCacheStorageTokenHours)
	require.NoError(t, f.svc.RecordUsage(ctx, f.input(writeResult)))
	require.NotNil(t, f.ledger.lastCmd)
	require.InDelta(t, amount, f.ledger.lastCmd.BalanceCost, 1e-15)
}

// TestGeminiCachedContentWriteSideFailsClosedWhenStoragePriceMissing 冻结价卡里没有
// 显式缓存存储单价时必须 fail-closed：既不能按 0 计（上游已计费的维度，等于白送
// 存储），也不能回落兜底价。
func TestGeminiCachedContentWriteSideFailsClosedWhenStoragePriceMissing(t *testing.T) {
	f := newLockedCacheStorageFixture(t, false)
	ctx := f.lockedContext(t, "claude-sonnet-4")
	bundle := GatewayRequestBillingFromContext(ctx)
	require.NotNil(t, bundle)

	_, err := GeminiCachedContentFrozenStoragePrice(bundle, f.group.ID, f.account.ID)
	require.Error(t, err, "缺存储单价必须在准入阶段就暴露")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	_, err = f.svc.QuoteGeminiCachedContentLockedCost(ctx, bundle, f.account,
		"client-model", "claude-sonnet-4", 1000, lockedCacheStorageTokenHours)
	require.Error(t, err, "缺存储单价不得算出金额")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}
