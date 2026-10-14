//go:build unit

package handler

import (
	"context"
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

// F6/F7 回归：Antigravity 账号按请求体 thinking / reasoning 档位选上游模型变体时，
// 冻结值必须与实际上游出站模型逐字一致。修复前 thinkingLevel=low（Gemini 原生）或
// reasoning.effort=medium（Chat Completions / Responses）会让冻结值停在默认 high，
// 消费侧等值核对失败 → 用户拿到 200 但整笔零计费。
type antigravityThinkingFixture struct {
	handler  *GatewayHandler
	group    *service.Group
	account  *service.Account
	key      *service.APIKey
	upstream *messagesAntigravityUpstream
	ledger   *lockedHTTPLedger
}

func newAntigravityThinkingFixture(t *testing.T, groupPlatform string) *antigravityThinkingFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const (
		groupID   = int64(7960)
		accountID = int64(7961)
	)
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &service.Group{ID: groupID, Hydrated: true, Platform: groupPlatform, Status: service.StatusActive, RateMultiplier: 1,
		ModelPricing: []service.ChannelModelPricing{
			{Models: []string{"client-model", "gemini-3.8-flash", "gemini-3.8-flash-low", "gemini-3.8-flash-medium", "gemini-3.8-flash-high"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero},
		}}
	account := &service.Account{ID: accountID, Name: "ag-thinking", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "fixture-not-a-secret", "project_id": "fixture-project",
			"model_mapping": map[string]any{
				"client-model":            "gemini-3.8-flash",
				"gemini-3.8-flash-low":    "gemini-3.8-flash-low",
				"gemini-3.8-flash-medium": "gemini-3.8-flash-medium",
				"gemini-3.8-flash-high":   "gemini-3.8-flash-high",
			},
		},
		Extra:         map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}}}

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account}}}
	groupRepo := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{groupID: group}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{groupID: service.PlatformAnthropic}}, nil, nil, nil, nil)
	upstream := &messagesAntigravityUpstream{}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	gatewayService := service.NewGatewayService(
		nil, groupRepo, logs, ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg),
		service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, upstream, nil,
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil,
	)
	tokenProvider := service.NewAntigravityTokenProvider(nil, nil, nil)
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
	key := &service.APIKey{ID: 7962, UserID: 7963, GroupID: lockedSharedInt64(groupID), Group: group, Status: service.StatusActive,
		User: &service.User{ID: 7963, Status: service.StatusActive, Concurrency: 10, Balance: 100}}
	return &antigravityThinkingFixture{handler: h, group: group, account: account, key: key, upstream: upstream, ledger: ledger}
}

func (f *antigravityThinkingFixture) serve(t *testing.T, path, body string, call func(*GatewayHandler, *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	f.upstream.models = nil
	f.ledger.commands = nil
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

func (f *antigravityThinkingFixture) serveGeminiNative(t *testing.T, action, body string, h *GatewayHandler, call func(*GatewayHandler, *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	f.ledger.commands = nil
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	path := "/v1beta/models/gemini-3.8-flash:" + action
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Params = gin.Params{{Key: "modelAction", Value: "gemini-3.8-flash:" + action}}
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	call(h, c)
	return recorder
}

func (f *antigravityThinkingFixture) assertFrozenMatchesOutbound(t *testing.T, c *gin.Context, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	models := f.upstream.upstreamModels()
	require.Len(t, models, 1, "must reach upstream exactly once")
	require.Len(t, f.ledger.commands, 1, "successful response must be billed exactly once")
	bundle := service.GatewayRequestBillingFromContext(c.Request.Context())
	require.NotNil(t, bundle)
	frozen, err := bundle.UpstreamModelFor(f.group.ID, f.account.ID)
	require.NoError(t, err)
	require.Equal(t, models[0], frozen, "frozen model must equal the actual outbound model")
}

// geminiNativeLockedUpstream 捕获 Antigravity Gemini 原生转发的 v1internal wrapped
// body.model，并按 SSE 返回 Gemini 响应。F6 的变体选择体现在 wrapped body.model，
// 不在 URL 路径里。
type geminiNativeLockedUpstream struct {
	service.HTTPUpstream
	mu     sync.Mutex
	models []string
}

func (u *geminiNativeLockedUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	model := gjson.GetBytes(body, "model").String()
	u.mu.Lock()
	u.models = append(u.models, model)
	u.mu.Unlock()
	respBody := "data: " + `{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":500}}}` + "\n\n"

	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req-gemini-1"}},
		Body: io.NopCloser(strings.NewReader(respBody))}, nil
}

func (u *geminiNativeLockedUpstream) upstreamModels() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.models...)
}

// TestGeminiNativeLockedPricingThinkingLevelMatchesOutbound 是 F6 的 handler 级回归：
// Gemini 原生 generateContent/streamGenerateContent 经 Antigravity OAuth 账号转发时，
// 冻结值必须等于 wrapped body.model 的实际出站变体，成功响应只入账一次且金额非零。
// 缺省 thinkingConfig 与 minimal 都按现有实现语义落到 high。
func TestGeminiNativeLockedPricingThinkingLevelMatchesOutbound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, action := range []string{"generateContent", "streamGenerateContent"} {
		for _, tc := range []struct {
			name     string
			body     string
			expected string
		}{
			{name: "low", expected: "gemini-3.8-flash-low",
				body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"low"}}}`},
			{name: "medium", expected: "gemini-3.8-flash-medium",
				body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"medium"}}}`},
			{name: "high", expected: "gemini-3.8-flash-high",
				body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}`},
			{name: "defaults-high", expected: "gemini-3.8-flash-high",
				body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`},
			{name: "minimal-defaults-high", expected: "gemini-3.8-flash-high",
				body: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"minimal"}}}`},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				// 复用 antigravityThinkingFixture 的账号/分组/价格与 Antigravity 转发装配；
				// Gemini 原生入口需要把分组平台和 Account.Groups 对到 native handler。
				f := newAntigravityThinkingFixture(t, service.PlatformGemini)
				upstream := &geminiNativeLockedUpstream{}
				h := newGeminiNativeHandlerFromAntigravityFixture(t, f, upstream)
				var captured *gin.Context
				recorder := f.serveGeminiNative(t, action, tc.body, h, func(h *GatewayHandler, c *gin.Context) {
					captured = c
					h.GeminiV1BetaModels(c)
				})
				require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
				require.Equal(t, []string{tc.expected}, upstream.upstreamModels())
				bundle := service.GatewayRequestBillingFromContext(captured.Request.Context())
				require.NotNil(t, bundle)
				frozen, err := bundle.UpstreamModelFor(f.group.ID, f.account.ID)
				require.NoError(t, err)
				require.Equal(t, tc.expected, frozen, "frozen model must equal the actual outbound wrapped body.model")
				require.Len(t, f.ledger.commands, 1, "successful response must be billed exactly once")
				require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0, "successful response must not be zero-billed")
			})
		}
	}
}

// newGeminiNativeHandlerFromAntigravityFixture 复用 antigravityThinkingFixture 的
// Group/Account/Pricing，构造 Gemini 原生入口所需的分组平台与带捕获桩的 handler。
func newGeminiNativeHandlerFromAntigravityFixture(t *testing.T, f *antigravityThinkingFixture, upstream *geminiNativeLockedUpstream) *GatewayHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	f.group.Platform = service.PlatformGemini
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: service.PlatformGemini}}, nil, nil, nil, nil)
	billing := service.NewBillingService(cfg, nil)
	logs := &lockedHTTPLogs{}
	gatewayService := service.NewGatewayService(
		nil, &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{f.group.ID: f.group}}, logs, f.ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(&groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{f.account}}}, nil, nil, nil, cfg),
		service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, upstream, nil,
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil,
	)
	tokenProvider := service.NewAntigravityTokenProvider(nil, nil, nil)
	antigravitySettings := service.NewSettingService(&lockedRegressionSettingRepo{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1 << 20}})
	antigravity := service.NewAntigravityGatewayService(nil, nil, nil, tokenProvider, nil, upstream, antigravitySettings, nil)
	billingCache := service.NewBillingCacheService(&lockedHTTPFunds{balance: 100}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	return &GatewayHandler{
		gatewayService:            gatewayService,
		antigravityGatewayService: antigravity,
		billingCacheService:       billingCache,
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:        1,
		cfg:                       cfg,
	}
}

func TestSharedChatCompletionsLockedPricingReasoningEffortMatchesOutbound(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		expected string
	}{
		{name: "medium", expected: "gemini-3.8-flash-medium",
			body: `{"model":"gemini-3.8-flash","max_completion_tokens":256,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"medium","stream":false}`},
		{name: "no-effort-defaults-high", expected: "gemini-3.8-flash-high",
			body: `{"model":"gemini-3.8-flash","max_completion_tokens":256,"messages":[{"role":"user","content":"hi"}],"stream":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAntigravityThinkingFixture(t, service.PlatformAntigravity)
			var captured *gin.Context
			recorder := f.serve(t, "/v1/chat/completions", tc.body, func(h *GatewayHandler, c *gin.Context) {
				captured = c
				h.ChatCompletions(c)
			})
			require.Equal(t, []string{tc.expected}, f.upstream.upstreamModels())
			f.assertFrozenMatchesOutbound(t, captured, recorder)
		})
	}
}

// TestSharedResponsesLockedPricingReasoningEffortMatchesOutbound 覆盖 F7（Responses）。
func TestSharedResponsesLockedPricingReasoningEffortMatchesOutbound(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		expected string
	}{
		{name: "medium", expected: "gemini-3.8-flash-medium",
			body: `{"model":"gemini-3.8-flash","max_output_tokens":256,"input":"hi","reasoning":{"effort":"medium"},"stream":false}`},
		{name: "no-effort-defaults-high", expected: "gemini-3.8-flash-high",
			body: `{"model":"gemini-3.8-flash","max_output_tokens":256,"input":"hi","stream":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAntigravityThinkingFixture(t, service.PlatformAntigravity)
			var captured *gin.Context
			recorder := f.serve(t, "/v1/responses", tc.body, func(h *GatewayHandler, c *gin.Context) {
				captured = c
				h.Responses(c)
			})
			require.Equal(t, []string{tc.expected}, f.upstream.upstreamModels())
			f.assertFrozenMatchesOutbound(t, captured, recorder)
		})
	}
}
