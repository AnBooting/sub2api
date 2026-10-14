//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesChainKeyRepo struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r *responsesChainKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	return r.key, nil
}

type responsesChainUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *responsesChainUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

type responsesChainUpstream struct {
	service.HTTPUpstream
	delegate      *sharedLockedAnthropicUpstream
	status        int
	beforeFailure func(*http.Request)
	calls         int
	groups        *groupMapRepo
	cache         *groupScopedSchedulerCache
}

func (u *responsesChainUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.calls++
	if u.beforeFailure != nil && u.calls == 1 {
		u.beforeFailure(req)
	}
	if u.status != 0 && u.calls == 1 {
		return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader("{\"error\":{\"type\":\"invalid_request_error\",\"message\":\"invalid request\"}}"))}, nil
	}
	return u.delegate.Do(req, proxy, id, concurrency)
}

func newResponsesChainFixture(t *testing.T) (*sharedLockedFixture, *service.Group, *responsesChainUpstream) {
	t.Helper()
	f := newSharedLockedFixture(t, true, "")
	b := *f.group
	b.ID = 7700
	b.RateMultiplier = 3
	f.fallback.AccountGroups = []service.AccountGroup{{AccountID: f.fallback.ID, GroupID: b.ID}}
	f.key.Groups = []service.APIKeyGroupLink{{GroupID: f.group.ID, Role: service.APIKeyGroupRoleNormal, Group: f.group}, {GroupID: b.ID, Role: service.APIKeyGroupRoleNormal, Position: 1, Group: &b}}
	groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{f.group.ID: f.group, b.ID: &b}}
	cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{f.account, f.fallback}}}
	cfg := f.handler.cfg
	accountingCache := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(accountingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: service.PlatformAnthropic, b.ID: service.PlatformAnthropic}}, nil, nil, nil, nil)
	upstream := &responsesChainUpstream{delegate: f.upstream, groups: groups, cache: cache}
	f.handler.gatewayService = service.NewGatewayService(nil, groups, f.logs, f.ledger, nil, nil, nil, nil, cfg,
		service.NewSchedulerSnapshotService(cache, nil, nil, nil, cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, accountingCache, nil, upstream, service.NewDeferredService(nil, nil, 0),
		nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	f.handler.apiKeyService = service.NewAPIKeyService(&responsesChainKeyRepo{key: f.key}, &responsesChainUserRepo{user: f.key.User}, groups, nil, nil, nil, cfg)
	f.upstream.failFirstIDs[f.account.ID] = true
	return f, &b, upstream
}

func TestSharedResponsesNormalGroupFailover(t *testing.T) {

	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			f, b, u := newResponsesChainFixture(t)
			// Change live B pricing after the bundle froze, while A is failing.
			f.upstream.onFirst = func() { b.RateMultiplier = 99 }
			rec := f.serve(t, "/v1/responses", sharedLockedResponsesBody(stream), func(h *GatewayHandler, c *gin.Context) {
				h.Responses(c)
			})
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, []string{fmt.Sprintf("%d:claude-sonnet-4", f.account.ID), fmt.Sprintf("%d:claude-sonnet-4", f.fallback.ID)}, f.upstream.callList())
			require.Equal(t, 2, u.calls)
			require.Len(t, f.logs.rows, 1)
			require.Equal(t, b.ID, *f.logs.rows[0].GroupID)
			require.Len(t, f.ledger.commands, 1)
			require.InDelta(t, 0.006, f.ledger.commands[0].BalanceCost, 0.000001)
			require.Equal(t, int64(7600), *f.key.GroupID, "do not mutate authentication identity")
		})
	}
}
func TestSharedResponsesNormalGroupFailoverMaxZero(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	f.handler.maxAccountSwitches = 0
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.ledger.commands)
}
func TestSharedResponsesNormalGroupFailoverRejectsBusinessAndMissingPrice(t *testing.T) {
	t.Run("business", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		u.status = http.StatusBadRequest
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("missing price", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		b.ModelPricing = nil
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "pricing_unavailable")
		require.Equal(t, 1, u.calls, "B missing price must reject before B forwarding")
		require.Empty(t, f.ledger.commands)
	})
	t.Run("revoked B", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.Status = "disabled" }
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.NotEqual(t, http.StatusOK, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("wrong forwarding family", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.Platform = service.PlatformOpenAI }
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
}

type responsesChainSubRepo struct {
	service.UserSubscriptionRepository
	subs map[int64]*service.UserSubscription
}

func (r *responsesChainSubRepo) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	sub := r.subs[groupID]
	if sub == nil {
		return nil, service.ErrSubscriptionNotFound
	}
	return sub, nil
}

type responsesChainFunds struct {
	*handlerInflightCache
	subs map[int64]*service.UserSubscription
}

func (r *responsesChainFunds) GetSubscriptionCache(_ context.Context, _, groupID int64) (*service.SubscriptionCacheData, error) {
	s := r.subs[groupID]
	return &service.SubscriptionCacheData{Status: s.Status, ExpiresAt: s.ExpiresAt}, nil
}

func TestSharedResponsesNormalGroupSubscriptionIdentity(t *testing.T) {
	for _, mode := range []string{"sub-to-sub", "balance-to-sub", "sub-to-balance"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newResponsesChainFixture(t)
			now := time.Now()
			exp := now.Add(time.Hour)
			subs := map[int64]*service.UserSubscription{}
			for _, g := range []*service.Group{f.group, b} {
				if (mode == "balance-to-sub" && g == f.group) || (mode == "sub-to-balance" && g == b) {
					continue
				}
				g.SubscriptionType = service.SubscriptionTypeSubscription
				subs[g.ID] = &service.UserSubscription{ID: g.ID + 100, UserID: f.key.UserID, GroupID: g.ID, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: exp, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
			}
			groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{f.group.ID: f.group, b.ID: b}}
			subRepo := &responsesChainSubRepo{subs: subs}
			f.handler.apiKeyService = service.NewAPIKeyService(&responsesChainKeyRepo{key: f.key}, &responsesChainUserRepo{user: f.key.User}, groups, subRepo, nil, nil, f.handler.cfg)
			f.handler.subscriptionService = service.NewSubscriptionService(groups, subRepo, nil, nil, nil)
			t.Cleanup(f.handler.subscriptionService.Stop)
			funds := &responsesChainFunds{handlerInflightCache: newHandlerInflightCache(100), subs: subs}
			f.handler.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
			billing := service.NewBillingCacheService(funds, nil, subRepo, nil, nil, nil, f.handler.cfg, nil)
			t.Cleanup(billing.Stop)
			f.handler.billingCacheService = billing
			rec := f.serveResponses(t, sharedLockedResponsesBody(false))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, 2, u.calls)
			require.Len(t, f.ledger.commands, 1)
			require.Len(t, f.logs.rows, 1)
			if mode == "sub-to-balance" {
				require.Nil(t, f.logs.rows[0].SubscriptionID)
				require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
			} else {
				require.Equal(t, subs[b.ID].ID, *f.logs.rows[0].SubscriptionID)
				require.Zero(t, f.ledger.commands[0].BalanceCost)
			}
			require.Zero(t, funds.count(), "no handler/worker reservation remains")
		})
	}
}

func TestSharedResponsesNormalGroupDoesNotReplayAfterOutput(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	rec := f.serve(t, "/v1/responses", sharedLockedResponsesBody(true), func(h *GatewayHandler, c *gin.Context) {
		u.beforeFailure = func(*http.Request) {
			_, err := c.Writer.WriteString("event: response.created\ndata: {}\n\n")
			require.NoError(t, err)
			c.Writer.Flush()
		}
		h.Responses(c)
	})
	require.Contains(t, rec.Body.String(), "response.created")
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.ledger.commands)
}

func TestSharedResponsesNormalGroupReservationHeldByWorker(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
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
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 2, u.calls)
	require.Equal(t, 1, funds.count(), "A released, B held by queued billing worker")
	require.Empty(t, f.ledger.commands)
	close(release)
	released = true
	pool.Stop()
	require.Zero(t, funds.count(), "worker completion releases B")
	require.Len(t, f.ledger.commands, 1)
	require.Len(t, f.logs.rows, 1)
}

func TestSharedResponsesNormalGroupKeepsAttemptBudget(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	var budget *service.RequestAttemptBudget
	u.beforeFailure = func(req *http.Request) { budget = service.RequestAttemptBudgetFromContext(req.Context()) }
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.NotNil(t, budget)
	require.Equal(t, 2, budget.Used())
}

type responsesAccountingFunds struct{ *lockedHTTPFunds }

func (f *responsesAccountingFunds) UpdateSubscriptionUsage(context.Context, int64, int64, float64) error {
	return nil
}

func TestSharedResponsesNormalGroupSwitchPermitAndRevoke(t *testing.T) {
	t.Run("max1 uses pending permit once", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Equal(t, 2, u.calls)
		require.Len(t, f.ledger.commands, 1)
	})
	t.Run("B fails no fresh switch budget", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		f.upstream.failFirstIDs[f.fallback.ID] = true
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 2, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("B model revoked", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"other"}} }
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.Equal(t, 404, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("B normal binding revoked", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { f.key.Groups = f.key.Groups[:1] }
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.Equal(t, 403, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("cancel after A", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		rec := f.serve(t, "/v1/responses", sharedLockedResponsesBody(false), func(h *GatewayHandler, c *gin.Context) {
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			u.beforeFailure = func(*http.Request) { cancel() }
			h.Responses(c)
			require.Equal(t, statusClientClosedRequest, c.Writer.Status())
		})
		require.Empty(t, rec.Body.String(), "canceled requests do not commit a response body")
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedResponsesNormalGroupEmptyPoolsConsumeSwitchPermits(t *testing.T) {
	t.Run("A empty max0", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 0
		f.account.Schedulable = false
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Zero(t, u.calls)
	})
	t.Run("A fail B empty max1", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		c := *b
		c.ID = 7800
		u.groups.groups[c.ID] = &c
		third := *f.fallback
		third.ID = 7810
		third.AccountGroups = []service.AccountGroup{{AccountID: third.ID, GroupID: c.ID}}
		u.cache.accounts = append(u.cache.accounts, &third)
		f.key.Groups = append(f.key.Groups, service.APIKeyGroupLink{GroupID: c.ID, Role: service.APIKeyGroupRoleNormal, Position: 2, Group: &c})
		f.upstream.onFirst = func() { f.fallback.Schedulable = false }
		rec := f.serveResponses(t, sharedLockedResponsesBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 1, u.calls, "A->B spends pending permit; empty B cannot reach C with max1")
		require.Empty(t, f.ledger.commands)
	})
}

func assertResponsesRouting(t *testing.T, row *service.UsageLog, requested, served int64, kind string, attempts int) {
	t.Helper()
	require.NotNil(t, row.RequestedGroupID)
	require.NotNil(t, row.ServedGroupID)
	require.NotNil(t, row.Failover)
	require.NotNil(t, row.AttemptCount)
	require.Equal(t, requested, *row.RequestedGroupID)
	require.Equal(t, served, *row.ServedGroupID)
	require.Equal(t, kind, *row.Failover)
	require.Equal(t, attempts, *row.AttemptCount)
}
func TestSharedResponsesRoutingUsageFacts(t *testing.T) {
	for _, mode := range []string{"none", "account", "group"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newResponsesChainFixture(t)
			expectedGroup, attempts := b.ID, 2
			if mode != "group" {
				f.key.Groups = f.key.Groups[:1]
				f.fallback.AccountGroups = []service.AccountGroup{{AccountID: f.fallback.ID, GroupID: f.group.ID}}
				expectedGroup = f.group.ID
			}
			if mode == "none" {
				f.upstream.failFirstIDs = map[int64]bool{}
				attempts = 1
			}
			rec := f.serveResponses(t, sharedLockedResponsesBody(false))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, attempts, u.calls)
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, expectedGroup, mode, attempts)
		})
	}
}
func TestSharedResponsesRoutingUsageSnapshotBeforeWorker(t *testing.T) {
	f, b, u := newResponsesChainFixture(t)
	pool := newUsageRecordTestPool(t)
	f.handler.usageRecordWorkerPool = pool
	entered, release := make(chan struct{}), make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	var budget *service.RequestAttemptBudget
	u.beforeFailure = func(req *http.Request) { budget = service.RequestAttemptBudgetFromContext(req.Context()) }
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Empty(t, f.logs.rows)
	// Mutation after handler return must not change the already captured C6 facts.
	require.NoError(t, budget.AllowSend())
	*f.key.GroupID = 999
	close(release)
	released = true
	pool.Stop()
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 2)
}

type responsesHiddenRetryUpstream struct {
	service.HTTPUpstream
	delegate *messagesAntigravityUpstream
	calls    int
}

func (u *responsesHiddenRetryUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.calls++
	if u.calls == 1 {
		return &http.Response{StatusCode: 429, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"RATE_LIMIT_EXCEEDED","metadata":{"model":"gemini-3.8-flash-high"}},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"0.1s"}]}}`))}, nil
	}
	return u.delegate.Do(req, proxy, id, concurrency)
}
func TestSharedResponsesRoutingUsageHiddenSameAccountRetry(t *testing.T) {
	f, b, _ := newResponsesChainFixture(t)
	ag := newAntigravityThinkingFixture(t, service.PlatformAnthropic)
	// Keep the existing shared billing/log fixture, but route its sole candidate to
	// a real Antigravity OAuth forwarder whose internal retry does two HTTP sends.
	*f.account = *ag.account
	f.account.AccountGroups = []service.AccountGroup{{AccountID: f.account.ID, GroupID: f.group.ID}}
	f.group.ModelPricing = ag.group.ModelPricing
	f.key.Groups = f.key.Groups[:1]
	_ = b
	upstream := &responsesHiddenRetryUpstream{delegate: ag.upstream}
	settings := service.NewSettingService(&lockedRegressionSettingRepo{}, &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1 << 20}})
	f.handler.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil, service.NewAntigravityTokenProvider(nil, nil, nil), nil, upstream, settings, nil)
	f.handler.maxAccountSwitches = 0 // success must be internal same-account retry
	rec := f.serveResponses(t, sharedLockedResponsesBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 2, upstream.calls)
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 2)
}

func TestSharedResponsesRoutingUsageScopeZeroCompatibility(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	f.key.GroupID = nil
	f.key.Group = nil
	f.key.Groups = nil
	f.account.AccountGroups = []service.AccountGroup{{AccountID: f.account.ID, GroupID: 0}}
	f.upstream.failFirstIDs = map[int64]bool{}
	f.account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4": "claude-sonnet-4"}
	rec := f.serveResponses(t, strings.ReplaceAll(sharedLockedResponsesBody(false), "client-model", "claude-sonnet-4"))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 1, u.calls)
	require.Len(t, f.logs.rows, 1)
	require.Len(t, f.ledger.commands, 1)
	row := f.logs.rows[0]
	require.Nil(t, row.RequestedGroupID)
	require.Nil(t, row.ServedGroupID)
	require.Nil(t, row.Failover)
	require.Nil(t, row.AttemptCount)
}
