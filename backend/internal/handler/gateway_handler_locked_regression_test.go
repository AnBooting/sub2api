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

// lockedRegressionSettingRepo 是 Antigravity 转换选项所需的最小设置仓储：
// 身份补丁相关的读取一律返回"未配置"，让转换走内置默认模板。
type lockedRegressionSettingRepo struct {
	service.SettingRepository
}

func (r *lockedRegressionSettingRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func (r *lockedRegressionSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

// messagesAntigravityUpstream 记录 Antigravity /v1/messages 的实际上游请求体模型。
type messagesAntigravityUpstream struct {
	service.HTTPUpstream
	mu       sync.Mutex
	models   []string
	respBody []byte
	status   int
}

func (u *messagesAntigravityUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	model := gjson.GetBytes(body, "model").String()
	u.mu.Lock()
	u.models = append(u.models, model)
	resp := u.respBody
	status := u.status
	u.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	if len(resp) == 0 {
		// Antigravity Claude 转发消费的是 Gemini 形态的 SSE。
		resp = []byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1000,\"candidatesTokenCount\":500},\"modelVersion\":\"claude-sonnet-4-5-thinking\"}}\n\n")
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req-ag-1"}},
		Body: io.NopCloser(strings.NewReader(string(resp)))}, nil
}

func (u *messagesAntigravityUpstream) upstreamModels() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.models...)
}

// TestMessagesLockedPricingAntigravityThinkingBillsAtFrozenModel 锁定 F1 回归：
// Antigravity + thinking 请求的冻结账号模型必须等于真实出站模型，并且落账 1 条且金额 > 0。
// 修复前：冻结侧硬编码 thinking=false，出站追加 -thinking，消费侧等值核对失败 →
// HTTP 200 但零扣费。
func TestMessagesLockedPricingAntigravityThinkingBillsAtFrozenModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		groupID   = int64(7600)
		accountID = int64(7610)
	)
	input, output, zero := 1e-6, 2e-6, 0.0
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1,
		ModelPricing: []service.ChannelModelPricing{
			{Models: []string{"client-model"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero},
			// 冻结 scope 的计费基准是账号映射后的模型，因此它也必须被定价。
			{Models: []string{"claude-sonnet-4-5", "claude-sonnet-4-5-thinking"}, InputPrice: &input, OutputPrice: &output, CacheReadPrice: &zero, CacheWritePrice: &zero},
		}}
	account := &service.Account{ID: accountID, Name: "ag-thinking", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "fixture-not-a-secret", "project_id": "fixture-project",
			// 账号映射必须同时认下 thinking 变体，否则调度阶段就会以
			// "model_unsupported" 过滤掉该账号（真实 Antigravity 账号的默认映射含变体）。
			"model_mapping": map[string]any{
				"client-model":               "claude-sonnet-4-5",
				"claude-sonnet-4-5":          "claude-sonnet-4-5",
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
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
	// 非流式请求会走"收集上游 SSE 再转换"的路径，该路径读 settingService 的
	// MaxLineSize；缺了它会 panic（生产装配恒有）。
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
	key := &service.APIKey{ID: 7620, UserID: 7630, GroupID: messagesLockedInt64(groupID), Group: group, Status: service.StatusActive,
		User: &service.User{ID: 7630, Status: service.StatusActive, Concurrency: 10, Balance: 100}}

	body := `{"model":"client-model","max_tokens":256,"thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.0.0 (external, cli)")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	models := upstream.upstreamModels()
	require.Len(t, models, 1)
	require.Equal(t, "claude-sonnet-4-5-thinking", models[0], "出站必须带上 thinking 后缀")

	bundle := service.GatewayRequestBillingFromContext(c.Request.Context())
	require.NotNil(t, bundle)
	frozen, err := bundle.UpstreamModelFor(groupID, accountID)
	require.NoError(t, err)
	require.Equal(t, models[0], frozen, "冻结账号模型必须等于实际上游出站模型")

	require.Len(t, ledger.commands, 1, "thinking 请求必须落账 1 条，不能 200 零计费")
	require.Greater(t, ledger.commands[0].BalanceCost, 0.0, "thinking 请求金额必须 > 0")
}

// TestMessagesLockedPricingWarmupShapeWithoutInterceptFlagStillFailsClosed 锁定 F3 回归：
// warmup 形状 + 账号未开启拦截时请求照常发上游，缺价必须在发上游前 503（upstreamCalls=0）。
// 修复前：准入只按 body 形状豁免，导致这类请求绕过锁价并按 legacy 兜底价放行。
func TestMessagesLockedPricingWarmupShapeWithoutInterceptFlagStillFailsClosed(t *testing.T) {
	f := newMessagesLockedFixture(t, false, "")
	// 账号不开 intercept_warmup_requests：形状命中但不会 mock。
	body := `{"model":"client-model","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`
	rec, _ := f.serve(t, body)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Equal(t, "pricing_unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Empty(t, f.upstream.callList(), "缺价绝不能放行到上游")
	require.Empty(t, f.ledger.commands)
}

// TestMessagesLockedPricingWarmupShapeWithInterceptFlagStillMocks 锁定 F3 的另一半：
// warmup 形状 + 账号确实开启拦截时仍返回 mock，且不发上游、不扣费（即使分组缺价）。
func TestMessagesLockedPricingWarmupShapeWithInterceptFlagStillMocks(t *testing.T) {
	f := newMessagesLockedFixture(t, false, "")
	f.account.Credentials["intercept_warmup_requests"] = true
	body := `{"model":"client-model","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`
	rec, _ := f.serve(t, body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, f.upstream.callList())
	require.Empty(t, f.ledger.commands)
	require.Contains(t, rec.Body.String(), "New Conversation")
}
