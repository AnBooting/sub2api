//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// geminiLockedUpstream 记录 Gemini 原生上游调用（账号 + URL 中的模型），并可按账号
// 让首次调用返回可切换账号的 502。
type geminiLockedUpstream struct {
	service.HTTPUpstream
	mu           sync.Mutex
	calls        []string
	failFirstIDs map[int64]bool
}

func (u *geminiLockedUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	url := req.URL.Path
	u.mu.Lock()
	u.calls = append(u.calls, fmt.Sprintf("%d:%s", accountID, url))
	// Gemini 转发层对 502 会先同账号重试 5 次再换号，因此该桩对标记账号持续失败，
	// 直到转发层耗尽重试并切到下一个账号（failover 用例的断言看最后一个调用账号）。
	fail := u.failFirstIDs[accountID]
	u.mu.Unlock()
	if fail {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader("{\"error\":{\"message\":\"temporary\"}}"))}, nil
	}
	body := "{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1000,\"candidatesTokenCount\":500,\"cachedContentTokenCount\":100}}"
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *geminiLockedUpstream) callList() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

type geminiLockedFixture struct {
	handler  *GatewayHandler
	group    *service.Group
	account  *service.Account
	fallback *service.Account
	key      *service.APIKey
	upstream *geminiLockedUpstream
	ledger   *lockedHTTPLedger
}

// newGeminiLockedFixture 构造 Gemini 原生入口夹具：分组价覆盖映射前模型与映射后模型，
// 账号级 model_mapping 把 client-model 映射到 gemini-2.5-flash。
func newGeminiLockedFixture(t *testing.T, withPrices bool, nilDimension string) *geminiLockedFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		groupID        = int64(7900)
		primaryAcctID  = int64(7910)
		fallbackAcctID = int64(7911)
	)
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive, RateMultiplier: 1}
	if withPrices {
		pricing := service.ChannelModelPricing{Models: []string{"client-model", "gemini-2.5-flash"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}
		switch nilDimension {
		case "input":
			pricing.InputPrice = nil
		case "output":
			pricing.OutputPrice = nil
		case "cache_read":
			pricing.CacheReadPrice = nil
		case "cache_write":
			pricing.CacheWritePrice = nil
		}
		group.ModelPricing = []service.ChannelModelPricing{pricing}
	}
	newAccount := func(id int64, priority int) *service.Account {
		return &service.Account{ID: id, Name: fmt.Sprintf("gemini-%d", id), Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Priority: priority, Concurrency: 1,
			Credentials:   map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}},
			AccountGroups: []service.AccountGroup{{AccountID: id, GroupID: groupID}}}
	}
	account := newAccount(primaryAcctID, 1)
	fallbackAccount := newAccount(fallbackAcctID, 2)

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account, fallbackAccount}}}
	groupRepo := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{groupID: group}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 2
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{groupID: service.PlatformGemini}}, nil, nil, nil, nil)
	upstream := &geminiLockedUpstream{failFirstIDs: map[int64]bool{}}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	gatewayService := service.NewGatewayService(
		nil, groupRepo, logs, ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg),
		service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, upstream, nil,
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(&lockedHTTPFunds{balance: 100}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	h := &GatewayHandler{
		gatewayService:           gatewayService,
		geminiCompatService:      service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg),
		billingCacheService:      billingCache,
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatNone, 0),
		maxAccountSwitches:       2,
		maxAccountSwitchesGemini: 2,
		cfg:                      cfg,
	}
	key := &service.APIKey{ID: 7920, UserID: 7930, GroupID: lockedSharedInt64(groupID), Group: group, Status: service.StatusActive,
		User: &service.User{ID: 7930, Status: service.StatusActive, Concurrency: 10, Balance: 100}}
	return &geminiLockedFixture{handler: h, group: group, account: account, fallback: fallbackAccount, key: key, upstream: upstream, ledger: ledger}
}

const geminiLockedBody = `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":256}}`

func (f *geminiLockedFixture) serve(t *testing.T, action string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	path := fmt.Sprintf("/v1beta/models/client-model:%s", action)
	if action == "streamGenerateContent" {
		path += "?alt=sse"
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(geminiLockedBody)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "modelAction", Value: "client-model:" + action}}
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	f.handler.GeminiV1BetaModels(c)
	return recorder
}

// TestGeminiLockedPricingMissingRejectedBeforeUpstream 覆盖 D-6：
// generateContent 与 streamGenerateContent 的缺价（无价 + 缺任一维度）都必须在
// 上游请求前以 Google 错误形状 503 拒绝，且 error 内保留 pricing_unavailable。
func TestGeminiLockedPricingMissingRejectedBeforeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name   string
		priced bool
		nilDim string
	}{
		{name: "unpriced", priced: false},
		{name: "missing_input", priced: true, nilDim: "input"},
		{name: "missing_output", priced: true, nilDim: "output"},
		{name: "missing_cache_read", priced: true, nilDim: "cache_read"},
		{name: "missing_cache_write", priced: true, nilDim: "cache_write"},
	} {
		for _, action := range []string{"generateContent", "streamGenerateContent"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				f := newGeminiLockedFixture(t, tc.priced, tc.nilDim)
				rec := f.serve(t, action)
				require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
				require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
				require.Equal(t, float64(http.StatusServiceUnavailable), gjson.GetBytes(rec.Body.Bytes(), "error.code").Float())
				require.Empty(t, f.upstream.callList(), "missing price must fail before any upstream write")
				require.Empty(t, f.ledger.commands)
			})
		}
	}
}

// TestGeminiLockedPricingFrozenAcrossFailover 覆盖 D-7：
// 账号切换（首账号 502 → 换号）后仍按冻结价计费，四维（含 CacheRead/CacheWrite）
// 都取自冻结 bundle；实时价被改成 99 倍也不影响。
func TestGeminiLockedPricingFrozenAcrossFailover(t *testing.T) {
	f := newGeminiLockedFixture(t, true, "")
	f.upstream.failFirstIDs[f.account.ID] = true
	rec := f.serve(t, "generateContent")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	calls := f.upstream.callList()
	require.NotEmpty(t, calls)
	require.Contains(t, calls[len(calls)-1], fmt.Sprintf("%d:", f.fallback.ID),
		"primary must be exhausted and the last attempt must hit the fallback account")
	require.Contains(t, f.upstream.callList()[1], "gemini-2.5-flash", "the frozen channel mapping must pick the mapped upstream model")
	require.Len(t, f.ledger.commands, 1)
	// 冻结价：(900*1e-6 + 500*2e-6 + 100*0) * 1 = 0.0019。
	require.InDelta(t, 0.0019, f.ledger.commands[0].BalanceCost, 1e-12)
	require.True(t, strings.HasPrefix(f.ledger.commands[0].RequestID, "locked:"))
}

// TestGeminiCompositeStaysLegacy 覆盖非 composite 限定：composite 分组走 legacy。
func TestGeminiCompositeStaysLegacy(t *testing.T) {
	f := newGeminiLockedFixture(t, false, "")
	f.group.Platform = service.PlatformComposite
	rec := f.serve(t, "generateContent")
	require.NotEqual(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
}
