package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// FrozenVideoPricing is the serializable part of a resolved group/channel card.
// Keep the source card explicitly: ResolvedPricing's private fields are required
// for explicit zero, currency, reasoning and time pricing, but JSON omits them.
// No repositories, credentials or live cache pointers cross the async boundary.
type FrozenVideoPricing struct {
	Mode                      BillingMode          `json:"mode"`
	BasePricing               *ModelPricing        `json:"base_pricing,omitempty"`
	Intervals                 []PricingInterval    `json:"intervals,omitempty"`
	RequestTiers              []PricingInterval    `json:"request_tiers,omitempty"`
	DefaultPerRequestPrice    float64              `json:"default_per_request_price"`
	Card                      *ChannelModelPricing `json:"card"`
	Source                    string               `json:"source"`
	PricingAt                 time.Time            `json:"pricing_at"`
	LongContextPricingEnabled bool                 `json:"long_context_pricing_enabled"`
	SupportsCacheBreakdown    bool                 `json:"supports_cache_breakdown"`
}

func freezeVideoPricing(r *ResolvedPricing, at time.Time) (*FrozenVideoPricing, error) {
	if r.pricingError != nil {
		return nil, r.pricingError
	}
	if r.BasePricing != nil && r.BasePricing.tokenPricingError != nil {
		return nil, r.BasePricing.tokenPricingError
	}
	if r.channelPricing == nil {
		return nil, fmt.Errorf("%w: video card is missing", ErrModelPricingUnavailable)
	}
	card := cloneAPIKeyGroupPricing(*r.channelPricing)
	// Clone the resolved intervals too (group token cards deliberately strip them).
	intervals := cloneAPIKeyGroupPricing(ChannelModelPricing{Intervals: r.Intervals}).Intervals
	tiers := cloneAPIKeyGroupPricing(ChannelModelPricing{Intervals: r.RequestTiers}).Intervals
	return &FrozenVideoPricing{Mode: r.Mode, BasePricing: cloneRequestModelPricing(r.BasePricing), Intervals: intervals, RequestTiers: tiers,
		DefaultPerRequestPrice: r.DefaultPerRequestPrice, Card: &card, Source: r.Source, PricingAt: at,
		LongContextPricingEnabled: r.longContextPricingEnabled, SupportsCacheBreakdown: r.SupportsCacheBreakdown}, nil
}

func (q *FrozenVideoPricing) resolved() (*ResolvedPricing, error) {
	if q == nil || q.Card == nil || q.PricingAt.IsZero() || (q.Source != PricingSourceGroup && q.Source != PricingSourceChannel) {
		return nil, fmt.Errorf("%w: incomplete frozen video card", ErrModelPricingUnavailable)
	}
	switch q.Mode {
	case BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo:
	default:
		return nil, fmt.Errorf("%w: invalid frozen video mode %q", ErrModelPricingUnavailable, q.Mode)
	}
	if q.Mode == BillingModeToken {
		if q.BasePricing == nil {
			return nil, fmt.Errorf("%w: frozen video token rates missing", ErrModelPricingUnavailable)
		}
		if err := manualTokenPricingError(q.Card); err != nil {
			return nil, err
		}
	}
	base := cloneRequestModelPricing(q.BasePricing)
	if base != nil && q.Mode == BillingModeToken {
		base.manualTokenPricing = hasChannelTokenOverrides(q.Card)
	}
	return &ResolvedPricing{Mode: q.Mode, BasePricing: base, Intervals: q.Intervals, RequestTiers: q.RequestTiers,
		DefaultPerRequestPrice: q.DefaultPerRequestPrice, Source: q.Source, channelPricing: q.Card, pricingAt: q.PricingAt,
		longContextPricingEnabled: q.LongContextPricingEnabled, SupportsCacheBreakdown: q.SupportsCacheBreakdown}, nil
}

func cloneVideoPlan(plan *domain.VideoPricePlan) *domain.VideoPricePlan {
	if plan == nil {
		return nil
	}
	// Normalization owns every scalar/map and preserves explicit zero prices.
	plans := NormalizeVideoPricePlans(map[string]domain.VideoPricePlan{"quote": *plan})
	if copy, ok := plans["quote"]; ok {
		return &copy
	}
	// Preserve an invalid plan as invalid, never silently fall back to live prices.
	return &domain.VideoPricePlan{Mode: plan.Mode}
}

// resolveVideoTariff is shared by admission and the legacy live video calculator.
// Only media prices are normalized to a video plan. Configured cards retain their
// own units/currency; in particular a Grok token card is never made per-second.
func (s *OpenAIGatewayService) resolveVideoTariff(ctx context.Context, key *APIKey, model, resolution string) (*domain.VideoPricePlan, *FrozenVideoPricing, string, error) {
	at := requestPricingAt(ctx, time.Time{})
	resolved := s.resolveOpenAIChannelPricing(ctx, model, key)
	cardQuote := func() (*domain.VideoPricePlan, *FrozenVideoPricing, string, error) {
		q, err := freezeVideoPricing(resolved, at)
		return nil, q, resolved.Source, err
	}
	if isGrokVideoBillingModel(model) && resolved != nil && resolved.Mode == BillingModeToken {
		return cardQuote()
	}
	if plan := videoPlanFromAPIKey(key, model); plan != nil {
		return cloneVideoPlan(plan), nil, "plan", nil
	}
	if key != nil && canonicalSeedanceVideoPriceFamily(model) != "" {
		if plan := SeedanceLegacyPerSecondPlan(key.Group, model); plan != nil {
			return cloneVideoPlan(plan), nil, "legacy_slot", nil
		}
		return nil, nil, "", fmt.Errorf("%w: no Seedance video tariff", ErrModelPricingUnavailable)
	}
	if resolved != nil && resolved.Source == PricingSourceGroup && resolved.Mode == BillingModeVideo {
		return cardQuote()
	}
	key = s.apiKeyWithFreshGroupMediaPricing(ctx, key)
	if apiKeyHasConfiguredVideoPrice(key, model, resolution) {
		prices := map[string]float64{}
		for _, tier := range VideoBillingResolutionTiers {
			if p := key.Group.GetVideoPriceForModel(model, tier); p != nil {
				prices[tier] = *p
			} else if p, ok := getDefaultGrokImagineVideoPrice(model, tier); ok {
				prices[tier] = p
			}
		}
		return &domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: prices}, nil, "legacy_slot", nil
	}
	if resolved != nil && resolved.Source == PricingSourceChannel && (resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo) {
		return cardQuote()
	}
	prices := map[string]float64{}
	for _, tier := range VideoBillingResolutionTiers {
		if p, ok := getDefaultGrokImagineVideoPrice(model, tier); ok {
			prices[tier] = p
		}
	}
	if len(prices) > 0 {
		return &domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: prices}, nil, "bundled_default", nil
	}
	return nil, nil, "", fmt.Errorf("%w: no video tariff for %q", ErrModelPricingUnavailable, model)
}

// FreezeVideoBilling captures the real customer quote BEFORE forwarding. Account
// retries reuse this value; accepting the task only persists it, never re-resolves.
func (s *OpenAIGatewayService) FreezeVideoBilling(ctx context.Context, key *APIKey, model, resolution string, hasInputVideo bool) (*GrokVideoPendingBilling, error) {
	if key == nil || key.Group == nil {
		return nil, fmt.Errorf("%w: video group missing", ErrModelPricingUnavailable)
	}
	at := requestPricingAt(ctx, time.Time{})
	ctx = context.WithValue(ctx, gatewayTokenRequestPricingAtCtxKey{}, at)
	plan, pricing, source, err := s.resolveVideoTariff(ctx, key, model, resolution)
	if err != nil {
		return nil, err
	}
	base := s.ResolveUserGroupRateMultiplier(ctx, key.UserID, key.Group.ID, key.Group.RateMultiplier)
	multiplier := resolveVideoRateMultiplier(key, base)
	if pricing != nil && pricing.Mode == BillingModeToken {
		multiplier, _ = computePeakAwareMultipliers(key, base, at)
	}
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 0 {
		return nil, fmt.Errorf("%w: invalid video multiplier", ErrModelPricingUnavailable)
	}
	pending := &GrokVideoPendingBilling{HasInputVideo: hasInputVideo, FrozenVideoPlan: plan, FrozenVideoPricing: pricing, FrozenVideoMultiplier: &multiplier, FrozenVideoPriceSource: source}
	// Validate the exact requested cell using the production calculator. Unit usage
	// is a price-availability probe, not an inferred final duration/token count.
	probe := &OpenAIForwardResult{Model: model, BillingModel: model, VideoResolution: resolution, VideoCount: 1, VideoDurationSeconds: 1, Usage: OpenAIUsage{OutputTokens: 1}}
	pending.ApplyToVideoResult(probe)
	_, err = s.calculateFrozenVideoCost(WithVideoPlanInputVideoFact(ctx, hasInputVideo), probe, UsageTokens{OutputTokens: 1}, "", nil)
	if err != nil {
		return nil, err
	}
	return pending, nil
}

// ApplyToVideoResult is shared by client polling and worker settlement. It carries
// the create-only input-video fact explicitly, including a proven false value.
func (p *GrokVideoPendingBilling) ApplyToVideoResult(result *OpenAIForwardResult) {
	if p == nil || result == nil {
		return
	}
	result.FrozenVideoPlan = p.FrozenVideoPlan
	result.FrozenVideoPricing = p.FrozenVideoPricing
	result.FrozenVideoMultiplier = p.FrozenVideoMultiplier
	result.FrozenVideoPriceSource = p.FrozenVideoPriceSource
	result.VideoHasInputVideo = cloneGroupValuePointer(&p.HasInputVideo)
}

func (r *OpenAIForwardResult) hasFrozenVideoQuote() bool {
	return r != nil && (r.FrozenVideoPlan != nil || r.FrozenVideoPricing != nil || r.FrozenVideoMultiplier != nil || r.FrozenVideoPriceSource != "")
}

func (s *OpenAIGatewayService) calculateFrozenVideoCost(ctx context.Context, result *OpenAIForwardResult, tokens UsageTokens, serviceTier string, longContext *bool) (*CostBreakdown, error) {
	if result.VideoHasInputVideo != nil {
		ctx = WithVideoPlanInputVideoFact(ctx, *result.VideoHasInputVideo)
	}
	multiplier := 1.0
	if result.FrozenVideoMultiplier != nil {
		multiplier = *result.FrozenVideoMultiplier
	}
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || multiplier < 0 {
		return nil, fmt.Errorf("%w: invalid frozen video multiplier", ErrModelPricingUnavailable)
	}
	if result.FrozenVideoPricing != nil {
		q := result.FrozenVideoPricing
		if q.Mode == BillingModeToken && result.Usage.OutputTokens <= 0 {
			return nil, fmt.Errorf("%w: frozen video completion token usage is missing", ErrModelPricingUnavailable)
		}
		resolved, err := q.resolved()
		if err != nil {
			return nil, err
		}
		count := max(1, result.VideoCount)
		units := float64(count)
		if q.Mode == BillingModeVideo {
			if result.VideoDurationSeconds <= 0 {
				return nil, fmt.Errorf("%w: missing video duration", ErrModelPricingUnavailable)
			}
			units *= float64(result.VideoDurationSeconds)
		}
		cost, err := s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: result.BillingModel, Resolved: resolved,
			Resolver: NewModelPricingResolver(nil, s.billingService), Tokens: tokens, RequestCount: count, UsageUnits: units,
			SizeTier: NormalizeVideoBillingResolutionOrDefault(result.VideoResolution), RateMultiplier: multiplier, PricingAt: q.PricingAt,
			ServiceTier: serviceTier, ReasoningEffort: optionalStringValue(result.ReasoningEffort), LongContextBillingEnabled: longContext})
		if err != nil {
			return nil, err
		}
		if q.Mode != BillingModeToken {
			cost.BillingMode = string(BillingModeVideo)
		}
		return cost, nil
	}
	return s.billingService.CalculateVideoCostFromPlan(VideoCostInput{Model: result.BillingModel, Resolution: result.VideoResolution,
		VideoCount: result.VideoCount, DurationSeconds: result.VideoDurationSeconds, CompletionTokens: videoBillingPlanTokenUsage(result),
		HasInputVideo: videoPlanInputVideoFromContext(ctx), Plan: result.FrozenVideoPlan, RateMultiplier: multiplier})
}

// Frozen plans decide required usage; only historical tasks consult live config.
func SeedanceResultRequiresTokens(group *Group, model string, result *OpenAIForwardResult) bool {
	if result != nil {
		if result.FrozenVideoPricing != nil {
			return result.FrozenVideoPricing.Mode == BillingModeToken
		}
		if result.FrozenVideoPlan != nil {
			return result.FrozenVideoPlan.Mode == domain.VideoBillingModePerToken
		}
	}
	return SeedanceCompletionRequiresTokens(group, model)
}
