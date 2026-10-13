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
)

// messagesPromptTooLongUpstream 对指定账号返回 prompt too long（触发 Messages 的
// fallbackGroupID 分组切换），其余账号正常返回。
type messagesPromptTooLongUpstream struct {
	service.HTTPUpstream
	mu            sync.Mutex
	accounts      []int64
	promptTooLong map[int64]bool
}

func (u *messagesPromptTooLongUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	_, _ = io.ReadAll(req.Body)
	u.mu.Lock()
	u.accounts = append(u.accounts, accountID)
	tooLong := u.promptTooLong[accountID]
	u.mu.Unlock()
	if tooLong {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"req-ptl"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Prompt is too long"}}`))}, nil
	}
	resp := `{"id":"msg_fixture","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":500}}`
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"up-1"}}, Body: io.NopCloser(strings.NewReader(resp))}, nil
}

func (u *messagesPromptTooLongUpstream) calledAccounts() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accounts...)
}

// TestMessagesLockedPricingFallbackRebillsAtFallbackGroupPrice 锁定 D-5 端到端：
// 主分组账号返回 prompt too long 后切到 fallback 分组，最终计费使用 fallback 分组
// 重新冻结的价格，而不是旧 bundle 的主分组价，也不是记录时刻的 live 价。
func TestMessagesLockedPricingFallbackRebillsAtFallbackGroupPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		primaryGroupID  = int64(7500)
		fallbackGroupID = int64(7501)
		primaryAcctID   = int64(7510)
		fallbackAcctID  = int64(7511)
	)
	primaryIn, primaryOut := 1e-6, 2e-6
	fallbackIn, fallbackOut := 7e-6, 11e-6
	zero := 0.0
	fallbackID := fallbackGroupID
	primary := &service.Group{ID: primaryGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1,
		FallbackGroupIDOnInvalidRequest: &fallbackID,
		ModelPricing:                    []service.ChannelModelPricing{{Models: []string{"client-model", "claude-sonnet-4"}, InputPrice: &primaryIn, OutputPrice: &primaryOut, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
	fallback := &service.Group{ID: fallbackGroupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1,
		ModelPricing: []service.ChannelModelPricing{{Models: []string{"client-model", "claude-sonnet-4"}, InputPrice: &fallbackIn, OutputPrice: &fallbackOut, CacheReadPrice: &zero, CacheWritePrice: &zero}}}

	// 主账号是 Antigravity OAuth：它是唯一能产出 PromptTooLongError 的转发路径。
	primaryAccount := &service.Account{ID: primaryAcctID, Name: "ag-primary", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials:   map[string]any{"access_token": "fixture-not-a-secret", "project_id": "fixture-project", "model_mapping": map[string]any{"client-model": "claude-sonnet-4"}},
		Extra:         map[string]any{"mixed_scheduling": true},
		AccountGroups: []service.AccountGroup{{AccountID: primaryAcctID, GroupID: primaryGroupID}}}
	fallbackAccount := &service.Account{ID: fallbackAcctID, Name: "fallback", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "claude-sonnet-4"}},
		AccountGroups: []service.AccountGroup{{AccountID: fallbackAcctID, GroupID: fallbackGroupID}}}

	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{primaryAccount, fallbackAccount}}}
	groupRepo := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{primaryGroupID: primary, fallbackGroupID: fallback}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 2
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{primaryGroupID: service.PlatformAnthropic, fallbackGroupID: service.PlatformAnthropic}}, nil, nil, nil, nil)
	upstream := &messagesPromptTooLongUpstream{promptTooLong: map[int64]bool{primaryAcctID: true}}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	logs := &lockedHTTPLogs{}
	gatewayService := service.NewGatewayService(
		nil, groupRepo, logs, ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg),
		service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, nil, nil, upstream, nil,
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil,
	)
	tokenProvider := service.NewAntigravityTokenProvider(nil, nil, nil)
	antigravity := service.NewAntigravityGatewayService(nil, nil, nil, tokenProvider, nil, upstream, nil, nil)
	billingCache := service.NewBillingCacheService(&lockedHTTPFunds{balance: 100}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	h := &GatewayHandler{
		gatewayService:            gatewayService,
		antigravityGatewayService: antigravity,
		billingCacheService:       billingCache,
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		maxAccountSwitches:        2,
		cfg:                       cfg,
	}
	key := &service.APIKey{ID: 7520, UserID: 7530, GroupID: messagesLockedInt64(primaryGroupID), Group: primary, Status: service.StatusActive,
		User: &service.User{ID: 7530, Status: service.StatusActive, Concurrency: 10, Balance: 100}}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, primary)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(messagesLockedBody(false))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.0.0 (external, cli)")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []int64{primaryAcctID, fallbackAcctID}, upstream.calledAccounts(),
		"primary must fail with prompt too long and the fallback group must serve the retry")
	require.Len(t, ledger.commands, 1)
	require.InDelta(t, 1000*fallbackIn+500*fallbackOut, ledger.commands[0].BalanceCost, 1e-15,
		"the fallback attempt must be billed with the fallback group's refrozen price")
	require.True(t, strings.HasPrefix(ledger.commands[0].RequestID, "locked:"))
}
