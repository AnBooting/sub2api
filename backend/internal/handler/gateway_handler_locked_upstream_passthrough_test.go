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

// passthroughModel 是透传账号的请求/出站模型：在 Antigravity 默认映射中映射到自身，
// 因此它既能通过调度准入，又能保证"出站模型 == 请求模型"。
const passthroughModel = "claude-sonnet-4-5"

// messagesUpstreamPassthroughUpstream 记录 Antigravity AccountTypeUpstream 透传账号
// 的实际上游请求体模型（该路径是纯透传：只换认证头，不改模型）。
type messagesUpstreamPassthroughUpstream struct {
	service.HTTPUpstream
	mu     sync.Mutex
	models []string
	calls  int
	stream bool
}

func (u *messagesUpstreamPassthroughUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	model := gjson.GetBytes(body, "model").String()
	u.mu.Lock()
	u.models = append(u.models, model)
	u.calls++
	stream := u.stream
	u.mu.Unlock()
	if stream {
		// Claude SSE：透传路径原样转写回客户端，usage 在 message_delta 里给出。
		start := fmt.Sprintf(`{"type":"message_start","message":{"id":"msg_fixture","type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"usage":{"input_tokens":1000,"output_tokens":0}}}`, model)
		delta := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":500}}`
		stop := `{"type":"message_stop"}`
		sse := "event: message_start\ndata: " + start + "\n\nevent: message_delta\ndata: " + delta + "\n\nevent: message_stop\ndata: " + stop + "\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"up-passthrough-s1"}},
			Body: io.NopCloser(strings.NewReader(sse))}, nil
	}
	resp := fmt.Sprintf(`{"id":"msg_fixture","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":500}}`, model)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"up-passthrough-1"}},
		Body: io.NopCloser(strings.NewReader(resp))}, nil
}

func (u *messagesUpstreamPassthroughUpstream) snapshot() ([]string, int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.models...), u.calls
}

type messagesUpstreamPassthroughFixture struct {
	handler  *GatewayHandler
	group    *service.Group
	account  *service.Account
	key      *service.APIKey
	upstream *messagesUpstreamPassthroughUpstream
	ledger   *lockedHTTPLedger
	logs     *lockedHTTPLogs
}

// newMessagesUpstreamPassthroughFixture 构造 Antigravity + AccountTypeUpstream 透传账号
// 的 /v1/messages 场景（走 ForwardUpstream 纯透传分支）。
// withPrices=false 时分组不带任何价格，用于缺价 case。
func newMessagesUpstreamPassthroughFixture(t *testing.T, withPrices bool) *messagesUpstreamPassthroughFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		groupID   = int64(7900)
		accountID = int64(7910)
	)
	// claude-sonnet-4-5 在 Antigravity 默认映射里映射到自身，因此透传账号的调度准入
	// 与"出站模型 == 请求模型"可同时成立。
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1}
	if withPrices {
		group.ModelPricing = []service.ChannelModelPricing{{Models: []string{passthroughModel}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero}}
	}
	// 透传账号：base_url + api_key，平台 Antigravity、类型 upstream。
	// 注意它没有 model_mapping：出站模型就是请求模型本身。
	account := &service.Account{ID: accountID, Name: "ag-upstream-passthrough", Platform: service.PlatformAntigravity, Type: service.AccountTypeUpstream,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials:   map[string]any{"base_url": "https://fixture.invalid", "api_key": "fixture-not-a-secret"},
		Extra:         map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}}}

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account}}}
	groupRepo := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{groupID: group}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{groupID: service.PlatformAnthropic}}, nil, nil, nil, nil)
	upstream := &messagesUpstreamPassthroughUpstream{}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	gatewayService := service.NewGatewayService(
		nil, groupRepo, logs, ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg),
		service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, upstream, nil,
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil,
	)
	tokenProvider := service.NewAntigravityTokenProvider(nil, nil, nil)
	// 流式透传路径读 settingService 的 MaxLineSize；缺了它会 panic（生产装配恒有）。
	antigravitySettings := service.NewSettingService(&lockedRegressionSettingRepo{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1 << 20}})
	antigravity := service.NewAntigravityGatewayService(nil, nil, nil, tokenProvider, nil, upstream, antigravitySettings, nil)
	billingCache := service.NewBillingCacheService(&lockedHTTPFunds{balance: 100}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	h := &GatewayHandler{
		gatewayService:            gatewayService,
		antigravityGatewayService: antigravity,
		billingCacheService:       billingCache,
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:        1,
		cfg:                       cfg,
	}
	key := &service.APIKey{ID: 7920, UserID: 7930, GroupID: messagesLockedInt64(groupID), Group: group, Status: service.StatusActive,
		User: &service.User{ID: 7930, Status: service.StatusActive, Concurrency: 10, Balance: 100}}
	return &messagesUpstreamPassthroughFixture{handler: h, group: group, account: account, key: key, upstream: upstream, ledger: ledger, logs: logs}
}

func (f *messagesUpstreamPassthroughFixture) serve(t *testing.T, body string) (*httptest.ResponseRecorder, *gin.Context) {
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

// TestMessagesLockedPricingAntigravityUpstreamTypeAccountBills 锁定 F5 回归：
// Antigravity + AccountTypeUpstream 透传账号此前 ForwardResult 不设 UpstreamModel，
// 消费侧等值核对拿到空串 -> HTTP 200 但零计费。
// 修复后：出站模型与冻结值一致，落账 1 条且金额 > 0。
func TestMessagesLockedPricingAntigravityUpstreamTypeAccountBills(t *testing.T) {
	f := newMessagesUpstreamPassthroughFixture(t, true)
	rec, c := f.serve(t, passthroughBody(false))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	models, calls := f.upstream.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, []string{passthroughModel}, models, "透传路径出站模型即请求模型，不做映射改写")

	bundle := service.GatewayRequestBillingFromContext(c.Request.Context())
	require.NotNil(t, bundle)
	frozen, err := bundle.UpstreamModelFor(f.group.ID, f.account.ID)
	require.NoError(t, err)
	require.Equal(t, models[0], frozen, "冻结账号模型必须等于实际上游出站模型")

	require.Len(t, f.ledger.commands, 1, "透传账号必须落账 1 条，不能 200 零计费")
	require.InDelta(t, 1000*1e-6+500*2e-6, f.ledger.commands[0].BalanceCost, 1e-15)
}

// TestMessagesLockedPricingAntigravityUpstreamTypeAccountMissingPriceFailsClosed 锁定
// F5 另一半：同形态 + 缺价时必须在发上游前 503，upstreamCalls=0。
func TestMessagesLockedPricingAntigravityUpstreamTypeAccountMissingPriceFailsClosed(t *testing.T) {
	f := newMessagesUpstreamPassthroughFixture(t, false)
	rec, _ := f.serve(t, passthroughBody(false))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	_, calls := f.upstream.snapshot()
	require.Zero(t, calls, "缺价绝不能放行到上游")
	require.Empty(t, f.ledger.commands)
}

// TestMessagesLockedPricingAntigravityUpstreamTypeAccountStreamingBills 锁定 F5 流式
// 分支：ForwardUpstream 的 streamUpstreamResponse 只回填 usage，UpstreamModel 由同一个
// 成功分支统一设置，因此流式也必须能正常落账。
func TestMessagesLockedPricingAntigravityUpstreamTypeAccountStreamingBills(t *testing.T) {
	f := newMessagesUpstreamPassthroughFixture(t, true)
	f.upstream.stream = true
	rec, _ := f.serve(t, passthroughBody(true))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	models, calls := f.upstream.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, []string{passthroughModel}, models)
	require.Len(t, f.ledger.commands, 1, "流式透传账号同样必须落账")
	require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
}
func passthroughBody(stream bool) string {
	return fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}],"stream":%t}`, stream)
}
