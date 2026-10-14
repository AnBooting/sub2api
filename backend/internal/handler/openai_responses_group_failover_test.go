//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newOpenAIResponsesChainFixture(t *testing.T, configure ...func(*openAIChainHTTP)) (*lockedHTTPFixture, *service.Group) {
	f := newLockedHTTPFixture(t, false)
	b := *f.group
	b.ID = 72
	f.key.Status = service.StatusActive
	f.key.Groups = []service.APIKeyGroupLink{{GroupID: 71, Role: service.APIKeyGroupRoleNormal, Group: f.group}, {GroupID: 72, Role: service.APIKeyGroupRoleNormal, Position: 1, Group: &b}}
	f.accounts[1].AccountGroups = []service.AccountGroup{{AccountID: 82, GroupID: 72}}
	groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{71: f.group, 72: &b}}
	cfg := f.cfg
	cache := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{71: service.PlatformOpenAI, 72: service.PlatformOpenAI}}, nil, nil, nil, nil)
	billing := service.NewBillingService(cfg, nil)
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{*f.accounts[0], *f.accounts[1]}}
	transport := &openAIChainHTTP{delegate: f.upstream}
	for _, fn := range configure {
		fn(transport)
	}
	f.handler.gatewayService = service.NewOpenAIGatewayService(repo, f.logs, f.ledger, nil, nil, f.rates, nil, cfg, service.NewSchedulerSnapshotService(&groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: f.accounts}}, nil, nil, nil, cfg), service.NewConcurrencyService(openAIChainSlots(t, f)), billing, nil, cache, transport, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channels, billing), channels, nil, nil, nil)
	f.handler.billingCacheService = cache
	f.handler.apiKeyService = service.NewAPIKeyService(&responsesChainKeyRepo{key: f.key}, &responsesChainUserRepo{user: f.key.User}, groups, nil, nil, nil, cfg)
	f.upstream.failFirst = true
	return f, &b
}
func TestOpenAIResponsesNormalGroupChain(t *testing.T) {
	f, b := newOpenAIResponsesChainFixture(t)
	rec, _ := f.request(t, "client-model", nil)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.calls, 2)
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], 71, b.ID, service.RoutingFailoverGroup, 2)
	require.Equal(t, int64(71), *f.key.GroupID)
}
func TestOpenAIResponsesNormalGroupMaxZero(t *testing.T) {
	f, _ := newOpenAIResponsesChainFixture(t)
	f.handler.maxAccountSwitches = 0
	rec, _ := f.request(t, "client-model", nil)
	require.NotEqual(t, 200, rec.Code)
	require.Len(t, f.upstream.calls, 1)
	require.Empty(t, f.ledger.commands)
}

// The legacy fixture assumes requested==served; this transport checks B's frozen scope.
type openAIChainHTTP struct {
	service.HTTPUpstream
	delegate    *lockedHTTPUpstream
	status      int
	partial     bool
	outputError bool
}

func (u *openAIChainHTTP) Do(req *http.Request, proxy string, id int64, n int) (*http.Response, error) {
	if id == 81 {
		response, e := u.delegate.Do(req, proxy, id, n)
		if e != nil {
			return nil, e
		}
		if u.status != 0 {
			response.StatusCode = u.status
		}
		if u.outputError {
			response.StatusCode = 200
			response.Header.Set("Content-Type", "text/event-stream")
			response.Body = io.NopCloser(strings.NewReader("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n"))
		}
		return response, nil
	}
	body, e := io.ReadAll(req.Body)
	if e != nil {
		return nil, e
	}
	bundle := service.GatewayRequestBillingFromContext(req.Context())
	up, e := bundle.UpstreamModelFor(72, id)
	if e != nil {
		return nil, e
	}
	scope, e := bundle.ScopeFor(72, up, "")
	if e != nil {
		return nil, e
	}
	quote, e := bundle.PricingFor(scope)
	if e != nil {
		return nil, e
	}
	model := gjson.GetBytes(body, "model").String()
	u.delegate.calls = append(u.delegate.calls, lockedHTTPCall{model: model, id: bundle.RequestID(), version: quote.Version(), accountID: id, startedAt: bundle.StartedAt(), header: req.Header.Clone()})
	payload := fmt.Sprintf(`{"id":"resp_B","object":"response","status":"completed","model":%q,"output":[],"usage":{"input_tokens":1000,"output_tokens":500}}`, model)
	ct := "application/json"
	if gjson.GetBytes(body, "stream").Bool() {
		ct = "text/event-stream"
		payload = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + payload + "}\n\ndata: [DONE]\n\n"
	}
	var resultBody io.ReadCloser = io.NopCloser(strings.NewReader(payload))
	if u.partial {
		payload = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\nevent: response.in_progress\ndata: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_partial_B\",\"model\":\"client-model\",\"usage\":{\"input_tokens\":1000,\"output_tokens\":500}}}\n\n"
		resultBody = &nativePartialReader{Reader: strings.NewReader(payload), err: io.ErrUnexpectedEOF}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{ct}}, Body: resultBody}, nil
}
func TestOpenAIResponsesNormalGroupModes(t *testing.T) {
	for _, body := range []string{`{"model":"client-model","input":"hi","stream":true}`, lockedCompactRequestBody("client-model"), `{"model":"client-model","input":[{"role":"user","content":"hi"},{"type":"compaction_trigger"}],"stream":true}`} {
		f, b := newOpenAIResponsesChainFixture(t)
		f.upstream.onFirst = func() { b.RateMultiplier = 99; *b.ModelPricing[0].InputPrice = 99 }
		rec, _ := f.requestBody(t, body, nil)
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Len(t, f.ledger.commands, 1)
		require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
		assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
	}
}
func TestOpenAIResponsesNormalGroupRevoked(t *testing.T) {
	for _, first := range []bool{false, true} {
		f, b := newOpenAIResponsesChainFixture(t)
		if first {
			f.key.Status = "disabled"
		} else {
			f.upstream.onFirst = func() { b.Status = "disabled" }
		}
		rec, _ := f.request(t, "client-model", nil)
		require.NotEqual(t, 200, rec.Code)
		if first {
			require.Empty(t, f.upstream.calls)
		} else {
			require.Len(t, f.upstream.calls, 1)
		}
		require.Empty(t, f.ledger.commands)
	}
}
func TestOpenAIResponsesNormalGroupWorkerSnapshot(t *testing.T) {
	f, b := newOpenAIResponsesChainFixture(t)
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
	rec, c := f.request(t, "client-model", nil)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Empty(t, f.logs.rows)
	require.NoError(t, service.RequestAttemptBudgetFromContext(c.Request.Context()).AllowSend())
	c.Keys = map[string]any{}
	*f.key.GroupID = 999
	b.RateMultiplier = 99
	close(release)
	released = true
	pool.Stop()
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
	require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
}
func TestOpenAIResponsesNormalGroupSubscriptionsAndReservations(t *testing.T) {
	for _, targetSubscription := range []bool{false, true} {
		f, b := newOpenAIResponsesChainFixture(t)
		groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{71: f.group, 72: b}}
		now := time.Now()
		subs := map[int64]*service.UserSubscription{}
		g := f.group
		if targetSubscription {
			g = b
		}
		g.SubscriptionType = service.SubscriptionTypeSubscription
		subs[g.ID] = &service.UserSubscription{ID: g.ID + 100, UserID: f.key.UserID, GroupID: g.ID, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
		repo := &responsesChainSubRepo{subs: subs}
		f.handler.apiKeyService = service.NewAPIKeyService(&responsesChainKeyRepo{key: f.key}, &responsesChainUserRepo{user: f.key.User}, groups, repo, nil, nil, f.cfg)
		f.handler.subscriptionService = service.NewSubscriptionService(groups, repo, nil, nil, nil)
		t.Cleanup(f.handler.subscriptionService.Stop)
		funds := &responsesChainFunds{handlerInflightCache: newHandlerInflightCache(100), subs: subs}
		f.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
		cache := service.NewBillingCacheService(funds, nil, nil, nil, nil, nil, f.cfg, nil)
		t.Cleanup(cache.Stop)
		f.handler.billingCacheService = cache
		rec, _ := f.request(t, "client-model", subs[71])
		require.Equal(t, 200, rec.Code, rec.Body.String())
		require.Len(t, f.logs.rows, 1)
		if targetSubscription {
			require.Equal(t, subs[72].ID, *f.logs.rows[0].SubscriptionID)
			require.Zero(t, f.ledger.commands[0].BalanceCost)
		} else {
			require.Nil(t, f.logs.rows[0].SubscriptionID)
			require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
		}
		require.Zero(t, funds.count())
		assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
	}
}
func TestOpenAIResponsesNormalGroupEmptyAndCrossFamily(t *testing.T) {
	f, b := newOpenAIResponsesChainFixture(t)
	b.Platform = service.PlatformGemini
	rec, _ := f.request(t, "client-model", nil)
	require.NotEqual(t, 200, rec.Code)
	require.Len(t, f.upstream.calls, 1)
	require.Empty(t, f.ledger.commands)
}
func openAIChainSlots(t *testing.T, f *lockedHTTPFixture) *nativePartialSlots {
	s := &nativePartialSlots{}
	t.Cleanup(func() { require.Equal(t, s.acquired, s.released, "all scheduler account slots released") })
	user := &messagesUserSlotCache{}
	f.handler.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(user), SSEPingFormatClaude, 0)
	t.Cleanup(func() { require.Equal(t, user.acquired, user.released, "user slot released") })
	return s
}
func TestOpenAIResponsesNormalGroupEmptyPool(t *testing.T) {
	f, _ := newOpenAIResponsesChainFixture(t)
	f.accounts[0].Schedulable = false
	rec, _ := f.request(t, "client-model", nil)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.upstream.calls, 1)
	require.Equal(t, int64(82), f.upstream.calls[0].accountID)
	assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 1)
}
func TestOpenAIResponsesNormalGroupNoReplay(t *testing.T) {
	for _, output := range []bool{false, true} {
		f, _ := newOpenAIResponsesChainFixture(t, func(u *openAIChainHTTP) {
			if output {
				u.outputError = true
			} else {
				u.status = 400
			}
		})
		rec, _ := f.requestBody(t, `{"model":"client-model","input":"hi","stream":true}`, nil)
		require.Len(t, f.upstream.calls, 1)
		if output {
			require.Contains(t, rec.Body.String(), "visible")
		} else {
			require.NotEqual(t, 200, rec.Code)
		}
		require.Empty(t, f.ledger.commands)
	}
}

// Measured B partial output bills once without replaying the logical request.
func TestOpenAIResponsesNormalGroupPartialB(t *testing.T) {
	f, _ := newOpenAIResponsesChainFixture(t, func(u *openAIChainHTTP) { u.partial = true })
	rec, _ := f.requestBody(t, `{"model":"client-model","input":"hi","stream":true}`, nil)
	require.Contains(t, rec.Body.String(), "visible")
	require.Len(t, f.upstream.calls, 2)
	require.Len(t, f.ledger.commands, 1)
	require.Len(t, f.logs.rows, 1)
	require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
	assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
}
