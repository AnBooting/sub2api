//go:build unit

package handler

import (
	"context"
	"encoding/json"
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

// openaiCCLockedUpstream 是 OpenAI 兼容分支的 Chat Completions 上游桩：
// 返回原生 CC 响应（账号标记为只支持 Chat Completions），并记录账号与出站模型。
type openaiCCLockedUpstream struct {
	service.HTTPUpstream
	mu           sync.Mutex
	calls        []string
	failFirstIDs map[int64]bool
	onFirst      func()
}

func (u *openaiCCLockedUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	model := gjson.GetBytes(body, "model").String()
	u.mu.Lock()
	u.calls = append(u.calls, fmt.Sprintf("%d:%s", accountID, model))
	fail := u.failFirstIDs[accountID]
	if fail {
		delete(u.failFirstIDs, accountID)
	}
	first := len(u.calls) == 1
	u.mu.Unlock()
	if first && u.onFirst != nil {
		u.onFirst()
	}
	if fail {
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader("{\"error\":{\"message\":\"temporary\"}}"))}, nil
	}
	resp := fmt.Sprintf("{\"id\":\"chatcmpl-fixture\",\"object\":\"chat.completion\",\"model\":%q,\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":500}}", model)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"up-1"}}, Body: io.NopCloser(strings.NewReader(resp))}, nil
}

func (u *openaiCCLockedUpstream) callList() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

type openaiCCLockedFixture struct {
	handler  *OpenAIGatewayHandler
	group    *service.Group
	account  *service.Account
	fallback *service.Account
	key      *service.APIKey
	upstream *openaiCCLockedUpstream
	ledger   *lockedHTTPLedger
	rates    *lockedHTTPRate
	logs     *lockedHTTPLogs
}

// newOpenAICCLockedFixture 构造 OpenAI 兼容分支的 Chat Completions 夹具：
// 两个「仅支持 Chat Completions」的 APIKey 账号 + 账号级 model_mapping。
func newOpenAICCLockedFixture(t *testing.T, withPrices bool, nilDimension string) *openaiCCLockedFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		groupID        = int64(7800)
		primaryAcctID  = int64(7810)
		fallbackAcctID = int64(7811)
	)
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1}
	if withPrices {
		pricing := service.ChannelModelPricing{Models: []string{"client-model", "gpt-5.4"}, Platform: service.PlatformOpenAI, BillingMode: service.BillingModeToken,
			InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}
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
		return &service.Account{ID: id, Name: fmt.Sprintf("openai-cc-%d", id), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Priority: priority, Concurrency: 1,
			Credentials: map[string]any{"api_key": "fixture-not-a-secret", "base_url": "https://fixture.invalid",
				"model_mapping": map[string]any{"client-model": "gpt-5.4"}},
			Extra:         map[string]any{"openai_responses_supported": false},
			AccountGroups: []service.AccountGroup{{AccountID: id, GroupID: groupID}}}
	}
	account := newAccount(primaryAcctID, 1)
	fallbackAccount := newAccount(fallbackAcctID, 2)

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account, fallbackAccount}}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxAccountSwitches = 2
	accounts := []service.Account{*account, *fallbackAccount}
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	rates := &lockedHTTPRate{rate: 1.2}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI}}, nil, nil, nil, nil)
	upstream := &openaiCCLockedUpstream{failFirstIDs: map[int64]bool{}}
	cacheSvc := service.NewBillingCacheService(&lockedHTTPFunds{balance: 100}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cacheSvc.Stop)
	svc := service.NewOpenAIGatewayService(repo, logs, ledger, nil, nil, rates, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}),
		billing, nil, cacheSvc, upstream, &service.DeferredService{}, nil, nil,
		service.NewModelPricingResolver(channels, billing), channels, nil, nil, nil)
	h := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), cacheSvc, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	key := &service.APIKey{ID: 7820, UserID: 7830, GroupID: lockedSharedInt64(groupID), Group: group, Status: service.StatusActive,
		User: &service.User{ID: 7830, Status: service.StatusActive, Balance: 100}}
	return &openaiCCLockedFixture{handler: h, group: group, account: account, fallback: fallbackAccount, key: key, upstream: upstream, ledger: ledger, rates: rates, logs: logs}
}

func openaiCCTestInt64(v int64) *int64 { return &v }

const openaiCCLockedBody = `{"model":"client-model","max_completion_tokens":256,"messages":[{"role":"user","content":"hello"}],"stream":false}`

func (f *openaiCCLockedFixture) serve(t *testing.T) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(openaiCCLockedBody)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	f.handler.ChatCompletions(c)
	return recorder, c
}

// TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream 覆盖 D-8：
// OpenAI 平台 Chat Completions 缺价（无价 + 缺任一维度）必须在上游前 503。
func TestOpenAIChatCompletionsLockedPricingMissingRejectedBeforeUpstream(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			f := newOpenAICCLockedFixture(t, tc.priced, tc.nilDim)
			rec, _ := f.serve(t)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Empty(t, f.upstream.callList(), "missing price must fail before any upstream write")
			require.Empty(t, f.ledger.commands)
		})
	}
}

// TestOpenAIChatCompletionsLockedPricingExplicitZerosReachUpstream 覆盖 D-9：
// 显式四维零价是合法定价，必须放行到上游（不得被缺价拒绝）。
func TestOpenAIChatCompletionsLockedPricingExplicitZerosReachUpstream(t *testing.T) {
	f := newOpenAICCLockedFixture(t, true, "")
	zero := 0.0
	for i := range f.group.ModelPricing {
		f.group.ModelPricing[i].InputPrice = &zero
		f.group.ModelPricing[i].OutputPrice = &zero
	}
	rec, _ := f.serve(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 1)
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.0, f.ledger.commands[0].BalanceCost, 1e-12)
}

// TestOpenAIChatCompletionsLockedPricingFrozenAcrossFailover 覆盖 D-10：
// failover 换号后价格不变；bundle 与既有 pricingAt/利润门共存（pricingAt 取冻结时刻）。
func TestOpenAIChatCompletionsLockedPricingFrozenAcrossFailover(t *testing.T) {
	f := newOpenAICCLockedFixture(t, true, "")
	f.upstream.failFirstIDs[f.account.ID] = true
	f.upstream.onFirst = func() {
		for i := range f.group.ModelPricing {
			hot := 99e-6
			f.group.ModelPricing[i].InputPrice = &hot
			f.group.ModelPricing[i].OutputPrice = &hot
		}
		f.rates.mu.Lock()
		f.rates.rate = 99
		f.rates.mu.Unlock()
	}
	rec, _ := f.serve(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 2, "primary must fail and the retry must hit the fallback account")
	require.Contains(t, f.upstream.callList()[1], fmt.Sprintf("%d:", f.fallback.ID))
	require.Len(t, f.ledger.commands, 1)
	// 冻结价 (1000*1e-6 + 500*2e-6) × 冻结用户组倍率 1.2 = 0.0024。
	require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12, "hot price/multiplier change must not affect a frozen request")
	require.True(t, strings.HasPrefix(f.ledger.commands[0].RequestID, "locked:"))
}

// TestOpenAIChatCompletionsLockedPricingUpstreamModelMismatchRejected 覆盖 D-11：
// 首次尝试失败后账号映射被改成冻结值之外的模型（冻结表仍是 gpt-5.4），
// 实际出站模型与冻结账号模型不一致时消费者必须 fail-closed，不能按错价扣费。
func TestOpenAIChatCompletionsLockedPricingUpstreamModelMismatchRejected(t *testing.T) {
	f := newOpenAICCLockedFixture(t, true, "")
	f.upstream.failFirstIDs[f.account.ID] = true
	f.upstream.onFirst = func() {
		// 冻结之后才改 fallback 的账号映射：bundle 里 fallback 仍是 gpt-5.4。
		f.fallback.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.5"}
	}
	rec, _ := f.serve(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 2)
	require.Contains(t, f.upstream.callList()[1], "gpt-5.5", "the failover attempt must actually go out with the changed model")
	require.Empty(t, f.ledger.commands, "an upstream model outside the frozen table must not bill")
	require.Empty(t, f.logs)
}

// TestOpenAIChatCompletionsCompositeStaysLegacy 覆盖非 composite 限定。
func TestOpenAIChatCompletionsCompositeStaysLegacy(t *testing.T) {
	f := newOpenAICCLockedFixture(t, false, "")
	f.group.Platform = service.PlatformComposite
	rec, _ := f.serve(t)
	require.NotEqual(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
}

var _ = json.Marshal
var _ = openaiCCTestInt64
