//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSharedCCNormalGroupFailover(t *testing.T) {

	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			f, b, u := newResponsesChainFixture(t)
			// Change live B pricing after the bundle froze, while A is failing.
			f.upstream.onFirst = func() { b.RateMultiplier = 99 }
			rec := f.serve(t, "/v1/chat/completions", sharedLockedChatBody(stream), func(h *GatewayHandler, c *gin.Context) {
				h.ChatCompletions(c)
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

func TestSharedCCNormalGroupFailoverMaxZero(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	f.handler.maxAccountSwitches = 0
	rec := f.serveChat(t, sharedLockedChatBody(false))
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.ledger.commands)
}

func TestSharedCCNormalGroupFailoverRejectsBusinessAndMissingPrice(t *testing.T) {
	t.Run("business", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		u.status = http.StatusBadRequest
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("missing price", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		b.ModelPricing = nil
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "pricing_unavailable")
		require.Equal(t, 1, u.calls, "B missing price must reject before B forwarding")
		require.Empty(t, f.ledger.commands)
	})
	t.Run("revoked B", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.Status = "disabled" }
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.NotEqual(t, http.StatusOK, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("wrong forwarding family", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.Platform = service.PlatformOpenAI }
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedCCNormalGroupSubscriptionIdentity(t *testing.T) {
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
			rec := f.serveChat(t, sharedLockedChatBody(false))
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

func TestSharedCCNormalGroupDoesNotReplayAfterOutput(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	rec := f.serve(t, "/v1/chat/completions", sharedLockedChatBody(true), func(h *GatewayHandler, c *gin.Context) {
		u.beforeFailure = func(*http.Request) {
			_, err := c.Writer.WriteString("event: response.created\ndata: {}\n\n")
			require.NoError(t, err)
			c.Writer.Flush()
		}
		h.ChatCompletions(c)
	})
	require.Contains(t, rec.Body.String(), "response.created")
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.ledger.commands)
}

func TestSharedCCNormalGroupReservationHeldByWorker(t *testing.T) {
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
	rec := f.serveChat(t, sharedLockedChatBody(false))
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

func TestSharedCCNormalGroupKeepsAttemptBudget(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	var budget *service.RequestAttemptBudget
	u.beforeFailure = func(req *http.Request) { budget = service.RequestAttemptBudgetFromContext(req.Context()) }
	rec := f.serveChat(t, sharedLockedChatBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.NotNil(t, budget)
	require.Equal(t, 2, budget.Used())
}

func TestSharedCCNormalGroupSwitchPermitAndRevoke(t *testing.T) {
	t.Run("max1 uses pending permit once", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Equal(t, 2, u.calls)
		require.Len(t, f.ledger.commands, 1)
	})
	t.Run("B fails no fresh switch budget", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		f.upstream.failFirstIDs[f.fallback.ID] = true
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 2, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("B model revoked", func(t *testing.T) {
		f, b, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"other"}} }
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.Equal(t, 404, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("B normal binding revoked", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.upstream.onFirst = func() { f.key.Groups = f.key.Groups[:1] }
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.Equal(t, 403, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("cancel after A", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		rec := f.serve(t, "/v1/chat/completions", sharedLockedChatBody(false), func(h *GatewayHandler, c *gin.Context) {
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			u.beforeFailure = func(*http.Request) { cancel() }
			h.ChatCompletions(c)
			require.Equal(t, statusClientClosedRequest, c.Writer.Status())
		})
		require.Empty(t, rec.Body.String(), "canceled requests do not commit a response body")
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedCCNormalGroupEmptyPoolsConsumeSwitchPermits(t *testing.T) {
	t.Run("A empty max0", func(t *testing.T) {
		f, _, u := newResponsesChainFixture(t)
		f.handler.maxAccountSwitches = 0
		f.account.Schedulable = false
		rec := f.serveChat(t, sharedLockedChatBody(false))
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
		rec := f.serveChat(t, sharedLockedChatBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 1, u.calls, "A->B spends pending permit; empty B cannot reach C with max1")
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedCCRoutingUsageFacts(t *testing.T) {
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
			rec := f.serveChat(t, sharedLockedChatBody(false))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, attempts, u.calls)
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, expectedGroup, mode, attempts)
		})
	}
}

func TestSharedCCRoutingUsageSnapshotBeforeWorker(t *testing.T) {
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
	rec := f.serveChat(t, sharedLockedChatBody(false))
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

func TestSharedCCRoutingUsageHiddenSameAccountRetry(t *testing.T) {
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
	rec := f.serveChat(t, sharedLockedChatBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 2, upstream.calls)
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 2)
}

func TestSharedCCGroupExhaustsAccountsBeforeNextGroup(t *testing.T) {
	f, b, u := newResponsesChainFixture(t)
	a2 := *f.account
	a2.ID = 7612
	a2.Priority = 2
	a2.AccountGroups = []service.AccountGroup{{AccountID: a2.ID, GroupID: f.group.ID}}
	u.cache.accounts = append(u.cache.accounts, &a2)
	f.upstream.failFirstIDs[a2.ID] = true
	f.handler.maxAccountSwitches = 2
	rec := f.serveChat(t, sharedLockedChatBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, []string{"7610:claude-sonnet-4", "7612:claude-sonnet-4", "7611:claude-sonnet-4"}, f.upstream.callList())
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 3)
}

func TestSharedCCCrossGroupGeminiLimitAndSuccess(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, limit := range []int{0, 1} {
			t.Run(fmt.Sprintf("stream=%t/targetLimit=%d", stream, limit), func(t *testing.T) {
				f, b, u := newResponsesChainFixture(t)
				b.Platform = service.PlatformGemini
				b.ModelPricing[0].Models = append(b.ModelPricing[0].Models, "gemini-2.5-flash")
				f.fallback.Platform = service.PlatformGemini
				f.fallback.Credentials = map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}}
				geminiUpstream := &ccGeminiUpstream{geminiLockedUpstream: &geminiLockedUpstream{failFirstIDs: map[int64]bool{}}}
				f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, geminiUpstream, nil, f.handler.cfg)
				f.handler.maxAccountSwitches = 3
				f.handler.maxAccountSwitchesGemini = limit
				rec := f.serveChat(t, sharedLockedChatBody(stream))
				require.Equal(t, 1, u.calls, "Anthropic A sends once")
				if limit == 0 {
					require.NotEqual(t, 200, rec.Code)
					require.Empty(t, geminiUpstream.callList())
					require.Empty(t, f.ledger.commands)
					return
				}
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Len(t, geminiUpstream.callList(), 1)
				require.Contains(t, geminiUpstream.callList()[0], "gemini-2.5-flash")
				require.Len(t, f.logs.rows, 1)
				require.Len(t, f.ledger.commands, 1)
				require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
				assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 2)
			})
		}
	}
}
func TestSharedCCRoutingUsageScopeZeroCompatibility(t *testing.T) {
	f, _, u := newResponsesChainFixture(t)
	f.key.GroupID = nil
	f.key.Group = nil
	f.key.Groups = nil
	f.account.AccountGroups = []service.AccountGroup{{AccountID: f.account.ID, GroupID: 0}}
	f.upstream.failFirstIDs = map[int64]bool{}
	f.account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4": "claude-sonnet-4"}
	rec := f.serveChat(t, strings.ReplaceAll(sharedLockedChatBody(false), "client-model", "claude-sonnet-4"))
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

type ccGeminiUpstream struct {
	*geminiLockedUpstream
	failStatus int
}

func (u *ccGeminiUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	if u.failStatus != 0 {
		u.mu.Lock()
		u.calls = append(u.calls, fmt.Sprintf("%d:%s", id, req.URL.Path))
		u.mu.Unlock()
		return &http.Response{StatusCode: u.failStatus, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}
	resp, err := u.geminiLockedUpstream.Do(req, proxy, id, concurrency)
	if err == nil && strings.Contains(req.URL.Path, "streamGenerateContent") {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(strings.NewReader("data: " + string(body) + "\n\n"))
		resp.Header.Set("Content-Type", "text/event-stream")
	}
	return resp, err
}
func TestSharedCCGeminiFirstToAnthropic(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			f, b, u := newResponsesChainFixture(t)
			f.group.Platform = service.PlatformGemini
			f.group.ModelPricing[0].Models = append(f.group.ModelPricing[0].Models, "gemini-2.5-flash")
			f.account.Platform = service.PlatformGemini
			f.account.Credentials = map[string]any{"api_key": "fixture-not-a-secret", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}}
			geminiUpstream := &ccGeminiUpstream{geminiLockedUpstream: &geminiLockedUpstream{}, failStatus: 429}
			f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, geminiUpstream, nil, f.handler.cfg)
			f.handler.maxAccountSwitchesGemini = 1
			f.handler.maxAccountSwitches = 3
			rec := f.serveChat(t, sharedLockedChatBody(stream))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Len(t, geminiUpstream.callList(), 5, "Gemini service performs five same-account sends before group failover")
			require.Equal(t, 1, u.calls)
			require.Len(t, f.ledger.commands, 1)
			require.Len(t, f.logs.rows, 1)
			require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 6)
		})
	}
}

type ccGeminiAccountRepo struct{ service.AccountRepository }

func (*ccGeminiAccountRepo) SetRateLimited(context.Context, int64, time.Time) error { return nil }
