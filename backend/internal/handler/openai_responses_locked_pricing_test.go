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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type lockedHTTPFunds struct {
	service.BillingCache
	mu      sync.Mutex
	balance float64
}

func (f *lockedHTTPFunds) GetUserBalance(context.Context, int64) (float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.balance, nil
}
func (f *lockedHTTPFunds) SetUserBalance(_ context.Context, _ int64, b float64) error {
	f.mu.Lock()
	f.balance = b
	f.mu.Unlock()
	return nil
}
func (f *lockedHTTPFunds) DeductUserBalance(_ context.Context, _ int64, c float64) error {
	f.mu.Lock()
	f.balance -= c
	f.mu.Unlock()
	return nil
}
func (f *lockedHTTPFunds) GetUserPlatformQuotaCache(context.Context, int64, string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	return &service.UserPlatformQuotaCacheEntry{}, true, nil
}
func (f *lockedHTTPFunds) IncrUserPlatformQuotaUsageCache(context.Context, int64, string, float64, time.Duration, bool) error {
	return nil
}

type lockedHTTPLedger struct {
	service.UsageBillingRepository // Unused batch methods intentionally remain nil; text tests must not silently succeed on them.
	mu                             sync.Mutex
	balance                        float64
	commands                       []*service.UsageBillingCommand
	seen                           map[string]bool
}

func (l *lockedHTTPLedger) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[cmd.RequestID] {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}
	l.seen[cmd.RequestID] = true
	copy := *cmd
	l.commands = append(l.commands, &copy)
	l.balance -= cmd.BalanceCost
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

type lockedHTTPLogs struct {
	service.UsageLogRepository
	mu   sync.Mutex
	rows []*service.UsageLog
}

func (l *lockedHTTPLogs) Create(_ context.Context, row *service.UsageLog) (bool, error) {
	l.mu.Lock()
	l.rows = append(l.rows, row)
	l.mu.Unlock()
	return true, nil
}

type lockedHTTPRate struct {
	service.UserGroupRateRepository
	mu    sync.Mutex
	rate  float64
	calls int
}

func (r *lockedHTTPRate) GetByUserAndGroup(context.Context, int64, int64) (*float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := r.rate
	return &v, nil
}
func (r *lockedHTTPRate) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, nil
}

type lockedHTTPCall struct {
	model, id, version string
	accountID          int64
	startedAt          time.Time
	header             http.Header
}
type lockedHTTPUpstream struct {
	service.HTTPUpstream
	calls     []lockedHTTPCall
	failFirst bool
	onFirst   func()
	// usageOverride 调试字段：非空时上游非流式 usage 采用该 JSON（测试图输入等维度）。
	usageOverride string
}

func (u *lockedHTTPUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	bundle := service.GatewayRequestBillingFromContext(req.Context())
	if bundle == nil {
		return nil, fmt.Errorf("actual upstream lost request billing bundle")
	}
	groupID := int64(0)
	if requested := bundle.RequestedGroupID(); requested != nil {
		groupID = *requested
	}
	upstream, err := bundle.UpstreamModelFor(groupID, accountID)
	if err != nil {
		return nil, err
	}
	scope, err := bundle.ScopeFor(groupID, upstream, "")
	if err != nil {
		return nil, err
	}
	quote, err := bundle.PricingFor(scope)
	if err != nil {
		return nil, err
	}
	model := gjson.GetBytes(body, "model").String()
	u.calls = append(u.calls, lockedHTTPCall{model: model, id: bundle.RequestID(), version: quote.Version(), accountID: accountID, startedAt: bundle.StartedAt(), header: req.Header.Clone()})
	first := len(u.calls) == 1
	if first && u.onFirst != nil {
		u.onFirst()
	}
	if first && u.failFirst {
		return &http.Response{StatusCode: 502, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporary failure"}}`))}, nil
	}
	if gjson.GetBytes(body, "stream").Bool() && !strings.HasSuffix(req.URL.Path, "chat/completions") {
		created := fmt.Sprintf(`{"type":"response.created","response":{"id":"resp_fixture","model":%q,"status":"in_progress"}}`, model)
		completed := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_fixture","model":%q,"status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":500,"input_tokens_details":{"cached_tokens":0}}}}`, model)
		sse := "event: response.created\ndata: " + created + "\n\nevent: response.completed\ndata: " + completed + "\n\ndata: [DONE]\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{fmt.Sprintf("up-%d", len(u.calls))}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
	}
	var response string
	if strings.HasSuffix(req.URL.Path, "chat/completions") {
		response = fmt.Sprintf(`{"id":"chatcmpl-test","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`, model)
	} else if u.usageOverride != "" {
		response = fmt.Sprintf(`{"id":"resp_fixture","object":"response","status":"completed","model":%q,"output":[],"usage":%s}`, model, u.usageOverride)
	} else {
		response = fmt.Sprintf(`{"id":"resp_fixture","object":"response","status":"completed","model":%q,"output":[],"usage":{"input_tokens":1000,"output_tokens":500,"input_tokens_details":{"cached_tokens":0}}}`, model)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{fmt.Sprintf("up-%d", len(u.calls))}}, Body: io.NopCloser(strings.NewReader(response))}, nil
}

type lockedHTTPFixture struct {
	handler     *OpenAIGatewayHandler
	cfg         *config.Config
	key         *service.APIKey
	group       *service.Group
	accounts    []*service.Account
	upstream    *lockedHTTPUpstream
	ledger      *lockedHTTPLedger
	rates       *lockedHTTPRate
	logs        *lockedHTTPLogs
	channelRepo *openAIWSUsageHandlerChannelRepoStub
	channels    *service.ChannelService
}

func newLockedHTTPFixture(t *testing.T, zero bool) *lockedHTTPFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	input, output, cacheZero := 1e-6, 2e-6, 0.0
	if zero {
		input, output = 0, 0
	}
	group := &service.Group{ID: 71, Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 1, ModelPricing: []service.ChannelModelPricing{{Models: []string{"client-model", "gpt-5.4"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &cacheZero, CacheWritePrice: &cacheZero}}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Gateway.MaxAccountSwitches = 1
	accounts := []*service.Account{}
	for i := 0; i < 2; i++ {
		accounts = append(accounts, &service.Account{ID: int64(81 + i), Name: fmt.Sprintf("fixture-%d", i), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Priority: i + 1, Concurrency: 1, AccountGroups: []service.AccountGroup{{AccountID: int64(81 + i), GroupID: 71}}, Credentials: map[string]any{"api_key": "fixture-not-a-secret", "base_url": "https://fixture.invalid", "model_mapping": map[string]any{"client-model": "gpt-5.4"}}, Extra: map[string]any{"openai_passthrough": true}})
	}
	scheduler := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: accounts}, nil, nil, nil, cfg)
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{*accounts[0], *accounts[1]}}
	funds := &lockedHTTPFunds{balance: 100}
	cache := service.NewBillingCacheService(funds, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	rates := &lockedHTTPRate{rate: 1.2}
	up := &lockedHTTPUpstream{}
	channelRepo := &openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{71: service.PlatformOpenAI}}
	channels := service.NewChannelService(channelRepo, nil, nil, nil, nil)
	billing := service.NewBillingService(cfg, nil)
	svc := service.NewOpenAIGatewayService(repo, logs, ledger, nil, nil, rates, nil, cfg, scheduler, service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, cache, up, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channels, billing), channels, nil, nil, nil)
	handler := NewOpenAIGatewayHandler(svc, service.NewConcurrencyService(&fakeConcurrencyCache{}), cache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	key := &service.APIKey{ID: 91, UserID: 92, GroupID: lockedHTTPInt64(71), Group: group, User: &service.User{ID: 92, Status: service.StatusActive, Balance: 100}}
	return &lockedHTTPFixture{handler: handler, cfg: cfg, key: key, group: group, accounts: accounts, upstream: up, ledger: ledger, rates: rates, logs: logs, channelRepo: channelRepo, channels: channels}
}
func lockedHTTPInt64(v int64) *int64 { return &v }
func (f *lockedHTTPFixture) request(t *testing.T, model string, subscription *service.UserSubscription) (*httptest.ResponseRecorder, *gin.Context) {
	return f.requestBody(t, fmt.Sprintf(`{"model":%q,"input":"hello","stream":false}`, model), subscription)
}

func (f *lockedHTTPFixture) requestBody(t *testing.T, body string, subscription *service.UserSubscription) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Client-Request-ID", "fixture-client")
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 92, Concurrency: 1})
	if subscription != nil {
		c.Set(string(middleware.ContextKeySubscription), subscription)
	}
	f.handler.Responses(c)
	return rec, c
}

func TestOpenAIResponsesLockedPricingNativeBodyCannotOptOut(t *testing.T) {
	for _, kind := range []string{"unknown", "missing_input", "complete"} {
		t.Run(kind, func(t *testing.T) {
			f := newLockedHTTPFixture(t, false)
			model := "client-model"
			if kind == "unknown" {
				model = "unknown-unpriced-model"
			}
			if kind == "missing_input" {
				f.group.ModelPricing[0].InputPrice = nil
			}
			body := fmt.Sprintf(`{"model":%q,"input":[{"role":"user","content":"hello"},{"type":"compaction_trigger"}],"stream":true}`, model)
			require.True(t, isOpenAIRemoteCompactionV2Request([]byte(body)))
			rec, c := f.requestBody(t, body, nil)
			require.True(t, isBareOpenAIResponsesPath(c))
			require.Empty(t, c.Request.Header.Get("x-codex-beta-features"), "client did not opt into a bypass using headers")
			if kind != "complete" {
				require.Equal(t, 503, rec.Code, rec.Body.String())
				require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
				require.Empty(t, f.upstream.calls)
				require.Empty(t, f.ledger.commands)
				return
			}
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "response.completed")
			require.Len(t, f.upstream.calls, 1)
			require.Contains(t, f.upstream.calls[0].header.Get("x-codex-beta-features"), "remote_compaction_v2")
			require.Equal(t, "client-model", f.upstream.calls[0].model)
			require.Len(t, f.ledger.commands, 1)
			require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
			request := service.GatewayRequestBillingFromContext(c.Request.Context())
			require.NotNil(t, request)
			require.Equal(t, request.RequestID(), f.upstream.calls[0].id)
			require.Equal(t, request.StartedAt(), f.upstream.calls[0].startedAt)
			require.Equal(t, request.RequestID(), f.ledger.commands[0].RequestID)
		})
	}
}

func TestOpenAIResponsesLockedPricingImageInputTokensBilled(t *testing.T) {
	f := newLockedHTTPFixture(t, false)
	f.upstream.usageOverride = `{"input_tokens":1000,"output_tokens":500,"input_tokens_details":{"image_tokens":200,"cached_tokens":0}}`
	body := `{"model":"client-model","input":[{"role":"user","content":[{"type":"input_text","text":"hi"},{"type":"input_image","image_url":"data:image/png;base64,xxx"}]}],"stream":false}`
	rec, _ := f.requestBody(t, body, nil)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.calls, 1)
	require.Len(t, f.ledger.commands, 1)
	// 图片输入价未配置时 computeTokenCost 回退文本 input 价：200 图 token 同按 1e-6 计，
	// 即 (1000*1e-6 + 500*2e-6) * 1.2 = 0.0024；配置图片输入价后才会图文分价。
	require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12, "image input tokens billed at text fallback price")
}
func TestOpenAIResponsesLockedPricingRejectsMissingBeforeUpstream(t *testing.T) {
	for _, kind := range []string{"unknown", "input", "output", "cache_read", "cache_write", "simple", "subscription", "no_group"} {
		t.Run(kind, func(t *testing.T) {
			f := newLockedHTTPFixture(t, false)
			model := "client-model"
			var sub *service.UserSubscription
			switch kind {
			case "unknown":
				model = "unknown-unpriced-model"
			case "input":
				f.group.ModelPricing[0].InputPrice = nil
			case "output":
				f.group.ModelPricing[0].OutputPrice = nil
			case "cache_read":
				f.group.ModelPricing[0].CacheReadPrice = nil
			case "cache_write":
				f.group.ModelPricing[0].CacheWritePrice = nil
			case "simple":
				f.cfg.RunMode = config.RunModeSimple
				model = "unknown-unpriced-model"
			case "subscription":
				sub = &service.UserSubscription{}
				model = "unknown-unpriced-model"
			case "no_group":
				f.key.GroupID = nil
				f.key.Group = nil
				model = "unknown-unpriced-model"
			}
			rec, c := f.request(t, model, sub)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
			require.Empty(t, f.upstream.calls)
			_, selected := c.Get(opsAccountIDKey)
			require.False(t, selected)
			require.Empty(t, f.ledger.commands)
		})
	}
}
func TestOpenAIResponsesLockedPricingActualNormalAndRawBody(t *testing.T) {
	for _, raw := range []bool{false, true} {
		name := "normal"
		if raw {
			name = "raw_chat"
		}
		t.Run(name, func(t *testing.T) {
			f := newLockedHTTPFixture(t, false)
			for _, account := range f.accounts {
				account.Extra["openai_passthrough"] = false
				if raw {
					account.Extra["openai_responses_supported"] = false
				}
			}
			rec, _ := f.request(t, "client-model", nil)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, f.upstream.calls, 1)
			require.Equal(t, "gpt-5.4", f.upstream.calls[0].model)
			require.Len(t, f.ledger.commands, 1)
			require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
		})
	}
}

func TestOpenAIResponsesLockedPricingCompleteZerosReachUpstream(t *testing.T) {
	f := newLockedHTTPFixture(t, true)
	rec, _ := f.request(t, "client-model", nil)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.calls, 1)
	require.Equal(t, "client-model", f.upstream.calls[0].model, "passthrough must ignore the normal account mapping")
	require.Equal(t, "Bearer fixture-not-a-secret", f.upstream.calls[0].header.Get("Authorization"))
}
func TestOpenAIResponsesLockedPricingRetryHotMutationAndRealDebit(t *testing.T) {
	for _, passthrough := range []bool{true, false} {
		name := "normal"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			f := newLockedHTTPFixture(t, false)
			for _, account := range f.accounts {
				account.Extra["openai_passthrough"] = passthrough
				account.Credentials["model_mapping"] = map[string]any{"gpt-5.4": "gpt-5.3"}
			}
			f.channelRepo.channels = []service.Channel{{ID: 101, Status: service.StatusActive, GroupIDs: []int64{71}, ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"client-model": "gpt-5.4"}}}}
			f.channels.InvalidateCache()
			expectedModel := "gpt-5.3"
			if passthrough {
				expectedModel = "gpt-5.4"
			}
			f.upstream.failFirst = true
			f.upstream.onFirst = func() {
				*f.group.ModelPricing[0].InputPrice = 20e-6
				*f.group.ModelPricing[0].OutputPrice = 40e-6
				f.group.RateMultiplier = 99
				f.rates.mu.Lock()
				f.rates.rate = 99
				f.rates.mu.Unlock()
				for _, account := range f.accounts {
					account.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-5.5", "gpt-5.4": "gpt-5.5"}
				}
				f.channelRepo.mu.Lock()
				f.channelRepo.channels[0].ModelMapping[service.PlatformOpenAI]["client-model"] = "client-model"
				f.channelRepo.mu.Unlock()
				f.channels.InvalidateCache()
			}
			rec, c := f.request(t, "client-model", nil)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, f.upstream.calls, 2)
			require.NotEqual(t, f.upstream.calls[0].accountID, f.upstream.calls[1].accountID)
			require.Equal(t, f.upstream.calls[0].id, f.upstream.calls[1].id)
			require.Equal(t, f.upstream.calls[0].version, f.upstream.calls[1].version)
			require.Equal(t, f.upstream.calls[0].startedAt, f.upstream.calls[1].startedAt)
			require.Equal(t, expectedModel, f.upstream.calls[0].model)
			require.Equal(t, expectedModel, f.upstream.calls[1].model)
			require.Len(t, f.ledger.commands, 1)
			require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
			require.InDelta(t, 100-0.0024, f.ledger.balance, 1e-12)
			require.Equal(t, f.upstream.calls[0].id, f.ledger.commands[0].RequestID)
			detached := usageRecordContext(c.Request.Context(), context.Background())
			require.Same(t, service.GatewayRequestBillingFromContext(c.Request.Context()), service.GatewayRequestBillingFromContext(detached))
			rec, _ = f.request(t, "client-model", nil)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, f.upstream.calls, 3)
			require.NotEqual(t, f.upstream.calls[0].id, f.upstream.calls[2].id)
			require.NotEqual(t, f.upstream.calls[0].version, f.upstream.calls[2].version)
			require.Len(t, f.ledger.commands, 2)
			require.InDelta(t, 0.048, f.ledger.commands[1].BalanceCost, 1e-12, "new generation uses the new price; user override cache semantics remain unchanged")
		})
	}
}
