//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func videoPlanPriceRef(v float64) *float64 { return &v }

// PRD-06/K-4: a per_request Seedance plan must settle without
// usage.completion_tokens — the charge is priced from the request, so a missing
// token figure must not drop the whole task.
func TestPrepareSeedanceCompletionBillingPerRequestPlanNeedsNoTokens(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{
		service.VideoPriceFamilySeedance: {
			Mode:            domain.VideoBillingModePerRequest,
			PerRequestPrice: videoPlanPriceRef(0.5),
		},
	}

	// The create request stores the pending snapshot the settlement needs.
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(
		context.Background(), "task-ark", subject.UserID, key.ID,
		service.GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance"}))

	result := &service.OpenAIForwardResult{
		Model: "doubao-seedance", BillingModel: "doubao-seedance",
		ResponseID: "seedance:task-ark",
		// No token usage at all.
	}
	billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "task-ark", result)
	require.NotNil(t, billed, "a per_request plan must settle without tokens")
	require.Equal(t, "doubao-seedance", billed.BillingModel)
}

// per_second plans settle from duration, likewise without tokens.
func TestPrepareSeedanceCompletionBillingPerSecondPlanNeedsNoTokens(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{
		service.VideoPriceFamilySeedance: {
			Mode:            domain.VideoBillingModePerSecond,
			PerSecondPrices: map[string]float64{service.VideoBillingResolution720P: 0.2},
		},
	}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(
		context.Background(), "task-ark", subject.UserID, key.ID,
		service.GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance"}))

	result := &service.OpenAIForwardResult{
		Model: "doubao-seedance", BillingModel: "doubao-seedance",
		ResponseID: "seedance:task-ark", VideoDurationSeconds: 5,
	}
	billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "task-ark", result)
	require.NotNil(t, billed, "a per_second plan must settle from duration without tokens")
}

// A per_token plan still requires tokens: without usage there is nothing to
// reconcile, so the settlement is refused rather than billed as zero.
func TestPrepareSeedanceCompletionBillingPerTokenPlanRequiresTokens(t *testing.T) {
	h, _, _, _ := newGrokMediaSlotHandler(t, false, false, service.PlatformOpenAI)
	c, _ := grokMediaSlotContext(context.Background(), true)
	key, _ := middleware.GetAPIKeyFromContext(c)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	key.Group.VideoPricePlans = map[string]domain.VideoPricePlan{
		service.VideoPriceFamilySeedance: {
			Mode: domain.VideoBillingModePerToken,
			PerTokenPrices: map[string]domain.VideoTokenPrice{
				service.VideoBillingResolution720P: {WithoutInputVideo: videoPlanPriceRef(3)},
			},
		},
	}
	require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(
		context.Background(), "task-ark", subject.UserID, key.ID,
		service.GrokVideoPendingBilling{Model: "doubao-seedance", BillingModel: "doubao-seedance"}))

	withoutTokens := &service.OpenAIForwardResult{
		Model: "doubao-seedance", BillingModel: "doubao-seedance", ResponseID: "seedance:task-ark",
	}
	require.Nil(t, prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "task-ark", withoutTokens))

	// With tokens the same task settles once.
	withTokens := &service.OpenAIForwardResult{
		Model: "doubao-seedance", BillingModel: "doubao-seedance", ResponseID: "seedance:task-ark",
		Usage: service.OpenAIUsage{OutputTokens: 12345},
	}
	billed := prepareSeedanceCompletionBilling(context.Background(), h, key, subject, "task-ark", withTokens)
	require.NotNil(t, billed)
	require.Equal(t, 12345, billed.Usage.OutputTokens)
}
