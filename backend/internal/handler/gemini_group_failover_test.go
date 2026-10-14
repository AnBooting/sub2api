//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func nativeChainBody() string {
	return `{"contents":[{"role":"user","parts":[{"text":"hello","thoughtSignature":"old-signature"}]}],"generationConfig":{"maxOutputTokens":128,"thinkingConfig":{"thinkingLevel":"low"}}}`
}
func serveNativeChain(t *testing.T, f *sharedLockedFixture, stream bool) *httptest.ResponseRecorder {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent"
	}
	return f.serve(t, "/v1beta/models/client-model:"+action, nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
		c.Params = gin.Params{{Key: "modelAction", Value: "client-model:" + action}}
		h.GeminiV1BetaModels(c)
	})
}
func newNativeChainFixture(t *testing.T) (*sharedLockedFixture, *service.Group, *nativeObservedUpstream) {
	f, b, chain := newResponsesChainFixture(t)
	for _, g := range []*service.Group{f.group, b} {
		g.Platform = service.PlatformGemini
		g.ModelPricing[0].Models = append(g.ModelPricing[0].Models, "gemini-2.5-flash")
	}
	for _, a := range []*service.Account{f.account, f.fallback} {
		a.Platform = service.PlatformGemini
		a.Credentials = map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}}
	}
	upstream := &nativeObservedUpstream{ccGeminiUpstream: &ccGeminiUpstream{geminiLockedUpstream: &geminiLockedUpstream{failFirstIDs: map[int64]bool{}}}, failID: f.account.ID, cache: chain.cache}
	f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, upstream, nil, f.handler.cfg)
	f.handler.maxAccountSwitchesGemini = 2
	groups := chain.groups
	cfg := f.handler.cfg
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: service.PlatformGemini, b.ID: service.PlatformGemini}}, nil, nil, nil, nil)
	accounting := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(accounting.Stop)
	f.handler.gatewayService = service.NewGatewayService(nil, groups, f.logs, f.ledger, nil, nil, nil, nil, cfg, service.NewSchedulerSnapshotService(&nativePlatformCache{groupScopedSchedulerCache: chain.cache}, nil, nil, nil, cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, accounting, nil, upstream, service.NewDeferredService(nil, nil, 0), nil, nil, nil, service.NewDigestSessionStore(), nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	return f, b, upstream
}
func TestNativeGeminiGroupFailover(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			f, b, u := newNativeChainFixture(t)
			rec := serveNativeChain(t, f, stream)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, f.ledger.commands, 1)
			require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
			require.Len(t, f.logs.rows, 1)
			require.Contains(t, u.callList()[len(u.callList())-1], fmt.Sprint(f.fallback.ID))
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, len(u.callList()))
		})
	}
}
func TestNativeGeminiGroupMaxZeroAndRevocation(t *testing.T) {
	for _, mode := range []string{"max0", "revoked", "missing", "wrong-platform", "allowlist"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newNativeChainFixture(t)
			switch mode {
			case "max0":
				f.handler.maxAccountSwitchesGemini = 0
			case "revoked":
				b.Status = "disabled"
			case "missing":
				b.ModelPricing = nil
			case "wrong-platform":
				b.Platform = service.PlatformAnthropic
			case "allowlist":
				b.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"other"}}
			}
			rec := serveNativeChain(t, f, false)
			require.NotEqual(t, 200, rec.Code)
			require.Empty(t, f.ledger.commands)
			for _, call := range u.callList() {
				require.True(t, strings.HasPrefix(call, fmt.Sprint(f.account.ID)+":"))
			}
		})
	}
}

type nativeObservedUpstream struct {
	cache            *groupScopedSchedulerCache
	additionalFailID int64
	*ccGeminiUpstream
	failID int64
	before func(*http.Request)
	bodies []string
}

func (u *nativeObservedUpstream) Do(req *http.Request, proxy string, id int64, limit int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	u.bodies = append(u.bodies, string(body))
	if u.before != nil {
		u.before(req)
	}
	if id == u.failID || id == u.additionalFailID {
		u.mu.Lock()
		u.calls = append(u.calls, fmt.Sprintf("%d:%s", id, req.URL.Path))
		u.mu.Unlock()
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}
	return u.ccGeminiUpstream.Do(req, proxy, id, limit)
}
func TestNativeGeminiGroupSignatureAndFrozenMultiplier(t *testing.T) {
	f, b, u := newNativeChainFixture(t)
	u.before = func(*http.Request) { b.RateMultiplier = 99 }
	rec := serveNativeChain(t, f, false)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, u.bodies, 2)
	for _, body := range u.bodies {
		require.NotContains(t, body, "old-signature")
		require.Equal(t, int64(128), gjson.Get(body, "generationConfig.maxOutputTokens").Int())
		require.Equal(t, "low", gjson.Get(body, "generationConfig.thinkingConfig.thinkingLevel").String())
	}
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.0057, f.ledger.commands[0].BalanceCost, 0.000001)
}
func TestNativeGeminiGroupSubscriptionAndWorker(t *testing.T) {
	f, b, _ := newNativeChainFixture(t)
	now := time.Now()
	subs := map[int64]*service.UserSubscription{}
	for _, g := range []*service.Group{f.group, b} {
		g.SubscriptionType = service.SubscriptionTypeSubscription
		subs[g.ID] = &service.UserSubscription{ID: g.ID + 100, UserID: f.key.UserID, GroupID: g.ID, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
	}
	groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{f.group.ID: f.group, b.ID: b}}
	repo := &responsesChainSubRepo{subs: subs}
	f.handler.apiKeyService = service.NewAPIKeyService(&responsesChainKeyRepo{key: f.key}, &responsesChainUserRepo{user: f.key.User}, groups, repo, nil, nil, f.handler.cfg)
	f.handler.subscriptionService = service.NewSubscriptionService(groups, repo, nil, nil, nil)
	t.Cleanup(f.handler.subscriptionService.Stop)
	funds := &responsesChainFunds{handlerInflightCache: newHandlerInflightCache(100), subs: subs}
	billing := service.NewBillingCacheService(funds, nil, repo, nil, nil, nil, f.handler.cfg, nil)
	t.Cleanup(billing.Stop)
	f.handler.billingCacheService = billing
	pool := newUsageRecordTestPool(t)
	f.handler.usageRecordWorkerPool = pool
	entered, release := make(chan struct{}), make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	rec := serveNativeChain(t, f, false)
	close(release)
	pool.Stop()
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.logs.rows, 1)
	require.Equal(t, subs[b.ID].ID, *f.logs.rows[0].SubscriptionID)
	require.Zero(t, f.ledger.commands[0].BalanceCost)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 2)
}
func TestNativeGeminiGroupReservationWorker(t *testing.T) {
	f, _, _ := newNativeChainFixture(t)
	funds := newHandlerInflightCache(100)
	f.handler.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billing := service.NewBillingCacheService(funds, nil, nil, nil, nil, nil, f.handler.cfg, nil)
	t.Cleanup(billing.Stop)
	f.handler.billingCacheService = billing
	pool := newUsageRecordTestPool(t)
	f.handler.usageRecordWorkerPool = pool
	entered, release := make(chan struct{}), make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	rec := serveNativeChain(t, f, false)
	held := funds.count()
	close(release)
	pool.Stop()
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 1, held)
	require.Zero(t, funds.count())
	require.Len(t, f.logs.rows, 1)
}

func TestNativeGeminiGroupNoReplayAfterOutput(t *testing.T) {
	f, _, u := newNativeChainFixture(t)
	rec := f.serve(t, "/v1beta/models/client-model:streamGenerateContent", nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
		c.Params = gin.Params{{Key: "modelAction", Value: "client-model:streamGenerateContent"}}
		u.before = func(*http.Request) { c.Writer.WriteString("data: {\"text\":\"visible\"}\n\n"); c.Writer.Flush() }
		h.GeminiV1BetaModels(c)
	})
	require.Len(t, u.callList(), 1)
	require.Equal(t, "data: {\"text\":\"visible\"}\n\n", rec.Body.String())
	require.Empty(t, f.ledger.commands)
}
func TestNativeGeminiGroupForcedRouteRejectsForeignPlatform(t *testing.T) {
	f, _, u := newNativeChainFixture(t)
	rec := f.serve(t, "/antigravity/v1beta/models/client-model:generateContent", nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
		c.Params = gin.Params{{Key: "modelAction", Value: "client-model:generateContent"}}
		middleware.ForcePlatform(service.PlatformAntigravity)(c)
		h.GeminiV1BetaModels(c)
	})
	require.NotEqual(t, 200, rec.Code, rec.Body.String())
	require.Empty(t, u.callList())
	require.Empty(t, f.ledger.commands)
}
func TestNativeGeminiGroupSnapshotBeforeWorker(t *testing.T) {
	f, b, u := newNativeChainFixture(t)
	pool := newUsageRecordTestPool(t)
	f.handler.usageRecordWorkerPool = pool
	entered, release := make(chan struct{}), make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	var budget *service.RequestAttemptBudget
	u.before = func(req *http.Request) { budget = service.RequestAttemptBudgetFromContext(req.Context()) }
	rec := serveNativeChain(t, f, false)
	require.NoError(t, budget.AllowSend())
	*f.key.GroupID = 999
	close(release)
	pool.Stop()
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 2)
}

func TestNativeGeminiGroupAccountOrder(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f, b, u := newNativeChainFixture(t)
			a2 := *f.account
			a2.ID = 7612
			a2.Priority = 2
			a2.AccountGroups = []service.AccountGroup{{AccountID: a2.ID, GroupID: f.group.ID}}
			u.cache.accounts = append(u.cache.accounts, &a2)
			u.additionalFailID = a2.ID
			rec := serveNativeChain(t, f, stream)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, u.callList(), 3)
			for i, id := range []int64{f.account.ID, a2.ID, f.fallback.ID} {
				require.True(t, strings.HasPrefix(u.callList()[i], fmt.Sprint(id)+":"))
			}
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 3)
		})
	}
}
func TestNativeGeminiGroupToAntigravity(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f, b, u := newNativeChainFixture(t)
			ag := newAntigravityThinkingFixture(t, service.PlatformAntigravity)
			id := f.fallback.ID
			*f.fallback = *ag.account
			f.fallback.ID = id
			f.fallback.AccountGroups = []service.AccountGroup{{AccountID: id, GroupID: b.ID}}
			b.Platform = service.PlatformAntigravity
			b.ModelPricing = ag.group.ModelPricing
			agUpstream := &geminiNativeLockedUpstream{}
			settings := service.NewSettingService(&lockedRegressionSettingRepo{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1 << 20}})
			f.handler.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil, service.NewAntigravityTokenProvider(nil, nil, nil), nil, agUpstream, settings, nil)
			rec := serveNativeChain(t, f, stream)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, u.callList(), 1)
			require.Equal(t, []string{"gemini-3.8-flash"}, agUpstream.upstreamModels())
			require.Len(t, f.ledger.commands, 1)
			require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 2)
		})
	}
}

func TestNativeGeminiGroupForcedAntigravityOnGeminiGroup(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f, _, u := newNativeChainFixture(t)
			ag := newAntigravityThinkingFixture(t, service.PlatformGemini)
			id := f.account.ID
			*f.account = *ag.account
			f.account.ID = id
			f.account.AccountGroups = []service.AccountGroup{{AccountID: id, GroupID: f.group.ID}}
			f.group.ModelPricing = ag.group.ModelPricing
			agUpstream := &geminiNativeLockedUpstream{}
			settings := service.NewSettingService(&lockedRegressionSettingRepo{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1 << 20}})
			f.handler.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil, service.NewAntigravityTokenProvider(nil, nil, nil), nil, agUpstream, settings, nil)
			action := "generateContent"
			if stream {
				action = "streamGenerateContent"
			}
			rec := f.serve(t, "/antigravity/v1beta/models/client-model:"+action, nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
				c.Params = gin.Params{{Key: "modelAction", Value: "client-model:" + action}}
				c.Set("force_platform", service.PlatformAntigravity)
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.ForcePlatform, service.PlatformAntigravity))
				h.GeminiV1BetaModels(c)
			})
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Empty(t, u.callList())
			require.Equal(t, []string{"gemini-3.8-flash"}, agUpstream.upstreamModels())
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
		})
	}
}

func TestNativeGeminiGroupFrozenChannelMapping(t *testing.T) {
	f, b, u := newNativeChainFixture(t)
	groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{f.group.ID: f.group, b.ID: b}}
	repo := &openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: service.PlatformGemini, b.ID: service.PlatformGemini}, channels: []service.Channel{
		{ID: 1, Status: service.StatusActive, GroupIDs: []int64{f.group.ID}, ModelMapping: map[string]map[string]string{service.PlatformGemini: {"client-model": "gemini-2.5-flash"}}},
		{ID: 2, Status: service.StatusActive, GroupIDs: []int64{b.ID}, ModelMapping: map[string]map[string]string{service.PlatformGemini: {"client-model": "gemini-2.0-flash"}}},
	}}
	b.ModelPricing[0].Models = append(b.ModelPricing[0].Models, "gemini-2.0-flash")
	f.fallback.Credentials["model_mapping"] = map[string]any{"client-model": "gemini-2.0-flash", "gemini-2.0-flash": "gemini-2.0-flash"}
	f.account.Credentials["model_mapping"] = map[string]any{"client-model": "gemini-2.5-flash", "gemini-2.5-flash": "gemini-2.5-flash"}
	channels := service.NewChannelService(repo, nil, nil, nil, nil)
	billing := service.NewBillingService(f.handler.cfg, nil)
	accounting := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, f.handler.cfg, nil)
	t.Cleanup(accounting.Stop)
	f.handler.gatewayService = service.NewGatewayService(nil, groups, f.logs, f.ledger, nil, nil, nil, nil, f.handler.cfg, service.NewSchedulerSnapshotService(u.cache, nil, nil, nil, f.handler.cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, accounting, nil, u, service.NewDeferredService(nil, nil, 0), nil, nil, nil, service.NewDigestSessionStore(), nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	rec := serveNativeChain(t, f, false)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, u.callList(), 2)
	require.Contains(t, u.callList()[0], "gemini-2.5-flash")
	require.Contains(t, u.callList()[1], "gemini-2.0-flash")
	require.Len(t, f.ledger.commands, 1)
	require.Len(t, f.logs.rows, 1)
}
func TestNativeGeminiGroupContextIsolationAndCancel(t *testing.T) {
	t.Run("isolation", func(t *testing.T) {
		f, b, u := newNativeChainFixture(t)
		var contexts []context.Context
		u.before = func(req *http.Request) { contexts = append(contexts, req.Context()) }
		rec := serveNativeChain(t, f, false)
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Len(t, contexts, 2)
		require.Equal(t, b.ID, contexts[1].Value(ctxkey.Group).(*service.Group).ID)
		account, ok := service.PrefetchedStickyAccountIDFromContext(contexts[1])
		require.True(t, ok)
		require.Zero(t, account)
		group, ok := service.PrefetchedStickyGroupIDFromContext(contexts[1])
		require.True(t, ok)
		require.Equal(t, b.ID, group)
		single, ok := service.SingleAccountRetryFromContext(contexts[1])
		require.True(t, ok)
		require.False(t, single)
	})
	t.Run("cancel", func(t *testing.T) {
		f, _, u := newNativeChainFixture(t)
		rec := f.serve(t, "/v1beta/models/client-model:generateContent", nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			c.Params = gin.Params{{Key: "modelAction", Value: "client-model:generateContent"}}
			u.before = func(*http.Request) { cancel() }
			h.GeminiV1BetaModels(c)
			require.Equal(t, statusClientClosedRequest, c.Writer.Status())
		})
		require.Len(t, u.callList(), 1)
		require.Empty(t, rec.Body.String())
		require.Empty(t, f.ledger.commands)
	})
}

func TestNativeGeminiGroupSingleGroupCompatibility(t *testing.T) {
	f, _, u := newNativeChainFixture(t)
	f.key.Groups = f.key.Groups[:1]
	u.failID = 0
	rec := serveNativeChain(t, f, false)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.logs.rows, 1)
	require.Len(t, f.ledger.commands, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
}
func TestNativeGeminiGroupScopeZeroCompatibility(t *testing.T) {
	f, _, u := newNativeChainFixture(t)
	f.key.Groups = nil
	f.key.Group = nil
	f.key.GroupID = nil
	f.account.AccountGroups = []service.AccountGroup{{AccountID: f.account.ID, GroupID: 0}}
	u.failID = 0
	f.account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4": "claude-sonnet-4"}
	rec := f.serve(t, "/v1beta/models/claude-sonnet-4:generateContent", nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
		c.Params = gin.Params{{Key: "modelAction", Value: "claude-sonnet-4:generateContent"}}
		middleware.ForcePlatform(service.PlatformGemini)(c)
		h.GeminiV1BetaModels(c)
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.logs.rows, 1)
	require.Len(t, f.ledger.commands, 1)
	row := f.logs.rows[0]
	require.Nil(t, row.RequestedGroupID)
	require.Nil(t, row.ServedGroupID)
	require.Nil(t, row.Failover)
	require.Nil(t, row.AttemptCount)
}

// Match the real snapshot bucket's platform contract rather than returning all
// group members (the shared legacy fixture intentionally only filters groups).
type nativePlatformCache struct{ *groupScopedSchedulerCache }

func (c *nativePlatformCache) GetSnapshot(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	accounts, ok, err := c.groupScopedSchedulerCache.GetSnapshot(ctx, bucket)
	out := make([]*service.Account, 0, len(accounts))
	for _, a := range accounts {
		if a.Platform == bucket.Platform || (bucket.Mode == service.SchedulerModeMixed && a.Platform == service.PlatformAntigravity && a.Extra["mixed_scheduling"] == true) {
			out = append(out, a)
		}
	}
	return out, ok, err
}

func TestNativeGeminiGroupDigestIsolation(t *testing.T) {
	f, b, _ := newNativeChainFixture(t)
	var prefix, chain string
	rec := f.serve(t, "/v1beta/models/client-model:generateContent", nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
		c.Params = gin.Params{{Key: "modelAction", Value: "client-model:generateContent"}}
		var req antigravity.GeminiRequest
		require.NoError(t, json.Unmarshal([]byte(nativeChainBody()), &req))
		chain = service.BuildGeminiDigestChain(&req)
		prefix = service.GenerateGeminiPrefixHash(f.key.UserID, f.key.ID, c.ClientIP(), c.GetHeader("User-Agent"), service.PlatformGemini, "client-model")
		require.NoError(t, h.gatewayService.SaveGeminiSession(c.Request.Context(), f.group.ID, prefix, chain, "A-only-session", f.account.ID, ""))
		h.GeminiV1BetaModels(c)
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())
	aUUID, aAccount, _, aFound := f.handler.gatewayService.FindGeminiSession(context.Background(), f.group.ID, prefix, chain)
	bUUID, bAccount, _, bFound := f.handler.gatewayService.FindGeminiSession(context.Background(), b.ID, prefix, chain)
	require.True(t, aFound)
	require.True(t, bFound)
	require.Equal(t, "A-only-session", aUUID)
	require.Equal(t, f.account.ID, aAccount)
	require.NotEqual(t, aUUID, bUUID)
	require.Equal(t, f.fallback.ID, bAccount)
}
