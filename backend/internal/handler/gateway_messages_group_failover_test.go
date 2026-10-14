//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
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

func messagesChainBody(stream bool) string {
	return fmt.Sprintf(`{"model":"client-model","max_tokens":256,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)
}
func TestSharedMessagesNormalGroupFailover(t *testing.T) {

	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			f, b, u := newMessagesChainFixture(t)
			// Change live B pricing after the bundle froze, while A is failing.
			f.upstream.onFirst = func() { b.RateMultiplier = 99 }
			rec := f.serve(t, "/v1/messages", messagesChainBody(stream), func(h *GatewayHandler, c *gin.Context) {
				h.Messages(c)
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

func TestSharedMessagesNormalGroupFailoverMaxZero(t *testing.T) {
	f, _, u := newMessagesChainFixture(t)
	f.handler.maxAccountSwitches = 0
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.ledger.commands)
}

func TestSharedMessagesNormalGroupFailoverRejectsBusinessAndMissingPrice(t *testing.T) {
	t.Run("business", func(t *testing.T) {
		f, _, u := newMessagesChainFixture(t)
		u.status = http.StatusBadRequest
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("missing price", func(t *testing.T) {
		f, b, u := newMessagesChainFixture(t)
		b.ModelPricing = nil
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "pricing_unavailable")
		require.Equal(t, 1, u.calls, "B missing price must reject before B forwarding")
		require.Empty(t, f.ledger.commands)
	})
	t.Run("revoked B", func(t *testing.T) {
		f, b, u := newMessagesChainFixture(t)
		f.upstream.onFirst = func() { b.Status = "disabled" }
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.NotEqual(t, http.StatusOK, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("wrong forwarding family", func(t *testing.T) {
		f, b, u := newMessagesChainFixture(t)
		f.upstream.onFirst = func() { b.Platform = service.PlatformOpenAI }
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedMessagesNormalGroupSubscriptionIdentity(t *testing.T) {
	for _, mode := range []string{"sub-to-sub", "balance-to-sub", "sub-to-balance"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newMessagesChainFixture(t)
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
			rec := serveMessagesChain(t, f, messagesChainBody(false))
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

func TestSharedMessagesNormalGroupDoesNotReplayAfterOutput(t *testing.T) {
	f, _, u := newMessagesChainFixture(t)
	rec := f.serve(t, "/v1/messages", messagesChainBody(true), func(h *GatewayHandler, c *gin.Context) {
		u.beforeFailure = func(*http.Request) {
			_, err := c.Writer.WriteString("event: response.created\ndata: {}\n\n")
			require.NoError(t, err)
			c.Writer.Flush()
		}
		h.Messages(c)
	})
	require.Contains(t, rec.Body.String(), "response.created")
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.ledger.commands)
}

func TestSharedMessagesNormalGroupReservationHeldByWorker(t *testing.T) {
	f, _, u := newMessagesChainFixture(t)
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
	rec := serveMessagesChain(t, f, messagesChainBody(false))
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

func TestSharedMessagesNormalGroupKeepsAttemptBudget(t *testing.T) {
	f, _, u := newMessagesChainFixture(t)
	var budget *service.RequestAttemptBudget
	u.beforeFailure = func(req *http.Request) { budget = service.RequestAttemptBudgetFromContext(req.Context()) }
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.NotNil(t, budget)
	require.Equal(t, 2, budget.Used())
}

func TestSharedMessagesNormalGroupSwitchPermitAndRevoke(t *testing.T) {
	t.Run("max1 uses pending permit once", func(t *testing.T) {
		f, _, u := newMessagesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Equal(t, 2, u.calls)
		require.Len(t, f.ledger.commands, 1)
	})
	t.Run("B fails no fresh switch budget", func(t *testing.T) {
		f, _, u := newMessagesChainFixture(t)
		f.handler.maxAccountSwitches = 1
		f.upstream.failFirstIDs[f.fallback.ID] = true
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 2, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("B model revoked", func(t *testing.T) {
		f, b, u := newMessagesChainFixture(t)
		f.upstream.onFirst = func() { b.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"other"}} }
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.Equal(t, 404, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("B normal binding revoked", func(t *testing.T) {
		f, _, u := newMessagesChainFixture(t)
		f.upstream.onFirst = func() { f.key.Groups = f.key.Groups[:1] }
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.Equal(t, 403, rec.Code)
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
	t.Run("cancel after A", func(t *testing.T) {
		f, _, u := newMessagesChainFixture(t)
		rec := f.serve(t, "/v1/messages", messagesChainBody(false), func(h *GatewayHandler, c *gin.Context) {
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			u.beforeFailure = func(*http.Request) { cancel() }
			h.Messages(c)
			require.Equal(t, statusClientClosedRequest, c.Writer.Status())
		})
		require.Empty(t, rec.Body.String(), "canceled requests do not commit a response body")
		require.Equal(t, 1, u.calls)
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedMessagesNormalGroupEmptyPoolsConsumeSwitchPermits(t *testing.T) {
	t.Run("A empty max0", func(t *testing.T) {
		f, _, u := newMessagesChainFixture(t)
		f.handler.maxAccountSwitches = 0
		f.account.Schedulable = false
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Zero(t, u.calls)
	})
	t.Run("A fail B empty max1", func(t *testing.T) {
		f, b, u := newMessagesChainFixture(t)
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
		rec := serveMessagesChain(t, f, messagesChainBody(false))
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 1, u.calls, "A->B spends pending permit; empty B cannot reach C with max1")
		require.Empty(t, f.ledger.commands)
	})
}

func TestSharedMessagesRoutingUsageFacts(t *testing.T) {
	for _, mode := range []string{"none", "account", "group"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newMessagesChainFixture(t)
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
			rec := serveMessagesChain(t, f, messagesChainBody(false))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, attempts, u.calls)
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, expectedGroup, mode, attempts)
		})
	}
}

func TestSharedMessagesRoutingUsageSnapshotBeforeWorker(t *testing.T) {
	f, b, u := newMessagesChainFixture(t)
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
	rec := serveMessagesChain(t, f, messagesChainBody(false))
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

func TestSharedMessagesRoutingUsageHiddenSameAccountRetry(t *testing.T) {
	f, b, _ := newMessagesChainFixture(t)
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
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 2, upstream.calls)
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 2)
}

func TestSharedMessagesRoutingUsageScopeZeroCompatibility(t *testing.T) {
	f, _, u := newMessagesChainFixture(t)
	f.key.GroupID = nil
	f.key.Group = nil
	f.key.Groups = nil
	f.account.AccountGroups = []service.AccountGroup{{AccountID: f.account.ID, GroupID: 0}}
	f.upstream.failFirstIDs = map[int64]bool{}
	f.account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4": "claude-sonnet-4"}
	rec := serveMessagesChain(t, f, strings.ReplaceAll(messagesChainBody(false), "client-model", "claude-sonnet-4"))
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

func serveMessagesChain(t *testing.T, f *sharedLockedFixture, body string) *httptest.ResponseRecorder {
	return f.serve(t, "/v1/messages", body, func(h *GatewayHandler, c *gin.Context) { h.Messages(c) })
}

func TestSharedMessagesNormalGroupAccountOrder(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f, b, u := newMessagesChainFixture(t)
			a2 := *f.account
			a2.ID = 7612
			a2.Priority = 2
			a2.AccountGroups = []service.AccountGroup{{AccountID: a2.ID, GroupID: f.group.ID}}
			u.cache.accounts = append(u.cache.accounts, &a2)
			f.upstream.failFirstIDs[a2.ID] = true
			f.handler.maxAccountSwitches = 2
			rec := serveMessagesChain(t, f, messagesChainBody(stream))
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, []string{"7610:claude-sonnet-4", "7612:claude-sonnet-4", "7611:claude-sonnet-4"}, f.upstream.callList())
			require.Len(t, f.logs.rows, 1)
			assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, 3)
		})
	}
}
func TestSharedMessagesNormalGroupClaudeCodePolicy(t *testing.T) {
	for _, mode := range []string{"ordinary-with-fallback", "restricted-nonclient", "restricted-client"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newMessagesChainFixture(t)
			outside := int64(99999)
			b.FallbackGroupID = &outside
			b.ClaudeCodeOnly = mode != "ordinary-with-fallback"
			requestBody := messagesChainBody(false)
			if mode == "restricted-client" {
				requestBody = strings.ReplaceAll(string(validClaudeCodeBodyJSON()), "claude-3-5-sonnet-20241022", "client-model")
			}
			rec := f.serve(t, "/v1/messages", requestBody, func(h *GatewayHandler, c *gin.Context) {
				if mode == "restricted-client" {
					c.Request.Header.Set("User-Agent", "claude-cli/2.0.0 (external, cli)")
					c.Request.Header.Set("X-App", "claude-code")
					c.Request.Header.Set("anthropic-beta", "message-batches-2024-09-24")
					c.Request.Header.Set("anthropic-version", "2023-06-01")
				}
				h.Messages(c)
			})
			if mode == "restricted-nonclient" {
				require.Equal(t, 403, rec.Code)
				require.Equal(t, 1, u.calls)
				require.Empty(t, f.ledger.commands)
			} else {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, 2, u.calls)
				require.Len(t, f.ledger.commands, 1)
			}
		})
	}
}

type messagesChainHTTP struct {
	service.HTTPUpstream
	delegate *responsesChainUpstream
	partial  bool
}

func (u *messagesChainHTTP) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	stream := gjson.GetBytes(body, "stream").Bool()
	resp, err := u.delegate.Do(req, proxy, id, concurrency)
	if err == nil && resp.StatusCode == 200 && stream && u.partial {
		resp.Body.Close()
		resp.Body = io.NopCloser(strings.NewReader(partialMessageStartSSE))
		return resp, nil
	}
	if err == nil && resp.StatusCode == 200 && !stream {
		resp.Body.Close()
		resp.Header.Set("Content-Type", "application/json")
		resp.Body = io.NopCloser(strings.NewReader(`{"id":"msg-chain","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1000,"output_tokens":500}}`))
	}
	return resp, err
}
func newMessagesChainFixture(t *testing.T) (*sharedLockedFixture, *service.Group, *responsesChainUpstream) {
	f, b, u := newResponsesChainFixture(t)
	cfg := f.handler.cfg
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: service.PlatformAnthropic, b.ID: service.PlatformAnthropic}}, nil, nil, nil, nil)
	accounting := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(accounting.Stop)
	settings := service.NewSettingService(&messagesChainSettingRepo{}, cfg)
	f.handler.settingService = settings
	f.handler.gatewayService = service.NewGatewayService(nil, u.groups, f.logs, f.ledger, nil, nil, nil, nil, cfg, service.NewSchedulerSnapshotService(u.cache, nil, nil, nil, cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, accounting, nil, &messagesChainHTTP{delegate: u}, service.NewDeferredService(nil, nil, 0), nil, nil, nil, nil, settings, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	return f, b, u
}

func TestSharedMessagesLegacyFallbackKeepsNullRouting(t *testing.T) {
	f, b, _ := newMessagesChainFixture(t)
	f.key.Groups = f.key.Groups[:1]
	f.group.FallbackGroupIDOnInvalidRequest = &b.ID
	ag := newAntigravityThinkingFixture(t, service.PlatformAnthropic)
	id := f.account.ID
	*f.account = *ag.account
	f.account.ID = id
	f.account.AccountGroups = []service.AccountGroup{{AccountID: id, GroupID: f.group.ID}}
	f.account.Credentials["model_mapping"] = map[string]any{"client-model": "claude-sonnet-4"}
	upstream := &messagesPromptTooLongUpstream{promptTooLong: map[int64]bool{id: true}}
	f.handler.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil, service.NewAntigravityTokenProvider(nil, nil, nil), nil, upstream, nil, nil)
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.logs.rows, 1)
	require.Len(t, f.ledger.commands, 1)
	row := f.logs.rows[0]
	require.Equal(t, b.ID, *row.GroupID)
	require.Nil(t, row.RequestedGroupID)
	require.Nil(t, row.ServedGroupID)
	require.Nil(t, row.Failover)
	require.Nil(t, row.AttemptCount)
}

func TestSharedMessagesPartialUsageSnapshots(t *testing.T) {
	f, _, u := newMessagesChainFixture(t)
	f.upstream.failFirstIDs = map[int64]bool{}
	cfg := f.handler.cfg
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{}, nil, nil, nil, nil)
	accounting := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(accounting.Stop)
	f.handler.gatewayService = service.NewGatewayService(nil, u.groups, f.logs, f.ledger, nil, nil, nil, nil, cfg, service.NewSchedulerSnapshotService(u.cache, nil, nil, nil, cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, accounting, nil, &messagesChainHTTP{delegate: u, partial: true}, service.NewDeferredService(nil, nil, 0), nil, nil, nil, nil, service.NewSettingService(&lockedRegressionSettingRepo{}, cfg), nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	rec := serveMessagesChain(t, f, messagesChainBody(true))
	require.Contains(t, rec.Body.String(), "message_start")
	require.Equal(t, 1, u.calls)
	require.Len(t, f.logs.rows, 1)
	require.Len(t, f.ledger.commands, 1)
	require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
}

type messagesChainSettingRepo struct{ lockedRegressionSettingRepo }

func (*messagesChainSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

type messagesUserSlotCache struct {
	fakeConcurrencyCache
	acquired, released int
}

func (c *messagesUserSlotCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	c.acquired++
	return true, nil
}
func (c *messagesUserSlotCache) ReleaseUserSlot(context.Context, int64, string) error {
	c.released++
	return nil
}
func TestSharedMessagesNormalChainAcquiresUserSlotOnce(t *testing.T) {
	f, _, _ := newMessagesChainFixture(t)
	cache := &messagesUserSlotCache{}
	f.handler.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0)
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 1, cache.acquired)
	require.Equal(t, 1, cache.released)
}
