//go:build unit

package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReReview_SeedancePollCarriesFrozenQuote(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{service.VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPriceRef(5)}}
	frozenPlan := &domain.VideoPricePlan{Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPriceRef(0.5)}
	mult := 1.5
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(context.Background(), "seedance:review-task", subject.UserID, key.ID, service.GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance", FrozenVideoPlan: frozenPlan, FrozenVideoMultiplier: &mult}))
	result := &service.OpenAIForwardResult{Model: "doubao-seedance", BillingModel: "doubao-seedance", ResponseID: "seedance:review-task"}
	billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "seedance:review-task", result)
	require.NotNil(t, billed)
	require.NotNil(t, billed.FrozenVideoPlan, "Seedance polling must carry the acceptance-time price into RecordUsage")
	require.NotNil(t, billed.FrozenVideoMultiplier)
}

func TestVideoQuoteCreateFreezesUserRateBeforeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name        string
		rate        float64
		independent bool
		want        float64
	}{
		{"user", 0.5, false, 0.5}, {"free user", 0, false, 0}, {"independent", 0.5, true, 0.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, slots, _, upstream := newGrokMediaSlotHandler(t, false, false)
			c, w := grokMediaSlotContext(context.Background(), true)
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.RateMultiplier = 2
			key.Group.VideoRateIndependent = tc.independent
			key.Group.VideoRateMultiplier = 0.25
			key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{service.VideoPriceFamilyGrokImagineVideo: {Mode: domain.VideoBillingModePerSecond, PerSecondPrices: map[string]float64{"480p": 0.7}}}
			repo := &keyBillingUserGroupRateRepo{rate: &tc.rate}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			bindings := &grokMediaSlotBindings{}
			accounts := []service.Account{{ID: 1, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 50, GroupIDs: []int64{24}, Credentials: map[string]any{"api_key": "test-key"}}}
			accountRepo := grokMediaSlotRepo{openAIImagesFailoverAccountRepo: openAIImagesFailoverAccountRepo{accounts: accounts}}
			h.gatewayService = service.NewOpenAIGatewayService(accountRepo, nil, nil, nil, nil, repo, bindings, cfg, nil, service.NewConcurrencyService(slots), service.NewBillingService(cfg, nil), nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil)
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				// An edit during the upstream call must not change the already captured quote.
				plan := key.Group.VideoPricePlans[service.VideoPriceFamilyGrokImagineVideo]
				plan.PerSecondPrices["480p"] = 99
				key.Group.RateMultiplier = 9
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"request_id":"quote-task","status":"pending"}`))}, nil
			}
			h.GrokVideoGeneration(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			pending, err := h.gatewayService.LoadGrokVideoPendingBilling(context.Background(), "quote-task", 10, 20)
			require.NoError(t, err)
			require.NotNil(t, pending)
			require.NotNil(t, pending.FrozenVideoPlan)
			require.Equal(t, 0.7, pending.FrozenVideoPlan.PerSecondPrices["480p"])
			require.Equal(t, tc.want, *pending.FrozenVideoMultiplier)
			require.Equal(t, int64(10), repo.gotUserID)
			require.Equal(t, int64(24), repo.gotGroupID)
			slots.assertReleased(t)
		})
	}
}

func TestVideoQuoteSeedancePollUsesFrozenModeAndCreationFacts(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{service.VideoPriceFamilySeedance: {Mode: domain.VideoBillingModePerToken}}
	pending := service.GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance", VideoResolution: "720p", VideoDurationSeconds: 5, HasInputVideo: true, CreatedAt: time.Now().Add(-time.Minute).Format(time.RFC3339Nano), FrozenVideoPlan: &domain.VideoPricePlan{Mode: domain.VideoBillingModePerRequest, PerRequestPrice: videoPlanPriceRef(0.5)}}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(context.Background(), "seedance:quote-mode", 10, 20, pending))
	result := &service.OpenAIForwardResult{Model: "doubao-seedance", ResponseID: "seedance:quote-mode", VideoDurationSeconds: 7}
	billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "seedance:quote-mode", result)
	require.NotNil(t, billed)
	require.NotNil(t, billed.FrozenVideoPlan)
	require.Equal(t, "720p", billed.VideoResolution)
	require.Equal(t, 7, billed.VideoDurationSeconds)
	require.Equal(t, true, *billed.VideoHasInputVideo)
	require.Greater(t, billed.Duration, 50*time.Second)
	require.Nil(t, prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "seedance:quote-mode", result))
}

func TestVideoQuoteSeedanceCreateUsesRequestedTier(t *testing.T) {
	for _, mode := range []domain.VideoBillingMode{domain.VideoBillingModePerSecond, domain.VideoBillingModePerToken} {
		t.Run(string(mode), func(t *testing.T) {
			h, _, _, upstream := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
			c, w := grokMediaSlotContext(context.Background(), true)
			body := `{"model":"doubao-seedance","resolution":"720p","duration":5,"content":[{"type":"text","text":"dance"}]}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			key, _ := middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = service.PlatformOpenAI
			key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{service.VideoPriceFamilySeedance: {Mode: mode, PerSecondPrices: map[string]float64{"720p": 0.5}, PerTokenPrices: map[string]domain.VideoTokenPrice{"720p": {WithoutInputVideo: videoPlanPriceRef(3)}}}}
			upstream.call = func(*http.Request, int64) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"ark-quote","status":"queued"}`))}, nil
			}
			h.SeedanceTasks(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
			pending, err := h.gatewayService.LoadGrokVideoPendingBilling(context.Background(), "seedance:ark-quote", 10, 20)
			require.NoError(t, err)
			require.NotNil(t, pending)
			require.NotNil(t, pending.FrozenVideoPlan)
			require.Equal(t, "720p", pending.VideoResolution)
			require.Equal(t, mode, pending.FrozenVideoPlan.Mode)
			// A different unpriced tier must be rejected BEFORE another upstream call.
			c, w = grokMediaSlotContext(context.Background(), true)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(strings.Replace(body, "720p", "1080p", 1)))
			c.Request.Header.Set("Content-Type", "application/json")
			key, _ = middleware.GetAPIKeyFromContext(c)
			key.Group.Platform = service.PlatformOpenAI
			key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{service.VideoPriceFamilySeedance: {Mode: mode, PerSecondPrices: map[string]float64{"720p": 0.5}, PerTokenPrices: map[string]domain.VideoTokenPrice{"720p": {WithoutInputVideo: videoPlanPriceRef(3)}}}}
			h.SeedanceTasks(c)
			require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
			require.Equal(t, 1, upstream.calls)
		})
	}
}
