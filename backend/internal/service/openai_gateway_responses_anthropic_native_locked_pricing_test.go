//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nativeLockedBillingRepo struct {
	UsageBillingRepository
	mu       sync.Mutex
	commands []*UsageBillingCommand
	balance  float64
}

func (r *nativeLockedBillingRepo) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmd.Normalize()
	r.commands = append(r.commands, cmd)
	r.balance -= cmd.BalanceCost
	return &UsageBillingApplyResult{Applied: true, NewBalance: nativeLockedFloat64Ptr(r.balance)}, nil
}
func (r *nativeLockedBillingRepo) ReserveBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) { return &BatchImageBalanceHoldResult{}, nil }
func (r *nativeLockedBillingRepo) CaptureBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) { return &BatchImageBalanceHoldResult{}, nil }
func (r *nativeLockedBillingRepo) ReleaseBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) { return &BatchImageBalanceHoldResult{}, nil }

type nativeLockedLogs struct {
	UsageLogRepository
	mu   sync.Mutex
	rows []*UsageLog
}
func (r *nativeLockedLogs) Create(_ context.Context, row *UsageLog) (bool, error) { r.mu.Lock(); defer r.mu.Unlock(); r.rows = append(r.rows, row); return true, nil }

func nativeLockedFloat64Ptr(v float64) *float64 { return &v }
func nativeLockedInt64Ptr(v int64) *int64 { return &v }


func newNativeAnthropicLockedContextFor(t *testing.T, pricing *BillingService, group *Group, account *Account, key *APIKey, requested, frozen string) context.Context {
	t.Helper()
	deps := inflightEstimateDeps{
		billing:  pricing,
		resolver: NewModelPricingResolver(nil, pricing),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{account.ID: {Model: frozen}}, nil
		},
	}
	ctx, _, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: requested})
	require.NoError(t, err)
	bound, err := WithGatewayRequestBillingAccount(ctx, group.ID, account.ID)
	require.NoError(t, err)
	return bound
}

func newNativeAnthropicLockedRegularContext(t *testing.T, pricing *BillingService, group *Group, account *Account, key *APIKey) context.Context {
	return newNativeAnthropicLockedContextFor(t, pricing, group, account, key, "client-model", "vendor/kimi-moonlight")
}

func newNativeAnthropicLockedCNYContext(t *testing.T, pricing *BillingService, group *Group, account *Account, key *APIKey) context.Context {
	return newNativeAnthropicLockedContextFor(t, pricing, group, account, key, "glm-test", "glm-test")
}


func withNativeLockedRequestContext(parent context.Context, body string) *gin.Context {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(body))
	c.Request = c.Request.WithContext(parent)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func nativeAnthropicLockedResponse(stream bool, model string) *http.Response {
	// The Responses→Anthropic path always forces upstream streaming even for a
	// buffered client response, so both branches carry Anthropic SSE.
	_ = stream
	return &http.Response{StatusCode: 200,
		Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"uid-native"}},
		Body:   io.NopCloser(strings.NewReader(strings.ReplaceAll(miniAnthropicSSEStream(), "glm-4.7", model)))}
}

func forwardNativeAnthropicLocked(t *testing.T, svc *OpenAIGatewayService, account *Account, stream bool) (*OpenAIForwardResult, *httpUpstreamRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := "{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":" + map[bool]string{true: "true", false: "false"}[stream] + "}"
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicLockedResponse(stream, "vendor/kimi-moonlight")}
	svc.httpUpstream = upstream
	result, err := svc.forwardResponsesViaNativeAnthropic(c.Request.Context(), c, account, []byte(body), "")
	require.NoError(t, err)
	require.NotNil(t, result)
	return result, upstream
}

func lockedRecordInput(result *OpenAIForwardResult, key *APIKey, account *Account) *OpenAIRecordUsageInput {
	return &OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, InboundEndpoint: "/v1/responses", UpstreamEndpoint: "/v1/messages"}
}


// recon §3.4-1: 冻结 bundle + anthropic 协议账号。出站模型必须等于冻结上游模型，
// 计费按冻结价，且同一请求只扣一次。
func TestNativeAnthropicLockedFrozenBundleBillsOnce(t *testing.T) {
	svc, ledger, logs, group, account, key := newNativeAnthropicLockedFixture(t, false)
	ctx := newNativeAnthropicLockedRegularContext(t, svc.billingService, group, account, key)
	body := "{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":false}"
	c := withNativeLockedRequestContext(ctx, body)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicLockedResponse(false, "vendor/kimi-moonlight")}
	svc.httpUpstream = upstream
	result, err := svc.forwardResponsesViaNativeAnthropic(ctx, c, account, []byte(body), "")
	require.NoError(t, err)
	require.Equal(t, "vendor/kimi-moonlight", result.BillingModel)
	require.Equal(t, "vendor/kimi-moonlight", result.UpstreamModel)
	require.Equal(t, "vendor/kimi-moonlight", strings.Trim(gjson.GetBytes(upstream.lastBody, "model").String(), "\""))
	require.NoError(t, svc.RecordUsage(ctx, lockedRecordInput(result, key, account)))
	require.Len(t, ledger.commands, 1)
	require.InDelta(t, 0.00002, ledger.commands[0].BalanceCost, 1e-12)
	require.InDelta(t, 100-0.00002, ledger.balance, 1e-12)
	require.Len(t, logs.rows, 1)
	request := GatewayRequestBillingFromContext(ctx)
	require.NotNil(t, request)
	require.Equal(t, request.RequestID(), ledger.commands[0].RequestID)
}


// recon §3.4-2: 冻结 bundle + native CNY 卡。四维检查豁免，按原生卡计价。
func TestNativeAnthropicLockedNativeCNYCardBilled(t *testing.T) {
	svc, ledger, logs, group, account, key := newNativeAnthropicLockedFixture(t, true)
	ctx := newNativeAnthropicLockedCNYContext(t, svc.billingService, group, account, key)
	body := "{\"model\":\"glm-test\",\"input\":\"hello\",\"stream\":false}"
	c := withNativeLockedRequestContext(ctx, body)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicLockedResponse(false, "glm-test")}
	svc.httpUpstream = upstream
	result, err := svc.forwardResponsesViaNativeAnthropic(ctx, c, account, []byte(body), "")
	require.NoError(t, err)
	require.Equal(t, "glm-test", result.BillingModel)
	require.Equal(t, "glm-test", result.UpstreamModel)
	require.NoError(t, svc.RecordUsage(ctx, lockedRecordInput(result, key, account)))
	require.Len(t, ledger.commands, 1)
	// mini SSE usage: 10 input / 5 output, native fixture for glm-test is CNY 2/3 per M.
	require.InDelta(t, (10*2+5*3)/1e6, ledger.commands[0].BalanceCost, 1e-12)
	require.Len(t, logs.rows, 1)
}

// recon §3.4-3: bundle 缺该账号时 WithGatewayRequestBillingAccount 必须 fail-closed，
// 不落零成本账。
func TestNativeAnthropicLockedMissingAccountMappingFailsClosed(t *testing.T) {
	svc, ledger, _, group, account, key := newNativeAnthropicLockedFixture(t, false)
	deps := inflightEstimateDeps{
		billing:  svc.billingService,
		resolver: NewModelPricingResolver(nil, svc.billingService),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{999: {Model: "other"}}, nil
		},
	}
	ctx, _, err := deps.prepareRequestBilling(context.Background(), key, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	_, err = WithGatewayRequestBillingAccount(ctx, group.ID, account.ID)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Empty(t, ledger.commands)
}


// recon §3.4-4: 流式/非流式两条结果构造的 BillingModel/UpstreamModel 与冻结值一致。
func TestNativeAnthropicLockedResultModelsMatchFrozenValue(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "buffered"
		if stream { name = "streaming" }
		t.Run(name, func(t *testing.T) {
			svc, ledger, logs, group, account, key := newNativeAnthropicLockedFixture(t, false)
			ctx := newNativeAnthropicLockedRegularContext(t, svc.billingService, group, account, key)
			body := "{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":" + map[bool]string{true: "true", false: "false"}[stream] + "}"
			c := withNativeLockedRequestContext(ctx, body)
			upstream := &httpUpstreamRecorder{resp: nativeAnthropicLockedResponse(stream, "vendor/kimi-moonlight")}
			svc.httpUpstream = upstream
			result, err := svc.forwardResponsesViaNativeAnthropic(ctx, c, account, []byte(body), "")
			require.NoError(t, err)
			require.Equal(t, stream, result.Stream)
			require.Equal(t, "vendor/kimi-moonlight", result.BillingModel)
			require.Equal(t, "vendor/kimi-moonlight", result.UpstreamModel)
			scope, err := GatewayRequestBillingFromContext(ctx).ScopeFor(71, result.UpstreamModel, "")
			require.NoError(t, err)
			require.Equal(t, "client-model", scope.BillingModel, "locked customer scope stays on the frozen channel-mapped baseline; upstream model is execution fact")
			require.NoError(t, svc.RecordUsage(ctx, lockedRecordInput(result, key, account)))
			require.Len(t, ledger.commands, 1)
			require.Len(t, logs.rows, 1)
		})
	}
}

// recon §3.4-5: 未冻结时 native-anthropic 仍按既有模型解析，不进 locked 计费分支。
func TestNativeAnthropicUnfrozenLegacyBehaviorUnchanged(t *testing.T) {
	svc, ledger, _, _, account, key := newNativeAnthropicLockedFixture(t, false)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := "{\"model\":\"client-model\",\"input\":\"hello\",\"stream\":false}"
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicLockedResponse(false, "gpt-5.4")}
	svc.httpUpstream = upstream
	result, err := svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, []byte(body), "")
	require.NoError(t, err)
	// No frozen bundle: native Responses still sets its parsed upstream model,
	// while the model mapping helper normalizes the actual CN upstream name.
	require.NotEqual(t, "vendor/kimi-moonlight", result.BillingModel)
	require.Equal(t, result.UpstreamModel, result.BillingModel)
	// Legacy RecordUsage must succeed without a locked request bundle.
	err = svc.RecordUsage(context.Background(), lockedRecordInput(result, key, account))
	require.NoError(t, err)
	require.Len(t, ledger.commands, 1)
}

func newNativeAnthropicLockedFixture(t *testing.T, nativeCNY bool) (*OpenAIGatewayService, *nativeLockedBillingRepo, *nativeLockedLogs, *Group, *Account, *APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	group := &Group{ID: 71, Platform: PlatformKimi, RateMultiplier: 1}
	pricing := newTestBillingService()
	if nativeCNY {
		pricing = newNativeBillingTest(t)
	} else {
		in, out, zero := 1e-6, 2e-6, 0.0
		group.ModelPricing = []ChannelModelPricing{{Models: []string{"client-model"}, InputPrice: &in, OutputPrice: &out, CacheReadPrice: &zero, CacheWritePrice: &zero}}
	}
	account := &Account{
		ID: 81, Name: "kimi-native", Platform: PlatformKimi, Type: AccountTypeAPIKey,
		Concurrency: 1, Status: StatusActive,
		Credentials: map[string]any{
			"api_key":       "sk-native",
			"api_protocol":  APIProtocolAnthropic,
			"api_base_urls": map[string]any{APIProtocolAnthropic: "https://anthropic.example"},
		},
	}
	key := &APIKey{ID: 91, UserID: 92, GroupID: nativeLockedInt64Ptr(71), Group: group, User: &User{ID: 92, Status: StatusActive}}
	ledger := &nativeLockedBillingRepo{balance: 100}
	logs := &nativeLockedLogs{}
	cfg := &config.Config{RunMode: config.RunModeStandard, Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true}}}
	deps := inflightEstimateDeps{
		billing:  pricing,
		resolver: NewModelPricingResolver(nil, pricing),
		accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
			return map[int64]requestAccountMapping{account.ID: {Model: "glm-test"}}, nil
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg, billingService: pricing, resolver: deps.resolver, usageBillingRepo: ledger, usageLogRepo: logs, deferredService: &DeferredService{}}
	return svc, ledger, logs, group, account, key
}
