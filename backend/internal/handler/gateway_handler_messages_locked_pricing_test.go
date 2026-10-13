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

// messagesLockedUpstream 记录实际到达上游的调用（账号 ID + 出站模型），并可让指定
// 账号先返回一次可切换账号的上游错误，用来验证 failover 语义。
type messagesLockedUpstream struct {
	service.HTTPUpstream
	mu            sync.Mutex
	calls         []string
	failFirstIDs  map[int64]bool
	responseModel string
}

func (u *messagesLockedUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
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
		return &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"api_error","message":"temporary"}}`))}, nil
	}
	responseModel := model
	if u.responseModel != "" {
		responseModel = u.responseModel
	}
	usage := `{"input_tokens":1000,"output_tokens":500}`
	resp := fmt.Sprintf(`{"id":"msg_fixture","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":%s}`, responseModel, usage)
	if gjson.GetBytes(body, "stream").Bool() {
		start := fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"usage":{"input_tokens":1000,"output_tokens":0}}}`, responseModel)
		delta := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`
		stop := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":500}}`
		sse := "event: message_start\ndata: " + start + "\n\nevent: content_block_delta\ndata: " + delta + "\n\nevent: message_delta\ndata: " + stop + "\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"up-stream-1"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"up-1"}}, Body: io.NopCloser(strings.NewReader(resp))}, nil
}

func (u *messagesLockedUpstream) callList() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

type messagesLockedFixture struct {
	handler   *GatewayHandler
	group     *service.Group
	fallback  *service.Group
	account   *service.Account
	fallbackA *service.Account
	key       *service.APIKey
	upstream  *messagesLockedUpstream
	ledger    *lockedHTTPLedger
	logs      *lockedHTTPLogs
}

func messagesLockedInt64(v int64) *int64 { return &v }

// newMessagesLockedFixture 构造真实走 /v1/messages 普通分支的 handler：
// 一个 Anthropic 分组 + 一个 Anthropic APIKey 账号（账号级 model_mapping 把
// client-model 映射到 claude-sonnet-4，用于核对冻结账号模型与实际出站模型）。
func newMessagesLockedFixture(t *testing.T, withPrices bool, nilDimension string) *messagesLockedFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		primaryGroupID  = int64(7400)
		fallbackGroupID = int64(7401)
		primaryAccount  = int64(7410)
		fallbackAcctID  = int64(7411)
	)
	input, output, zero := 1e-6, 2e-6, 0.0
	primary := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1}
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
		primary.ModelPricing = []service.ChannelModelPricing{pricing}
	}
	fallback := &service.Group{ID: fallbackGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1,
		ModelPricing: []service.ChannelModelPricing{{Models: []string{"client-model"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}}}

	account := &service.Account{ID: primaryAccount, Name: "primary", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "claude-sonnet-4"}},
		AccountGroups: []service.AccountGroup{{AccountID: primaryAccount, GroupID: primaryGroupID}}}
	// 第二个账号同时属于主分组与 fallback 分组：主分组里的 failover 有替补可切，
	// 而 prompt-too-long 的 fallback 分组重冻也有账号可绑。
	fallbackAccount := &service.Account{ID: fallbackAcctID, Name: "fallback", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Priority: 2, Concurrency: 1,
		Credentials: map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "claude-sonnet-4"}},
		AccountGroups: []service.AccountGroup{
			{AccountID: fallbackAcctID, GroupID: primaryGroupID},
			{AccountID: fallbackAcctID, GroupID: fallbackGroupID},
		}}

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account, fallbackAccount}}}
	groupRepo := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{primaryGroupID: primary, fallbackGroupID: fallback}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 2
	billing := service.NewBillingService(cfg, nil)
	channelRepo := &openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{primaryGroupID: service.PlatformAnthropic, fallbackGroupID: service.PlatformAnthropic}}
	channels := service.NewChannelService(channelRepo, nil, nil, nil, nil)
	upstream := &messagesLockedUpstream{failFirstIDs: map[int64]bool{}}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	gatewayService := service.NewGatewayService(
		nil, groupRepo, logs, ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg),
		service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, upstream, nil,
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil,
	)
	// 用内存余额桩替代 DB 余额查询，避免 handler 的余额资格检查依赖仓储。
	billingCache := service.NewBillingCacheService(&lockedHTTPFunds{balance: 100}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	h := &GatewayHandler{
		gatewayService:      gatewayService,
		billingCacheService: billingCache,
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:  2,
		cfg:                 cfg,
	}
	key := &service.APIKey{ID: 7420, UserID: 7430, GroupID: messagesLockedInt64(primaryGroupID), Group: primary, Status: service.StatusActive,
		User: &service.User{ID: 7430, Status: service.StatusActive, Concurrency: 10, Balance: 100}}
	return &messagesLockedFixture{handler: h, group: primary, fallback: fallback, account: account, fallbackA: fallbackAccount, key: key, upstream: upstream, ledger: ledger, logs: logs}
}

func (f *messagesLockedFixture) serve(t *testing.T, body string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.0.0 (external, cli)")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	f.handler.Messages(c)
	return recorder, c
}

func messagesLockedBody(stream bool) string {
	return fmt.Sprintf(`{"model":"client-model","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"stream":%t}`, stream)
}

// TestMessagesLockedPricingMissingPriceReturns503StreamAndNonStream 锁定 D-1：缺价模型
// 必须在选号/上游请求前返回 503 pricing_unavailable（流式与非流式一致）。
func TestMessagesLockedPricingMissingPriceReturns503StreamAndNonStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			f := newMessagesLockedFixture(t, false, "")
			rec, c := f.serve(t, messagesLockedBody(stream))
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Empty(t, f.upstream.callList())
			require.Empty(t, f.ledger.commands)
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected, "must reject before account selection")
		})
	}
}

// TestMessagesLockedPricingMissingDimensionReturns503 锁定 D-2：四维价缺任一维都拒绝。
func TestMessagesLockedPricingMissingDimensionReturns503(t *testing.T) {
	for _, dimension := range []string{"input", "output", "cache_read", "cache_write"} {
		t.Run(dimension, func(t *testing.T) {
			f := newMessagesLockedFixture(t, true, dimension)
			rec, _ := f.serve(t, messagesLockedBody(false))
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Empty(t, f.upstream.callList())
		})
	}
}

func TestMessagesLockedPricingExplicitZeroPricesAllowed(t *testing.T) {
	f := newMessagesLockedFixture(t, true, "")
	zero := 0.0
	f.group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"client-model", "claude-sonnet-4"}, InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}}
	rec, _ := f.serve(t, messagesLockedBody(false))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 1)
	require.Len(t, f.ledger.commands, 1)
	require.Zero(t, f.ledger.commands[0].BalanceCost, "explicit zeros bill at zero, not 503")
}

// TestMessagesLockedPricingUsesFrozenPriceAfterFailover 锁定 D-4：failover 换号后仍用
// 请求开始冻结的价格，且成功计费只有一个幂等 requestID（locked: 前缀）。
func TestMessagesLockedPricingUsesFrozenPriceAfterFailover(t *testing.T) {
	f := newMessagesLockedFixture(t, true, "")
	f.upstream.failFirstIDs[f.account.ID] = true
	rec, _ := f.serve(t, messagesLockedBody(false))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.callList(), 2, "first attempt fails, second succeeds")
	require.Len(t, f.ledger.commands, 1, "only the successful attempt bills")
	require.InDelta(t, 1000*1e-6+500*2e-6, f.ledger.commands[0].BalanceCost, 1e-15)
	require.True(t, strings.HasPrefix(f.ledger.commands[0].RequestID, "locked:"), "requestID comes from the frozen bundle")
}

// TestMessagesLockedPricingBindingFailureRejectsBeforeForward 锁定 D-3：冻结表里没有
// 该账号（账号不在冻结映射集内）时绑定失败，返回 ErrModelPricingUnavailable，
// 调用方据此 503 且不会 Forward。
func TestMessagesLockedPricingBindingFailureRejectsBeforeForward(t *testing.T) {
	f := newMessagesLockedFixture(t, true, "")
	// 冻结时调度快照里只有 fallback 账号，因此冻结表只含 fallback 账号的模型。
	f.account.AccountGroups = nil
	priceRequest := tokenInflightEstimate("client-model", []byte(messagesLockedBody(false)))
	prepared, _, err := f.handler.gatewayService.PrepareRequestBilling(context.Background(), f.key, priceRequest)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("")).WithContext(prepared)
	locked, ok := prepareSharedGatewayLockedBilling(c, f.handler.gatewayService, f.key, "client-model", []byte(messagesLockedBody(false)), nil, true, false)
	require.True(t, ok)
	require.True(t, locked.active)

	bound, bindErr := bindSharedGatewayLockedBillingAccount(c, locked, derefGroupID(f.key.GroupID), f.fallbackA.ID+999, "client-model", nil)
	require.False(t, bound)
	require.ErrorIs(t, bindErr, service.ErrModelPricingUnavailable)
	status, code, _, _ := billingErrorDetails(bindErr)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "pricing_unavailable", code)
	require.Empty(t, f.upstream.callList())
	require.Empty(t, f.ledger.commands)
}

// TestMessagesLockedPricingNoSchedulableAccountStopsBeforeForward 锁定：无可用账号时
// 既不 Forward 也不扣费。
func TestMessagesLockedPricingNoSchedulableAccountStopsBeforeForward(t *testing.T) {
	f := newMessagesLockedFixture(t, true, "")
	f.account.AccountGroups = nil
	f.fallbackA.AccountGroups = nil
	rec, _ := f.serve(t, messagesLockedBody(false))
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.Empty(t, f.upstream.callList())
	require.Empty(t, f.ledger.commands)
}

// TestMessagesLockedPricingFallbackRefreezeUsesNewGroupBundle 锁定 D-5：fallback 分组
// 必须重新冻结，新 bundle 的候选分组是 fallback 分组；旧 bundle 无法绑定该分组账号
// （证明复用旧 bundle 会失败，所以必须重冻）。
func TestMessagesLockedPricingFallbackRefreezeUsesNewGroupBundle(t *testing.T) {
	f := newMessagesLockedFixture(t, true, "")
	body := []byte(messagesLockedBody(false))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("")).WithContext(context.Background())
	initial, ok := prepareSharedGatewayLockedBilling(c, f.handler.gatewayService, f.key, "client-model", body, nil, true, false)
	require.True(t, ok)
	require.True(t, initial.active)
	oldBundle := service.GatewayRequestBillingFromContext(c.Request.Context())
	require.NotNil(t, oldBundle)

	fallbackKey := cloneAPIKeyWithGroup(f.key, f.fallback)
	_, oldBindErr := service.WithGatewayRequestBillingAccount(c.Request.Context(), f.fallback.ID, f.fallbackA.ID)
	require.Error(t, oldBindErr, "old bundle must not cover the fallback group")

	refrozen, refreezeOK := refreezeSharedGatewayLockedBilling(c, f.handler.gatewayService, fallbackKey, "client-model", body, nil)
	require.True(t, refreezeOK)
	require.True(t, refrozen.active)
	require.Equal(t, f.fallback.ID, refrozen.groupID)
	newBundle := service.GatewayRequestBillingFromContext(c.Request.Context())
	require.NotNil(t, newBundle)
	require.NotSame(t, oldBundle, newBundle, "fallback must freeze a fresh bundle")
	require.NotEqual(t, oldBundle.RequestID(), newBundle.RequestID(), "fresh bundle carries a fresh requestID")

	if _, err := service.WithGatewayRequestBillingAccount(c.Request.Context(), f.fallback.ID, f.fallbackA.ID); err != nil {
		t.Fatalf("refrozen bundle must bind the fallback group account: %v", err)
	}
}

// TestMessagesWarmupInterceptStillBypassesLockedPricing 锁定：本机拦截路径不因缺价被拒。
func TestMessagesWarmupInterceptStillBypassesLockedPricing(t *testing.T) {
	f := newMessagesLockedFixture(t, false, "")
	f.account.Credentials["intercept_warmup_requests"] = true
	body := `{"model":"client-model","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`
	rec, _ := f.serve(t, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, f.upstream.callList())
	require.Empty(t, f.ledger.commands)
}

// TestMessagesLockedPricingFrozenFlagOnRequestContext 锁定：通过锁价准入门后，
// 请求 ctx 携带冻结 bundle，后续 worker 转移不会丢失。
func TestMessagesLockedPricingFrozenFlagOnRequestContext(t *testing.T) {
	f := newMessagesLockedFixture(t, true, "")
	rec, c := f.serve(t, messagesLockedBody(false))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, service.GatewayRequestBillingFromContext(c.Request.Context()), "frozen bundle must remain on the request context")
}
