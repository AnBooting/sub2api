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

// sharedLockedAnthropicUpstream 是共享入口（Chat Completions / Responses 经
// Anthropic 上游转换链）的上游桩：恒定返回 Anthropic SSE（两个入口都会把上游
// 请求强制为流式），并可按账号让首次请求返回可切换账号的 502。
type sharedLockedAnthropicUpstream struct {
	service.HTTPUpstream
	mu            sync.Mutex
	calls         []string
	failFirstIDs  map[int64]bool
	responseModel string
	// onFirst 在首次返回可切换错误时调用，用于在 failover 窗口内篡改实时价格。
	onFirst func()
}

func (u *sharedLockedAnthropicUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	model := gjson.GetBytes(body, "model").String()
	u.mu.Lock()
	u.calls = append(u.calls, fmt.Sprintf("%d:%s", accountID, model))
	fail := u.failFirstIDs[accountID]
	if fail {
		delete(u.failFirstIDs, accountID)
	}
	u.mu.Unlock()
	if fail {
		if u.onFirst != nil {
			u.onFirst()
		}
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader("{\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"temporary\"}}"))}, nil
	}
	responseModel := model
	if u.responseModel != "" {
		responseModel = u.responseModel
	}
	start := fmt.Sprintf("{\"type\":\"message_start\",\"message\":{\"id\":\"msg_fixture\",\"type\":\"message\",\"role\":\"assistant\",\"model\":%q,\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1000,\"output_tokens\":0}}}", responseModel)
	blockStart := "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}"
	delta := "{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}"
	stop := "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":500}}"
	sse := "event: message_start\ndata: " + start + "\n\n" +
		"event: content_block_start\ndata: " + blockStart + "\n\n" +
		"event: content_block_delta\ndata: " + delta + "\n\n" +
		"event: message_delta\ndata: " + stop + "\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"up-1"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
}

func (u *sharedLockedAnthropicUpstream) callList() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

// sharedLockedFixture 是共享入口（Chat Completions 非 OpenAI 分支 / Responses）
// 的端到端夹具：真实计费服务 + 冻结 bundle + 账本/日志桩。
type sharedLockedFixture struct {
	handler  *GatewayHandler
	group    *service.Group
	account  *service.Account
	fallback *service.Account
	key      *service.APIKey
	upstream *sharedLockedAnthropicUpstream
	ledger   *lockedHTTPLedger
	logs     *lockedHTTPLogs
}

// newSharedLockedFixture 构造一个非 OpenAI 平台的共享入口分组：
// 分组价覆盖 client-model 与账号映射后的 claude-sonnet-4，账号级 model_mapping
// 把 client-model 映射到 claude-sonnet-4（用于冻结账号模型与出站模型的核对）。
func newSharedLockedFixture(t *testing.T, withPrices bool, nilDimension string) *sharedLockedFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		groupID        = int64(7600)
		primaryAcctID  = int64(7610)
		fallbackAcctID = int64(7611)
	)
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1}
	if withPrices {
		pricing := service.ChannelModelPricing{Models: []string{"client-model", "claude-sonnet-4"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}
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

	account := &service.Account{ID: primaryAcctID, Name: "primary", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "claude-sonnet-4"}},
		AccountGroups: []service.AccountGroup{{AccountID: primaryAcctID, GroupID: groupID}}}
	fallbackAccount := &service.Account{ID: fallbackAcctID, Name: "fallback", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Priority: 2, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "claude-sonnet-4"}},
		AccountGroups: []service.AccountGroup{{AccountID: fallbackAcctID, GroupID: groupID}}}

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account, fallbackAccount}}}
	groupRepo := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{groupID: group}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 2
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{groupID: service.PlatformAnthropic}}, nil, nil, nil, nil)
	upstream := &sharedLockedAnthropicUpstream{failFirstIDs: map[int64]bool{}}
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
		gatewayService:      gatewayService,
		billingCacheService: billingCache,
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:  2,
		cfg:                 cfg,
	}
	key := &service.APIKey{ID: 7620, UserID: 7630, GroupID: lockedSharedInt64(groupID), Group: group, Status: service.StatusActive,
		User: &service.User{ID: 7630, Status: service.StatusActive, Concurrency: 10, Balance: 100}}
	return &sharedLockedFixture{handler: h, group: group, account: account, fallback: fallbackAccount, key: key, upstream: upstream, ledger: ledger, logs: logs}
}

func lockedSharedInt64(v int64) *int64 { return &v }

func sharedLockedChatBody(stream bool) string {
	return fmt.Sprintf(`{"model":"client-model","max_completion_tokens":256,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)
}

func sharedLockedResponsesBody(stream bool) string {
	return fmt.Sprintf(`{"model":"client-model","max_output_tokens":256,"input":"hello","stream":%t}`, stream)
}

// serve 直接调用指定的共享入口 handler（Chat Completions 或 Responses）。
func (f *sharedLockedFixture) serve(t *testing.T, path, body string, call func(*GatewayHandler, *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	call(f.handler, c)
	return recorder
}

func (f *sharedLockedFixture) serveChat(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return f.serve(t, "/v1/chat/completions", body, (*GatewayHandler).ChatCompletions)
}

func (f *sharedLockedFixture) serveResponses(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return f.serve(t, "/v1/responses", body, (*GatewayHandler).Responses)
}

// ---------- Chat Completions（非 OpenAI 分支） ----------

// TestSharedChatCompletionsLockedPricingMissingRejectedBeforeUpstream 覆盖 D-1：
// 非 OpenAI 平台的 Chat Completions 缺价必须在选号/上游前 503 pricing_unavailable，
// 四种缺价形态（无价、缺 input、缺 output、缺 cache）与流式/非流式一致。
func TestSharedChatCompletionsLockedPricingMissingRejectedBeforeUpstream(t *testing.T) {
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
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				f := newSharedLockedFixture(t, tc.priced, tc.nilDim)
				rec := f.serveChat(t, sharedLockedChatBody(stream))
				require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
				require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
				require.Empty(t, f.upstream.callList(), "missing price must fail before any upstream write")
				require.Empty(t, f.ledger.commands)
			})
		}
	}
}

// TestSharedChatCompletionsLockedPricingFrozenAcrossFailover 覆盖 D-2：
// 请求建立时冻结价格，首账号 502 换号后仍按冻结价计费（实时价被改成 99 倍也不受影响），
// 且四个维度（含 CacheRead/CacheWrite）都按冻结价计算。
func TestSharedChatCompletionsLockedPricingFrozenAcrossFailover(t *testing.T) {
	f := newSharedLockedFixture(t, true, "")
	f.upstream.failFirstIDs[f.account.ID] = true
	f.upstream.onFirst = func() {
		for i := range f.group.ModelPricing {
			hot := 99e-6
			f.group.ModelPricing[i].InputPrice = &hot
			f.group.ModelPricing[i].OutputPrice = &hot
		}
		f.group.RateMultiplier = 99
	}
	rec := f.serveChat(t, sharedLockedChatBody(false))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 2, "primary must fail and the retry must hit the fallback account")
	require.Contains(t, f.upstream.callList()[1], fmt.Sprintf("%d:", f.fallback.ID))
	require.Len(t, f.ledger.commands, 1)
	// 冻结价：(1000*1e-6 + 500*2e-6) * 1 = 0.002；被篡改的 99 倍实时价不得生效。
	require.InDelta(t, 0.002, f.ledger.commands[0].BalanceCost, 1e-12)
	require.True(t, strings.HasPrefix(f.ledger.commands[0].RequestID, "locked:"))
}

// TestSharedChatCompletionsCompositeStaysLegacy 覆盖非 composite 限定：
// composite 分组不进入锁价，保持 legacy 行为（无价也不在入口 503）。
func TestSharedChatCompletionsCompositeStaysLegacy(t *testing.T) {
	f := newSharedLockedFixture(t, false, "")
	f.group.Platform = service.PlatformComposite
	rec := f.serveChat(t, sharedLockedChatBody(false))
	require.NotEqual(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String(),
		"composite groups keep the legacy path and must not be rejected by locked pricing")
}

// ---------- Responses（共享入口） ----------

// TestSharedResponsesLockedPricingMissingRejectedBeforeUpstream 覆盖 D-3：
// Responses 缺价（无价 + 缺任一维度）必须在选号/上游前 503 pricing_unavailable。
func TestSharedResponsesLockedPricingMissingRejectedBeforeUpstream(t *testing.T) {
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
			f := newSharedLockedFixture(t, tc.priced, tc.nilDim)
			rec := f.serveResponses(t, sharedLockedResponsesBody(false))
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Empty(t, f.upstream.callList())
			require.Empty(t, f.ledger.commands)
		})
	}
}

// TestSharedResponsesLockedPricingExplicitZerosAndFrozenFailover 覆盖 D-4：
// 显式四维零价放行；failover 换号后价格仍取自冻结 bundle。
func TestSharedResponsesLockedPricingExplicitZerosAndFrozenFailover(t *testing.T) {
	f := newSharedLockedFixture(t, true, "")
	f.upstream.failFirstIDs[f.account.ID] = true
	f.upstream.onFirst = func() {
		for i := range f.group.ModelPricing {
			hot := 99e-6
			f.group.ModelPricing[i].InputPrice = &hot
			f.group.ModelPricing[i].OutputPrice = &hot
		}
	}
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 2)
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.002, f.ledger.commands[0].BalanceCost, 1e-12)
}

// TestSharedLockedBillingBindUnknownAccountFailsClosed 覆盖"绑定失败不 Forward"：
// 冻结 bundle 的账号映射表里没有该账号时，绑定必须 fail-closed 返回
// ErrModelPricingUnavailable（调用方据此 503，不转发、不扣费）。
func TestSharedLockedBillingBindUnknownAccountFailsClosed(t *testing.T) {
	f := newSharedLockedFixture(t, true, "")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(sharedLockedChatBody(false))
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	locked, ok := prepareSharedGatewayLockedBilling(c, f.handler.gatewayService, f.key, "client-model", body, nil, true, false)
	require.True(t, ok)
	require.True(t, locked.active)
	bound, bindErr := bindSharedGatewayLockedBillingAccount(c, locked, *f.key.GroupID, 999999, "client-model", nil)
	require.False(t, bound)
	require.ErrorIs(t, bindErr, service.ErrModelPricingUnavailable)
	require.Empty(t, f.upstream.callList())
	require.Empty(t, f.ledger.commands)
}

// TestSharedResponsesCompositeStaysLegacy 覆盖非 composite 限定。
func TestSharedResponsesCompositeStaysLegacy(t *testing.T) {
	f := newSharedLockedFixture(t, false, "")
	f.group.Platform = service.PlatformComposite
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.NotEqual(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
}
