//go:build unit

package handler

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// lockedCacheCatalogWithoutStorage 是"四项 token 价卡齐备、但没有显式缓存存储单价"的价卡。
// 模型名不是内置 Gemini 型号，因此也没有内置兜底存储价：缺价必须被识别出来。
const lockedCacheCatalogWithoutStorage = `{"custom-cache-model":{
	"litellm_provider":"gemini","mode":"chat","input_cost_per_token":0.00000125,"output_cost_per_token":0.00001,
	"cache_creation_input_token_cost":0.00000125,"cache_read_input_token_cost":0.000000125}}`

// lockedCacheCatalogWithStorage 在上一张价卡上补上显式缓存存储单价。
const lockedCacheCatalogWithStorage = `{"custom-cache-model":{
	"litellm_provider":"gemini","mode":"chat","input_cost_per_token":0.00000125,"output_cost_per_token":0.00001,
	"cache_creation_input_token_cost":0.00000125,"cache_read_input_token_cost":0.000000125,
	"cache_storage_cost_per_token_per_hour":0.000001}}`

func lockedCacheCreateBody(model string) string {
	return `{"model":"models/` + model + `","ttl":"86400s","contents":[{"role":"user","parts":[{"text":"doc"}]}]}`
}

// TestGeminiCachedContentsCreate_LockedPriceRejectsMissingStoragePrice 显式缓存的 create
// 已纳入请求锁价：冻结价卡里没有显式缓存存储单价时，必须在上游建缓存**之前**拒绝
// （pricing_unavailable），不建上游缓存、不落费用行。
//
// 用同一张价卡补上存储单价做对照，证明拒绝的原因确实是"缺存储单价"，而不是价卡整体缺价。
func TestGeminiCachedContentsCreate_LockedPriceRejectsMissingStoragePrice(t *testing.T) {
	// 对照组：同样的价卡，只补上存储单价 → 放行。
	envOK := newCachedBillingEnvWithCatalog(t, 500, lockedCacheCatalogWithStorage, 900)
	envOK.upstream.createBody = cachedBillingCreateBody(time.Now().Add(100 * time.Hour))
	recOK := envOK.serve(http.MethodPost, "/v1beta/cachedContents", lockedCacheCreateBody("custom-cache-model"),
		nil, envOK.h.GeminiCachedContentsCreate)
	require.Equal(t, http.StatusOK, recOK.Code, recOK.Body.String())
	require.Equal(t, 1, envOK.upstream.methodCalls(http.MethodPost))

	// 实验组：缺显式缓存存储单价 → 上游之前拒绝。
	env := newCachedBillingEnvWithCatalog(t, 500, lockedCacheCatalogWithoutStorage, 900)
	rec := env.serve(http.MethodPost, "/v1beta/cachedContents", lockedCacheCreateBody("custom-cache-model"),
		nil, env.h.GeminiCachedContentsCreate)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "pricing_unavailable")
	require.Equal(t, 0, env.upstream.methodCalls(http.MethodPost), "缺价不得放行到上游")
	require.Empty(t, env.usage.totals(), "缺价不得落费用行")
}

// lockedReservationCache 记录每次在途预留的金额：用于核对"预留口径 == 记账口径"。
type lockedReservationCache struct {
	service.BillingCache
	mu      sync.Mutex
	balance float64
	history []float64
}

func (c *lockedReservationCache) GetUserBalance(context.Context, int64) (float64, error) {
	return c.balance, nil
}

func (c *lockedReservationCache) ReserveInflightBalance(_ context.Context, _ int64, _ string, amount, _ float64, _ time.Duration) (bool, float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history = append(c.history, amount)
	return true, 0, nil
}

func (c *lockedReservationCache) ReserveInflightScope(_ context.Context, _, _ string, amount float64, _ time.Duration) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history = append(c.history, amount)
	return 0, nil
}

func (c *lockedReservationCache) ReleaseInflightBalance(context.Context, int64, string) error {
	return nil
}

func (c *lockedReservationCache) ReleaseInflightScope(context.Context, string, string) error {
	return nil
}

func (c *lockedReservationCache) reserved() []float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]float64, len(c.history))
	copy(out, c.history)
	return out
}

func enableLockedReservations(t *testing.T, env *cachedBillingEnv, cache *lockedReservationCache) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.Enabled = true
	bcs := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(bcs.Stop)
	env.h.billingCacheService = bcs
}

// TestGeminiCachedContentsCreate_ReservationMatchesLockedCharge 写侧的余额/额度预留
// 必须与最终扣费金额一致（同价版本、同倍率）：出现"按冻结价记账、按 live 价预留"时
// 这条断言会红。
func TestGeminiCachedContentsCreate_ReservationMatchesLockedCharge(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(100 * time.Hour))
	cache := &lockedReservationCache{balance: 500}
	enableLockedReservations(t, env, cache)
	// 冻结在入口（上游调用之前），预留与记账都在上游返回之后：把**实时价表**改在中间
	// （管理员改价），锁价路径的预留与记账都必须仍按冻结价卡 —— 这正是本条要抓的回归：
	// 按冻结价记账、按 live 价预留。
	env.upstream.beforePost = func(int) {
		if p := env.pricing.GetModelPricing("gemini-2.5-pro"); p != nil {
			p.CacheStorageCostPerTokenPerHour = cachedBillingStoragePrice * 10
		}
	}

	rec := env.create()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	totals := env.waitForUsage(t, 1)
	require.Len(t, totals, 1)

	reserved := cache.reserved()
	require.NotEmpty(t, reserved, "写侧必须先按冻结价预留")
	// 最后一笔是确定金额预留（写侧定价预留）：它必须与最终扣费金额一致。
	// 前面的在途预估预留是入口按 token 估算的另一套机制，不参与这个口径。
	require.InDelta(t, totals[0], reserved[len(reserved)-1], 1e-9, "预留金额必须与最终扣费金额一致")
}

// TestGeminiCachedContentsCreate_FrozenMultiplierUnaffectedByAdminChange 写侧金额按
// 请求建立时冻结的倍率计算：管理员在上游建缓存之后改分组倍率，不影响已冻结的写侧金额。
func TestGeminiCachedContentsCreate_FrozenMultiplierUnaffectedByAdminChange(t *testing.T) {
	env := newCachedBillingEnv(t, 500, 900)
	env.upstream.createBody = cachedBillingCreateBody(time.Now().Add(100 * time.Hour))
	// 冻结发生在入口（上游调用之前），计费发生在上游返回之后：把**实时价表**改在中间
	// （管理员改价），锁价路径必须仍然用冻结价卡，live 路径则会跟着变。
	env.upstream.beforePost = func(int) {
		if p := env.pricing.GetModelPricing("gemini-2.5-pro"); p != nil {
			p.CacheStorageCostPerTokenPerHour = cachedBillingStoragePrice * 10
		}
	}

	rec := env.create()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	totals := env.waitForUsage(t, 1)
	require.Len(t, totals, 1)
	require.InDelta(t, cachedBillingTokens*cachedBillingInputPrice+cachedBillingTokens*cachedBillingStoragePrice*24, totals[0], 1e-9,
		"写侧金额按冻结倍率，不受建缓存之后的倍率变更影响")
}
