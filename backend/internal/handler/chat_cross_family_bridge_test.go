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

type crossChatFunds struct {
	*responsesChainFunds
	updatedGroups []int64
}

func (f *crossChatFunds) UpdateSubscriptionUsage(_ context.Context, _ int64, g int64, _ float64) error {
	f.updatedGroups = append(f.updatedGroups, g)
	return nil
}

type crossChatAccountRepo struct {
	*openAIWSFailoverHandlerAccountRepoStub
}

func (r *crossChatAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, _ []string, _ bool) ([]service.Account, error) {
	return r.accounts, nil
}

type crossChatRPM struct {
	service.UserRPMCache
	groups []int64
}

func (r *crossChatRPM) IncrementUserGroupRPM(_ context.Context, _ int64, g int64) (int, error) {
	r.groups = append(r.groups, g)
	if g == 72 {
		return 2, nil
	}
	return 1, nil
}
func (r *crossChatRPM) IncrementUserRPM(context.Context, int64) (int, error) { return 1, nil }

func (f *crossChatFunds) DeductUserBalance(_ context.Context, _ int64, cost float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balance -= cost
	return nil
}

type crossChatHTTP struct {
	service.HTTPUpstream
	calls   []string
	mode    string
	onFirst func()
}

func (u *crossChatHTTP) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.calls = append(u.calls, fmt.Sprintf("%d:%s:%s", id, req.URL.Path, gjson.GetBytes(body, "model").String()))
	if len(u.calls) == 1 && u.onFirst != nil {
		u.onFirst()
	}
	if id == 81 && u.mode != "visible" {
		if u.mode == "business" {
			return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"bad input"}}`))}, nil
		}
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"busy"}}`))}, nil
	}
	payload := `{"id":"chat_b","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`
	ct := "application/json"
	if strings.Contains(req.URL.Path, "messages") {
		ct = "text/event-stream"
		payload = "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_b","model":"claude-sonnet-4","role":"assistant","type":"message","content":[],"usage":{"input_tokens":1000}}}` + "\n\nevent: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\nevent: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\nevent: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":500}}` + "\n\nevent: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
	} else if gjson.GetBytes(body, "stream").Bool() {
		ct = "text/event-stream"
		payload = "data: " + `{"id":"chat_b","object":"chat.completion.chunk","model":"gpt-5.4","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500}}` + "\n\ndata: [DONE]\n\n"
	}
	var reader io.ReadCloser = io.NopCloser(strings.NewReader(payload))
	if u.mode == "partial" || u.mode == "visible" {
		payload = strings.ReplaceAll(payload, "data: [DONE]\n\n", "")
		payload = strings.ReplaceAll(payload, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", "")
		reader = &nativePartialReader{Reader: strings.NewReader(payload), err: io.ErrUnexpectedEOF}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{ct}}, Body: reader}, nil
}
func TestChatCrossFamilyBidirectional(t *testing.T) { runChatCrossFamilyMatrix(t, "success") }
func TestChatCrossFamilyFailures(t *testing.T) {
	for _, mode := range []string{"business", "visible", "partial", "max0", "revoked"} {
		t.Run(mode, func(t *testing.T) { runChatCrossFamilyMatrix(t, mode) })
	}
}
func TestChatCrossFamilyResourceOwnership(t *testing.T) {
	for _, mode := range []string{"source-sub", "target-sub", "rpm", "profit", "queued"} {
		t.Run(mode, func(t *testing.T) { runChatCrossFamilyMatrix(t, mode) })
	}
}
func runChatCrossFamilyMatrix(t *testing.T, mode string) {
	for _, reverse := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			name := fmt.Sprint(reverse, stream)
			if mode == "partial" && reverse && !stream {
				name = "known_raw_json_read_error_zero_unmeasured_usage"
			}
			t.Run(name, func(t *testing.T) {
				f, b, _ := newChatChainFixture(t)
				aGroup, zGroup := f.group, b
				if reverse {
					aGroup.Platform = service.PlatformAnthropic
				} else {
					zGroup.Platform = service.PlatformAnthropic
				}
				for i, a := range f.accounts {
					a.Platform = []string{aGroup.Platform, zGroup.Platform}[i]
					model := "gpt-5.4"
					if a.Platform == service.PlatformAnthropic {
						model = "claude-sonnet-4"
					}
					a.Credentials["model_mapping"] = map[string]any{"client-model": model}
					a.Extra["openai_responses_supported"] = false
				}
				for _, g := range []*service.Group{aGroup, zGroup} {
					g.ModelPricing[0].Models = []string{"client-model", "gpt-5.4", "claude-sonnet-4"}
				}
				groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{71: aGroup, 72: zGroup}}
				cache := &groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: f.accounts}}
				channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{71: aGroup.Platform, 72: zGroup.Platform}}, nil, nil, nil, nil)
				billing := service.NewBillingService(f.cfg, nil)
				subs := map[int64]*service.UserSubscription{}
				if mode == "source-sub" || mode == "target-sub" {
					g := aGroup
					if mode == "target-sub" {
						g = zGroup
					}
					g.SubscriptionType = service.SubscriptionTypeSubscription
					now := time.Now()
					subs[g.ID] = &service.UserSubscription{ID: g.ID + 100, UserID: f.key.UserID, GroupID: g.ID, Status: service.SubscriptionStatusActive, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}
				}
				subRepo := &responsesChainSubRepo{subs: subs}
				if mode == "rpm" {
					zGroup.RPMLimit = 1
				}
				if mode == "profit" {
					zGroup.Hydrated = true
					zGroup.ProfitControlEnabled = true
					rate := 1.0
					f.accounts[1].RateMultiplier = &rate
					zGroup.ProfitMinMargin = 0.99
				}
				reservationFunds := &responsesChainFunds{handlerInflightCache: newHandlerInflightCache(100), subs: subs}
				accountingFunds := &crossChatFunds{responsesChainFunds: reservationFunds}
				rpm := &crossChatRPM{}
				var rpmCache service.UserRPMCache
				if mode == "rpm" {
					rpmCache = rpm
				}
				f.cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
				funds := service.NewBillingCacheService(accountingFunds, nil, nil, nil, rpmCache, nil, f.cfg, nil)
				t.Cleanup(func() { require.Zero(t, reservationFunds.count()) })
				f.handler.billingCacheService = funds
				t.Cleanup(funds.Stop)
				up := &crossChatHTTP{mode: mode}
				up.onFirst = func() {
					if mode == "success" {
						zGroup.RateMultiplier = 99
					}
					if mode == "revoked" {
						zGroup.Status = "disabled"
					}
				}
				repo := &crossChatAccountRepo{&openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{*f.accounts[0], *f.accounts[1]}}}
				snap := service.NewSchedulerSnapshotService(cache, nil, nil, nil, f.cfg)
				concurrency := service.NewConcurrencyService(openAIChainSlots(t, f))
				shared := service.NewGatewayService(repo, groups, f.logs, f.ledger, nil, nil, f.rates, nil, f.cfg, snap, concurrency, billing, nil, funds, nil, up, service.NewDeferredService(nil, nil, 0), nil, nil, nil, nil, nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
				openAI := service.NewOpenAIGatewayService(repo, f.logs, f.ledger, nil, nil, f.rates, nil, f.cfg, snap, concurrency, billing, nil, funds, up, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channels, billing), channels, nil, nil, nil)
				f.handler.gatewayService = openAI
				f.handler.sharedGatewayService = shared
				f.handler.apiKeyService = service.NewAPIKeyService(&responsesChainKeyRepo{key: f.key}, &responsesChainUserRepo{user: f.key.User}, groups, subRepo, nil, nil, f.cfg)
				f.handler.subscriptionService = service.NewSubscriptionService(groups, subRepo, nil, nil, nil)
				t.Cleanup(f.handler.subscriptionService.Stop)
				var pool *service.UsageRecordWorkerPool
				var release chan struct{}
				released := false
				if mode == "queued" {
					pool = newUsageRecordTestPool(t)
					release = make(chan struct{})
					entered := make(chan struct{})
					pool.Submit(func(context.Context) { close(entered); <-release })
					<-entered
					f.handler.usageRecordWorkerPool = pool
					defer func() {
						if !released {
							close(release)
						}
					}()
				}
				if mode == "max0" {
					f.handler.maxAccountSwitches = 0
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				body := fmt.Sprintf(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"stream":%t,"max_tokens":256}`, stream)
				c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)).WithContext(context.WithValue(context.Background(), ctxkey.Group, aGroup))
				c.Set(string(middleware.ContextKeyAPIKey), f.key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 92, Concurrency: 1})
				if reverse {
					h := &GatewayHandler{subscriptionService: f.handler.subscriptionService, usageRecordWorkerPool: pool, gatewayService: shared, openAIGatewayService: openAI, billingCacheService: funds, apiKeyService: f.handler.apiKeyService, concurrencyHelper: f.handler.concurrencyHelper, cfg: f.cfg, maxAccountSwitches: 5}
					if mode == "max0" {
						h.maxAccountSwitches = 0
					}
					h.ChatCompletions(c)
				} else {
					f.handler.ChatCompletions(c)
				}
				if mode == "queued" {
					require.Empty(t, f.logs.rows)
					require.NoError(t, service.RequestAttemptBudgetFromContext(c.Request.Context()).AllowSend())
					c.Keys = map[string]any{}
					*f.key.GroupID = 999
					zGroup.RateMultiplier = 99
					*zGroup.ModelPricing[0].InputPrice = 99
					close(release)
					released = true
					pool.Stop()
				}
				if mode == "rpm" || mode == "profit" {
					require.Len(t, up.calls, 1)
					require.Empty(t, f.ledger.commands)
					require.Empty(t, f.logs.rows)
					require.NotEqual(t, 200, rec.Code)
					if mode == "rpm" {
						require.Equal(t, 429, rec.Code)
						require.Contains(t, rpm.groups, int64(72))
					}
					return
				}
				if mode == "business" || mode == "max0" || mode == "revoked" {
					require.Len(t, up.calls, 1)
					require.Empty(t, f.ledger.commands)
					require.NotEqual(t, 200, rec.Code)
					return
				}
				if mode == "visible" {
					require.Len(t, up.calls, 1)
					return
				}
				if mode == "partial" && reverse && !stream {
					require.Equal(t, 502, rec.Code)
					require.Len(t, up.calls, 2)
					require.Empty(t, f.ledger.commands)
					return
				} // Raw JSON read failed before producer obtained measured usage.
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Len(t, up.calls, 2, up.calls)
				require.Len(t, f.ledger.commands, 1)
				require.Len(t, f.logs.rows, 1)
				assertResponsesRouting(t, f.logs.rows[0], 71, 72, service.RoutingFailoverGroup, 2)
				if mode == "target-sub" {
					require.Zero(t, f.ledger.commands[0].BalanceCost)
					require.Equal(t, subs[72].ID, *f.logs.rows[0].SubscriptionID)
					funds.Stop()
					require.Equal(t, []int64{72}, accountingFunds.updatedGroups)
				} else {
					require.Nil(t, f.logs.rows[0].SubscriptionID)
					require.InDelta(t, 0.0024, f.ledger.commands[0].BalanceCost, 1e-12)
				}
				if reverse {
					require.Contains(t, up.calls[0], "messages:claude-sonnet-4")
					require.Contains(t, up.calls[1], "chat/completions:gpt-5.4")
				} else {
					require.Contains(t, up.calls[0], "chat/completions:gpt-5.4")
					require.Contains(t, up.calls[1], "messages:claude-sonnet-4")
				}
			})
		}
	}
}
