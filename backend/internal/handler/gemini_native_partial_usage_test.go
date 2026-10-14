//go:build unit

package handler

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type nativePartialHTTP struct {
	service.HTTPUpstream
	calls    int
	measured bool
	cancel   bool
	success  bool
	slots    *nativePartialSlots
}

func (u *nativePartialHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	usage := `,"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":500}`
	if !u.measured {
		usage = ""
	}
	payload := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]}}]` + usage + "}\n\n"
	var body io.ReadCloser = &nativePartialReader{Reader: strings.NewReader(payload), err: errors.New("upstream disconnected")}
	if u.cancel {
		body = &nativePartialReader{Reader: strings.NewReader(payload), err: context.Canceled}
	}
	if u.success {
		body = io.NopCloser(strings.NewReader(payload))
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}, nil
}

type nativePartialReader struct {
	*strings.Reader
	err error
}

func (r *nativePartialReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, r.err
	}
	return n, err
}
func (r *nativePartialReader) Close() error { return nil }
func nativePartialFixture(t *testing.T) (*sharedLockedFixture, *nativePartialHTTP) {
	f, b, up := newNativeChainFixture(t)
	slots := &nativePartialSlots{}
	u := &nativePartialHTTP{measured: true, slots: slots}
	cfg := f.handler.cfg
	billing := service.NewBillingService(cfg, nil)
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: map[int64]string{f.group.ID: service.PlatformGemini, b.ID: service.PlatformGemini}}, nil, nil, nil, nil)
	funds := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(funds.Stop)
	f.handler.gatewayService = service.NewGatewayService(nil, &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{f.group.ID: f.group, b.ID: b}}, f.logs, f.ledger, nil, nil, nil, nil, cfg, service.NewSchedulerSnapshotService(&nativePlatformCache{groupScopedSchedulerCache: up.cache}, nil, nil, nil, cfg), service.NewConcurrencyService(slots), billing, nil, funds, nil, u, service.NewDeferredService(nil, nil, 0), nil, nil, nil, service.NewDigestSessionStore(), nil, nil, channels, service.NewModelPricingResolver(channels, billing), nil, nil, nil)
	f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, nil, nil, u, nil, f.handler.cfg)
	return f, u
}
func TestNativePartialUsageBillsOnceAndStopsChain(t *testing.T) {
	for _, measured := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			f, u := nativePartialFixture(t)
			u.measured = measured
			u.cancel = cancel
			cache := &messagesUserSlotCache{}
			f.handler.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0)
			rec := serveNativeChain(t, f, true)
			require.Contains(t, rec.Body.String(), "partial")
			require.Equal(t, 1, u.calls)
			require.Equal(t, 1, cache.acquired)
			require.Equal(t, 1, cache.released)
			require.Equal(t, 1, u.slots.acquired)
			require.Equal(t, 1, u.slots.released)
			if measured {
				require.Len(t, f.ledger.commands, 1)
				require.InDelta(t, 0.002, f.ledger.commands[0].BalanceCost, 1e-12)
				require.Len(t, f.logs.rows, 1)
				assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
			} else {
				require.Empty(t, f.ledger.commands)
				require.Empty(t, f.logs.rows)
			}
		}
	}
}
func TestNativePartialUsageQueuedSnapshot(t *testing.T) {
	f, u := nativePartialFixture(t)
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
	rec := f.serve(t, "/v1beta/models/client-model:streamGenerateContent", nativeChainBody(), func(h *GatewayHandler, c *gin.Context) {
		c.Params = gin.Params{{Key: "modelAction", Value: "client-model:streamGenerateContent"}}
		h.GeminiV1BetaModels(c)
		require.NoError(t, service.RequestAttemptBudgetFromContext(c.Request.Context()).AllowSend())
		c.Keys = map[string]any{}
		*f.key.GroupID = 999
		*f.group.ModelPricing[0].InputPrice = 99
	})
	require.Equal(t, 200, rec.Code)
	require.Equal(t, 1, u.calls)
	require.Empty(t, f.logs.rows)
	close(release)
	released = true
	pool.Stop()
	require.Len(t, f.logs.rows, 1)
	require.Len(t, f.ledger.commands, 1)
	assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
	require.InDelta(t, 0.002, f.ledger.commands[0].BalanceCost, 1e-12)
}
func TestNativePartialUsageSuccessfulStreamStillOneBill(t *testing.T) {
	f, u := nativePartialFixture(t)
	u.success = true
	rec := serveNativeChain(t, f, true)
	require.Equal(t, 200, rec.Code)
	require.Len(t, f.ledger.commands, 1)
	require.Equal(t, 1, u.calls)
}

type nativePartialSlots struct {
	fakeConcurrencyCache
	acquired, released int
}

func (s *nativePartialSlots) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	s.acquired++
	return true, nil
}
func (s *nativePartialSlots) ReleaseAccountSlot(context.Context, int64, string) error {
	s.released++
	return nil
}
