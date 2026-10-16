//go:build unit

package handler

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type geminiImageHTTP struct {
	service.HTTPUpstream
	calls  int
	mutate func()
	paths  []string
	failID int64
}

func (u *geminiImageHTTP) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.calls++
	if accountID == u.failID {
		return &http.Response{StatusCode: 503, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"unavailable"}}`))}, nil
	}
	u.paths = append(u.paths, req.URL.Path)
	if u.mutate != nil {
		u.mutate()
	}
	body := `{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}`
	ct := "application/json"
	if strings.Contains(req.URL.Path, "streamGenerateContent") {
		ct = "text/event-stream"
		body = "data: " + body + "\n\n"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{ct}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func geminiImageFixture(t *testing.T) (*sharedLockedFixture, *geminiImageHTTP) {
	f, _, _ := newNativeChainFixture(t)
	f.key.Groups = f.key.Groups[:1]
	price := 0.25
	f.group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"client-model", "gemini-2.5-flash"}, BillingMode: service.BillingModeImage, PerRequestPrice: &price}}
	f.group.ImageRateIndependent = true
	f.group.ImageRateMultiplier = 3
	u := &geminiImageHTTP{}
	f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, u, nil, f.handler.cfg)
	return f, u
}
func serveGeminiImage(t *testing.T, f *sharedLockedFixture, native, stream bool, size string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"model":"client-model","max_tokens":64,"stream":%t,"messages":[{"role":"user","content":"hi"}],"generationConfig":{"imageConfig":{"imageSize":"%s"}}}`, stream, size)
	path := "/v1/messages"
	action := "generateContent"
	if native {
		if stream {
			action = "streamGenerateContent"
		}
		path = "/v1beta/models/client-model:" + action
		body = fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"imageConfig":{"imageSize":"%s"}}}`, size)
	}
	return f.serve(t, path, body, func(h *GatewayHandler, c *gin.Context) {
		if native {
			c.Params = gin.Params{{Key: "modelAction", Value: "client-model:" + action}}
			h.GeminiV1BetaModels(c)
		} else {
			h.Messages(c)
		}
	})
}
func TestGeminiImageLockedEntries(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, size := range []string{"", "2K", "4K"} {
				t.Run(fmt.Sprint(native, stream, size), func(t *testing.T) {
					f, u := geminiImageFixture(t)
					u.mutate = func() {
						*f.group.ModelPricing[0].PerRequestPrice = 99
						f.group.ImageRateMultiplier = 99
						f.account.Credentials["model_mapping"] = map[string]any{"client-model": "gemini-2.5-pro"}
					}
					rec := serveGeminiImage(t, f, native, stream, size)
					require.Equal(t, 200, rec.Code, rec.Body.String())
					require.Equal(t, 1, u.calls)
					require.Contains(t, u.paths[0], "gemini-2.5-flash")
					require.Len(t, f.ledger.commands, 1)
					require.InDelta(t, 0.75, f.ledger.commands[0].BalanceCost, 1e-12)
					require.Len(t, f.logs.rows, 1)
					require.Equal(t, 1, f.logs.rows[0].ImageCount)
					require.InDelta(t, 3, f.logs.rows[0].RateMultiplier, 1e-12)
					assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
				})
			}
		}
	}
}
func TestGeminiImageMissingTargetTierBeforeSend(t *testing.T) {
	for _, native := range []bool{false, true} {
		f, u := geminiImageFixture(t)
		p := 0.25
		f.group.ModelPricing[0].PerRequestPrice = nil
		f.group.ModelPricing[0].Intervals = []service.PricingInterval{{TierLabel: "4K", PerRequestPrice: &p}}
		rec := serveGeminiImage(t, f, native, false, "")
		require.Equal(t, 503, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "pricing_unavailable")
		require.Equal(t, 0, u.calls)
		require.Empty(t, f.ledger.commands)
	}
}
func TestGeminiImageMessagesFreshAdmission(t *testing.T) {
	for _, multi := range []bool{false, true} {
		f, u := geminiImageFixture(t)
		if multi {
			g := *f.group
			g.ID++
			f.key.Groups = append(f.key.Groups, service.APIKeyGroupLink{GroupID: g.ID, Role: service.APIKeyGroupRoleNormal, Position: 1, Group: &g})
		}
		f.key.Status = "disabled"
		rec := serveGeminiImage(t, f, false, false, "")
		require.NotEqual(t, 200, rec.Code)
		require.Equal(t, 0, u.calls)
		require.Empty(t, f.ledger.commands)
	}
}
func TestGeminiImageExplicitZeroAndTokenCard(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, token := range []bool{false, true} {
			f, u := geminiImageFixture(t)
			if token {
				a, b, z := 1e-6, 2e-6, 0.0
				f.group.ModelPricing = []service.ChannelModelPricing{{Models: []string{"client-model", "gemini-2.5-flash"}, InputPrice: &a, OutputPrice: &b, CacheReadPrice: &z, CacheWritePrice: &z}}
			} else {
				*f.group.ModelPricing[0].PerRequestPrice = 0
			}
			rec := serveGeminiImage(t, f, native, false, "2K")
			require.Equal(t, 200, rec.Code, rec.Body.String())
			require.Equal(t, 1, u.calls)
			require.Len(t, f.ledger.commands, 1)
			want := 0.0
			if token {
				want = 18e-6
			}
			require.InDelta(t, want, f.ledger.commands[0].BalanceCost, 1e-12)
		}
	}
}
func TestGeminiImageMessagesWorkerSnapshot(t *testing.T) {
	f, _ := geminiImageFixture(t)
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
	body := messagesChainBody(false)
	rec := f.serve(t, "/v1/messages", body, func(h *GatewayHandler, c *gin.Context) {
		h.Messages(c)
		budget := service.RequestAttemptBudgetFromContext(c.Request.Context())
		require.NoError(t, budget.AllowSend())
		c.Keys = map[string]any{}
		*f.key.GroupID = 999
	})
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Empty(t, f.logs.rows)
	close(release)
	released = true
	pool.Stop()
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
	require.InDelta(t, 0.75, f.ledger.commands[0].BalanceCost, 1e-12)
}
func TestGeminiImageEffectivePlatformPriority(t *testing.T) {
	for _, force := range []string{service.PlatformAntigravity, service.PlatformGemini} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		middleware.ForcePlatform(force)(c)
		key := &service.APIKey{Group: &service.Group{Platform: service.PlatformGemini}}
		require.Equal(t, force == service.PlatformGemini, messagePlatformResolvesToGemini(c, key))
	}
}
func TestGeminiImageNativeTargetMissingTier(t *testing.T) {
	f, b, _ := newNativeChainFixture(t)
	u := &geminiImageHTTP{failID: f.account.ID}
	f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, u, nil, f.handler.cfg)
	p := 0.25
	b.ModelPricing = []service.ChannelModelPricing{{Models: []string{"client-model", "gemini-2.5-flash"}, BillingMode: service.BillingModeImage, Intervals: []service.PricingInterval{{TierLabel: "4K", PerRequestPrice: &p}}}}
	rec := serveGeminiImage(t, f, true, false, "1K")
	require.Equal(t, 503, rec.Code, rec.Body.String())
	require.Empty(t, u.paths, "B must not send; A attempts only return failures before path capture")
	require.Greater(t, u.calls, 0)
	require.Empty(t, f.ledger.commands)
}
func TestGeminiImageMessagesAccountFailoverAudit(t *testing.T) {
	f, u := geminiImageFixture(t)
	u.failID = f.account.ID
	f.fallback.AccountGroups = []service.AccountGroup{{AccountID: f.fallback.ID, GroupID: f.group.ID}}
	rec := serveGeminiImage(t, f, false, false, "")
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Len(t, f.logs.rows, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverAccount, u.calls)
	require.Len(t, f.ledger.commands, 1)
}
func TestGeminiImageMissingTierReleasesUserSlot(t *testing.T) {
	for _, native := range []bool{false, true} {
		f, u := geminiImageFixture(t)
		cache := &messagesUserSlotCache{}
		f.handler.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0)
		p := 0.25
		f.group.ModelPricing[0].PerRequestPrice = nil
		f.group.ModelPricing[0].Intervals = []service.PricingInterval{{TierLabel: "4K", PerRequestPrice: &p}}
		rec := serveGeminiImage(t, f, native, false, "")
		require.Equal(t, 503, rec.Code)
		require.Zero(t, u.calls)
		require.Equal(t, 1, cache.acquired)
		require.Equal(t, 1, cache.released)
	}
}
func TestGeminiImageResolvedPlatformPriority(t *testing.T) {
	for _, resolved := range []string{service.PlatformGemini, service.PlatformAnthropic} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil).WithContext(service.WithResolvedTargetPlatform(context.Background(), resolved))
		key := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
		require.Equal(t, resolved == service.PlatformGemini, messagePlatformResolvesToGemini(c, key))
		middleware.ForcePlatform(service.PlatformAntigravity)(c)
		require.False(t, messagePlatformResolvesToGemini(c, key))
	}
}
