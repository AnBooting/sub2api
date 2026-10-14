//go:build unit

package handler

import (
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type messagesCrossHTTP struct {
	service.HTTPUpstream
	failID   int64
	partial  bool
	calls    []int64
	paths    []string
	before   func()
	delegate *messagesChainHTTP
}

func (u *messagesCrossHTTP) Do(req *http.Request, proxy string, id int64, n int) (*http.Response, error) {
	u.calls = append(u.calls, id)
	u.paths = append(u.paths, req.URL.Path)
	if u.before != nil {
		u.before()
		u.before = nil
	}
	if id == u.failID {
		return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporary"}}`))}, nil
	}
	if strings.Contains(req.URL.Path, "generateContent") || strings.Contains(req.URL.Path, "streamGenerateContent") {
		body := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":500}}`
		ct := "application/json"
		if strings.Contains(req.URL.Path, "v1internal") {
			body = `{"response":` + body + `}`
		}
		if strings.Contains(req.URL.Path, "streamGenerateContent") {
			body = "data: " + body + "\n\n"
			ct = "text/event-stream"
		}
		var responseBody io.ReadCloser = io.NopCloser(strings.NewReader(body))
		if u.partial {
			body = strings.ReplaceAll(body, `,"finishReason":"STOP"`, "")
			responseBody = &messagesCrossBrokenStream{Reader: strings.NewReader(body)}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{ct}}, Body: responseBody}, nil
	}
	return u.delegate.Do(req, proxy, id, n)
}
func messagesCrossFixture(t *testing.T, from, to string) (*sharedLockedFixture, *service.Group, *messagesCrossHTTP) {
	f, b, chain := newMessagesChainFixture(t)
	u := &messagesCrossHTTP{failID: f.account.ID, delegate: &messagesChainHTTP{delegate: chain}}
	f.upstream.failFirstIDs = map[int64]bool{}
	for i, g := range []*service.Group{f.group, b} {
		platform := from
		a := f.account
		if i == 1 {
			platform = to
			a = f.fallback
		}
		g.Platform = platform
		a.Platform = platform
		g.ModelPricing[0].Models = append(g.ModelPricing[0].Models, "gemini-2.5-flash")
		if platform == service.PlatformAntigravity {
			a.Type = service.AccountTypeOAuth
			a.Credentials = map[string]any{"access_token": "fixture", "project_id": "fixture-project", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}}
		}
		if platform == service.PlatformGemini {
			a.Credentials = map[string]any{"api_key": "fixture", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}}
		}
	}
	cfg := f.handler.cfg
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: from, b.ID: to}}, nil, nil, nil, nil)
	accounting := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(accounting.Stop)
	f.handler.gatewayService = service.NewGatewayService(nil, chain.groups, f.logs, f.ledger, nil, nil, nil, nil, cfg, service.NewSchedulerSnapshotService(&nativePlatformCache{groupScopedSchedulerCache: chain.cache}, nil, nil, nil, cfg), service.NewConcurrencyService(&fakeConcurrencyCache{}), billing, nil, accounting, nil, u, service.NewDeferredService(nil, nil, 0), nil, nil, nil, nil, service.NewSettingService(&messagesChainSettingRepo{}, cfg), nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, u, nil, cfg)
	f.handler.antigravityGatewayService = service.NewAntigravityGatewayService(nil, nil, nil, service.NewAntigravityTokenProvider(nil, nil, nil), nil, u, service.NewSettingService(&messagesChainSettingRepo{}, cfg), nil)
	f.handler.maxAccountSwitchesGemini = 2
	return f, b, u
}
func TestMessagesCrossFamilyBidirectional(t *testing.T) {
	for _, from := range []string{service.PlatformAnthropic, service.PlatformGemini} {
		to := service.PlatformGemini
		if from == to {
			to = service.PlatformAnthropic
		}
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprint(from, stream), func(t *testing.T) {
				f, b, u := messagesCrossFixture(t, from, to)
				rec := serveMessagesChain(t, f, messagesChainBody(stream))
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Equal(t, f.fallback.ID, u.calls[len(u.calls)-1])
				if to == service.PlatformGemini {
					require.Contains(t, u.paths[len(u.paths)-1], "gemini-2.5-flash")
				} else {
					require.Contains(t, u.paths[len(u.paths)-1], "messages")
				}
				require.Len(t, f.ledger.commands, 1)
				require.Len(t, f.logs.rows, 1)
				assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, len(u.calls))
				require.InDelta(t, 0.006, f.ledger.commands[0].BalanceCost, 1e-12)
			})
		}
	}
}
func TestMessagesCrossFamilyTargetSmallerLimit(t *testing.T) {
	f, _, u := messagesCrossFixture(t, service.PlatformAnthropic, service.PlatformGemini)
	f.handler.maxAccountSwitchesGemini = 0
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.NotEqual(t, 200, rec.Code)
	for _, id := range u.calls {
		require.Equal(t, f.account.ID, id)
	}
	require.Empty(t, f.ledger.commands)
}
func TestMessagesCrossFamilyUnusedTailDoesNotLimitFirstGroup(t *testing.T) {
	f, _, u := messagesCrossFixture(t, service.PlatformAnthropic, service.PlatformGemini)
	u.failID = 0
	f.handler.maxAccountSwitchesGemini = 0
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, []int64{f.account.ID}, u.calls)
}
func TestMessagesCrossFamilyAGBidirectional(t *testing.T) {
	for _, from := range []string{service.PlatformAntigravity, service.PlatformGemini} {
		to := service.PlatformGemini
		if from == to {
			to = service.PlatformAntigravity
		}
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprint(from, stream), func(t *testing.T) {
				f, b, u := messagesCrossFixture(t, from, to)
				rec := serveMessagesChain(t, f, messagesChainBody(stream))
				require.Equal(t, 200, rec.Code, rec.Body.String())
				require.Len(t, f.ledger.commands, 1)
				require.Len(t, f.logs.rows, 1)
				assertResponsesRouting(t, f.logs.rows[0], f.group.ID, b.ID, service.RoutingFailoverGroup, len(u.calls))
			})
		}
	}
}
func TestMessagesCrossFamilyTargetRevoked(t *testing.T) {
	f, b, u := messagesCrossFixture(t, service.PlatformAnthropic, service.PlatformGemini)
	u.before = func() { b.Status = "disabled" }
	rec := serveMessagesChain(t, f, messagesChainBody(false))
	require.NotEqual(t, 200, rec.Code)
	for _, id := range u.calls {
		require.Equal(t, f.account.ID, id)
	}
	require.Empty(t, f.ledger.commands)
}

type messagesCrossBrokenStream struct{ *strings.Reader }

func (r *messagesCrossBrokenStream) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, errors.New("fixture stream disconnected")
	}
	return n, err
}
func (r *messagesCrossBrokenStream) Close() error { return nil }

// A measured partial Gemini stream must bill once and never replay into B.
func TestMessagesCrossFamilyGeminiPartialUsageStopsChain(t *testing.T) {
	f, _, u := messagesCrossFixture(t, service.PlatformGemini, service.PlatformAnthropic)
	u.failID = 0
	u.partial = true
	rec := serveMessagesChain(t, f, messagesChainBody(true))
	require.Contains(t, rec.Body.String(), "message_start")
	require.Equal(t, []int64{f.account.ID}, u.calls)
	require.Len(t, f.ledger.commands, 1)
	require.Greater(t, f.ledger.commands[0].BalanceCost, 0.0)
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
}
