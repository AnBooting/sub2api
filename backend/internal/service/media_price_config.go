package service

import "github.com/Wei-Shaw/sub2api/internal/domain"

func imagePriceConfigFromAPIKey(apiKey *APIKey) *ImagePriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return &ImagePriceConfig{
		Price1K: apiKey.Group.ImagePrice1K,
		Price2K: apiKey.Group.ImagePrice2K,
		Price4K: apiKey.Group.ImagePrice4K,
	}
}

func apiKeyHasConfiguredImagePrice(apiKey *APIKey, imageSize string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetImagePrice(imageSize) != nil
}

func videoPriceConfigFromAPIKey(apiKey *APIKey) *VideoPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return &VideoPriceConfig{
		Price480P:   apiKey.Group.VideoPrice480P,
		Price720P:   apiKey.Group.VideoPrice720P,
		Price1080P:  apiKey.Group.VideoPrice1080P,
		ModelPrices: apiKey.Group.VideoModelPrices,
	}
}

func apiKeyHasConfiguredVideoPrice(apiKey *APIKey, model, resolution string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetVideoPriceForModel(model, resolution) != nil
}

// GroupHasSeedanceVideoPrice reports whether the group carries an operator-set
// Seedance price. PRD-06/K-4 sells Seedance per request or per second; there is no
// bundled default and no price source is guessed, so callers must fail closed when
// this returns false instead of charging another model's tariff.
func GroupHasSeedanceVideoPrice(group *Group) bool {
	if group == nil {
		return false
	}
	// A per-model slot counts if ANY tier is priced; checking a single default tier
	// would miss a group that only configured e.g. 720p.
	prices := NormalizeVideoModelPrices(group.VideoModelPrices)
	for _, tier := range VideoBillingResolutionTiers {
		if LookupVideoModelPrice(prices, VideoPriceFamilySeedance, tier) != nil {
			return true
		}
	}
	// PRD-06/K-4: a metering plan (per_request / per_second / per_token) is the
	// first-class Seedance price source. A plan with a valid mode and at least one
	// priced unit is a configured price; a plan with a mode but no prices is not.
	//
	// The flat video_price_* columns are deliberately NOT accepted here: they are
	// xAI's per-second tariff, and a Seedance result never reaches that settlement
	// path (its VideoCount stays 0, so it falls through to token billing). Accepting
	// them would admit the create and then settle at zero cost.
	return GroupHasVideoPricePlan(group, VideoPriceFamilySeedance)
}

// apiKeyHasConfiguredVideoPlan reports whether the group behind the API key
// declares a usable video metering plan for the model. Callers use it to prefer
// the plan path over the legacy per-second tariff without ever guessing a price.
func apiKeyHasConfiguredVideoPlan(apiKey *APIKey, model string) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	return GroupHasVideoPricePlan(apiKey.Group, model)
}

// videoPlanFromAPIKey returns the group's video metering plan for the model, or
// nil when the group declares none (fail closed at the caller).
func videoPlanFromAPIKey(apiKey *APIKey, model string) *domain.VideoPricePlan {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return VideoPricePlanForGroup(apiKey.Group, model)
}

// FrozenVideoBillingForGroup resolves the video quote and effective multiplier to
// freeze into an async task snapshot at acceptance time (PRD-06 AC-B4).
//
// Resolution order mirrors the settlement path exactly, so "frozen at accept" and
// "resolved live at settle" agree for every configuration that can currently settle:
//
//  1. the operator's per-request/per-second/per-token plan (video_price_plans);
//  2. the Seedance legacy per-model / flat price columns;
//  3. the bundled Grok per-resolution defaults.
//
// Step 3 matters: Grok has no plan row, so without freezing the bundled default the
// R3 scenario (Grok price changed mid-flight) would still settle at the live price.
// Returns nil plan when nothing priceable is found — callers keep the existing
// fail-closed behavior rather than inventing a price.
func FrozenVideoBillingForGroup(group *Group, model string, effectiveGroupMultiplier float64) (*domain.VideoPricePlan, *float64, string) {
	if group == nil {
		return nil, nil, ""
	}
	plan := VideoPricePlanForGroup(group, model)
	source := "plan"
	if plan == nil {
		plan = SeedanceLegacyPerSecondPlan(group, model)
		source = "legacy_slot"
	}
	if plan == nil {
		plan = bundledDefaultVideoPlan(model)
		source = "bundled_default"
	}
	if plan == nil {
		return nil, nil, ""
	}
	multiplier := resolveVideoRateMultiplier(&APIKey{Group: group}, effectiveGroupMultiplier)
	return plan, &multiplier, source
}

// bundledDefaultVideoPlan materializes the built-in Grok per-resolution tariff as a
// plan so it can be frozen like any operator-configured plan. Returns nil when the
// model has no bundled default (fail closed).
func bundledDefaultVideoPlan(model string) *domain.VideoPricePlan {
	switch CanonicalVideoPriceFamily(model) {
	case VideoPriceFamilyGrokImagineVideo, VideoPriceFamilyGrokImagineVideo15:
	default:
		return nil
	}
	prices := map[string]float64{}
	for _, tier := range VideoBillingResolutionTiers {
		if price, ok := getDefaultGrokImagineVideoPrice(model, tier); ok && price >= 0 {
			prices[tier] = price
		}
	}
	if len(prices) == 0 {
		return nil
	}
	return &domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: prices}
}

func webSearchPricePerCallFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.WebSearchPricePerCall
}

func groupSearchPricePer1kFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.GetSearchPricePer1k()
}

func groupAudioPriceConfigFromAPIKey(apiKey *APIKey) *audioPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	g := apiKey.Group
	return &audioPriceConfig{
		RealtimePerMin: g.AudioRealtimePricePerMin,
		TTSPerMChars:   g.AudioTTSPricePerMillionChars,
		STTPerHour:     g.AudioSTTPricePerHour,
	}
}
