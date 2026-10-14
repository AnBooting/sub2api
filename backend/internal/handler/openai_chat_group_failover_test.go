//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
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

type chatChainHTTP struct {
	service.HTTPUpstream
	calls           []int64
	models          []string
	status          int
	partial         bool
	output          bool
	onFirst         func()
	mutateAccountID func()
}

func (u *chatChainHTTP) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.calls = append(u.calls, id)
	u.models = append(u.models, gjson.GetBytes(body, "model").String())
	if len(u.calls) == 1 && u.onFirst != nil {
		u.onFirst()
	}
	status := 200
	payload := ""
	if id == 81 && !u.output {
		status = 503
		if u.status != 0 {
			status = u.status
		}
		payload = `{"error":{"message":"fixture failure","type":"invalid_request_error"}}`
	} else {
		payload = fmt.Sprintf(`{"id":"chat_B","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"visible"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`, u.models[len(u.models)-1])
	}
	ct := "application/json"
	if status == 200 && gjson.GetBytes(body, "stream").Bool() {
		ct = "text/event-stream"
		payload = fmt.Sprintf(`data: {"id":"chat_B","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":"visible"},"finish_reason":null}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`, u.models[len(u.models)-1]) + "\n\n"
		if !u.partial && !u.output {
			payload += "data: [DONE]\n\n"
		}
	}
	var responseBody io.ReadCloser = io.NopCloser(strings.NewReader(payload))
	if status == 200 && (u.partial || u.output) {
		responseBody = &nativePartialReader{Reader: strings.NewReader(payload), err: io.ErrUnexpectedEOF}
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{ct}}, Body: responseBody}, nil
}
func newChatChainFixture(t *testing.T) (*lockedHTTPFixture, *service.Group, *chatChainHTTP) {
	f, b := newOpenAIResponsesChainFixture(t)
	up := &chatChainHTTP{}
	for _, a := range f.accounts {
		a.Extra["openai_passthrough"] = false
		a.Extra["openai_responses_supported"] = false
		a.Extra["upstream_api_protocol"] = "chat_completions"
	}
	cfg := f.cfg
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{71: service.PlatformOpenAI, 72: service.PlatformOpenAI}}, nil, nil, nil, nil)
	billing := service.NewBillingService(cfg, nil)
	cache := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{*f.accounts[0], *f.accounts[1]}}
	up.mutateAccountID = func() { f.accounts[1].ID = 83; repo.accounts[1].ID = 83 }
	f.handler.gatewayService = service.NewOpenAIGatewayService(repo, f.logs, f.ledger, nil, nil, f.rates, nil, cfg, service.NewSchedulerSnapshotService(&groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: f.accounts}}, nil, nil, nil, cfg), service.NewConcurrencyService(openAIChainSlots(t, f)), billing, nil, cache, up, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channels, billing), channels, nil, nil, nil)
	return f, b, up
}
func serveChatChain(t *testing.T, f *lockedHTTPFixture, stream bool) (*httptest.ResponseRecorder, *gin.Context) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := fmt.Sprintf(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":%t,"max_completion_tokens":256}`, stream)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 1})
	f.handler.ChatCompletions(c)
	return rec, c
}
func TestOpenAIChatNormalGroupChain(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f, b, u := newChatChainFixture(t)
			u.onFirst = func() { b.RateMultiplier = 99; *b.ModelPricing[0].InputPrice = 99 }
			rec, _ := serveChatChain(t, f, stream)
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, []int64{81, 82}, u.calls)
			require.Equal(t, []string{"gpt-5.4", "gpt-5.4"}, u.models)
			require.Len(t, f.ledger.commands, 1)
			require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
			assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
		})
	}
}
func TestOpenAIChatNormalGroupNoReplay(t *testing.T) {
	for _, output := range []bool{false, true} {
		f, _, u := newChatChainFixture(t)
		u.output = output
		u.status = 400
		rec, _ := serveChatChain(t, f, true)
		require.Equal(t, []int64{81}, u.calls)
		if output {
			require.Contains(t, rec.Body.String(), "visible")
		} else {
			require.Equal(t, 400, rec.Code)
		}
	}
}
func TestOpenAIChatNormalGroupPartialB(t *testing.T) {
	f, _, u := newChatChainFixture(t)
	u.partial = true
	rec, _ := serveChatChain(t, f, true)
	require.Contains(t, rec.Body.String(), "visible")
	require.Equal(t, []int64{81, 82}, u.calls)
	require.Len(t, f.ledger.commands, 1)
	require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
	assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
}
func TestOpenAIChatNormalGroupEmptyMaxAndRevoked(t *testing.T) {
	for _, mode := range []string{"empty", "max0", "first-revoked", "target-revoked", "no-price"} {
		t.Run(mode, func(t *testing.T) {
			f, b, u := newChatChainFixture(t)
			switch mode {
			case "empty":
				f.accounts[0].Schedulable = false
			case "max0":
				f.handler.maxAccountSwitches = 0
			case "first-revoked":
				f.key.Status = "disabled"
			case "target-revoked":
				u.onFirst = func() { b.Status = "disabled" }
			case "no-price":
				f.group.ModelPricing[0].InputPrice = nil
			}
			rec, _ := serveChatChain(t, f, false)
			if mode == "empty" {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, []int64{82}, u.calls)
				assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 1)
			} else {
				require.NotEqual(t, 200, rec.Code)
				require.Empty(t, f.ledger.commands)
				if mode == "first-revoked" || mode == "no-price" {
					require.Empty(t, u.calls)
				} else {
					require.Equal(t, []int64{81}, u.calls)
				}
			}
		})
	}
}
func TestOpenAIChatNormalGroupQueuedSnapshot(t *testing.T) {
	f, b, _ := newChatChainFixture(t)
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
	rec, c := serveChatChain(t, f, true)
	require.Equal(t, 200, rec.Code)
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
func TestOpenAIChatNormalGroupSubscriptionsAndReservations(t *testing.T) {
	for _, targetSubscription := range []bool{false, true} {
		f, b, _ := newChatChainFixture(t)
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
		rec, _ := serveChatChain(t, f, false)
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
func TestOpenAIChatNormalGroupBindingRejectReleasesSlots(t *testing.T) {
	f, _, u := newChatChainFixture(t)
	u.onFirst = u.mutateAccountID
	rec, _ := serveChatChain(t, f, false)
	require.Equal(t, 503, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "pricing_unavailable")
	require.Equal(t, []int64{81}, u.calls)
	require.Empty(t, f.ledger.commands)
}
