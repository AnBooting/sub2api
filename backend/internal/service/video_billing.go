package service

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// Canonical video price family keys used in groups.video_model_prices JSONB.
const (
	VideoPriceFamilyGrokImagineVideo   = "grok-imagine-video"
	VideoPriceFamilyGrokImagineVideo15 = "grok-imagine-video-1.5"
	// VideoPriceFamilySeedance is the reserved price slot for Ark Seedance video.
	// PRD-06/K-4: Seedance is sold per request or per second with an operator-set
	// unit price. There is deliberately NO bundled default and no price source is
	// guessed here; an operator must fill groups.video_model_prices[seedance] or
	// the flat video_price_* columns before Seedance can be sold.
	VideoPriceFamilySeedance = "seedance"
)

// CanonicalVideoPriceFamily maps a request model id onto the family key used in
// groups.video_model_prices. It returns "" when no family is known, which callers
// must treat as "no configured slot" rather than falling back to another tariff.
func CanonicalVideoPriceFamily(model string) string {
	if family := CanonicalGrokImagineVideoPriceFamily(model); family != "" {
		return family
	}
	if canonicalSeedanceVideoPriceFamily(model) != "" {
		return VideoPriceFamilySeedance
	}
	return ""
}

// canonicalSeedanceVideoPriceFamily recognizes Ark Seedance model ids (including
// versioned/dated endpoint ids) so every Seedance variant shares one price slot.
func canonicalSeedanceVideoPriceFamily(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ""
	}
	for _, prefix := range []string{"ark/", "volcengine/", "bytedance/", "doubao/", "dreamina/"} {
		if strings.HasPrefix(m, prefix) {
			m = strings.TrimPrefix(m, prefix)
			break
		}
	}
	// Ark ships the same family under several id shapes, and the pricing page
	// lists dotted names that are NOT the callable id (doubao-seedance-2.5 vs
	// doubao-seedance-2-5-260628). All of them must land on one price slot, or an
	// operator who priced "doubao-seedance-2.5" would not cover the callable id:
	//   doubao-seedance-<gen>-<tier>-<YYMMDD>  e.g. doubao-seedance-2-5-260628
	//   dreamina-seedance-<gen>-<tier>-<YYMMDD> (international site prefix)
	//   seedance-1-0-lite-* / seedance / doubao-seedance-2.5 (pricing-page names)
	switch {
	case strings.HasPrefix(m, "seedance"),
		strings.HasPrefix(m, "doubao-seedance"),
		strings.HasPrefix(m, "dreamina-seedance"):
		return VideoPriceFamilySeedance
	}
	return ""
}

// CanonicalGrokImagineVideoPriceFamily normalizes model aliases / preview / legacy
// IDs onto the price-family keys stored in video_model_prices.
func CanonicalGrokImagineVideoPriceFamily(model string) string {
	if model == "" {
		return ""
	}
	// Prefer shared xAI helper for known aliases. Keep future native Imagine
	// models distinct so operators can assign them independent prices.
	if c := xai.CanonicalImagineVideoModel(model); c != "" {
		switch c {
		case xai.DefaultImagineVideo15Model:
			return VideoPriceFamilyGrokImagineVideo15
		case xai.DefaultImagineVideoModel:
			return VideoPriceFamilyGrokImagineVideo
		}
		if strings.HasPrefix(c, "grok-imagine-video-") {
			return c
		}
	}
	m := strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"xai/", "x-ai/", "grok/"} {
		if strings.HasPrefix(m, prefix) {
			m = strings.TrimPrefix(m, prefix)
			break
		}
	}
	switch {
	case m == "grok-imagine-video-1.5" || m == "grok-imagine-video-1.5-preview" ||
		m == "grok-video-1.5" || strings.Contains(m, "video-1.5"):
		return VideoPriceFamilyGrokImagineVideo15
	case m == "grok-imagine-video" || m == "grok-imagine-video-preview" ||
		m == "grok-video" || m == "grok-video-latest":
		return VideoPriceFamilyGrokImagineVideo
	default:
		return ""
	}
}

// NormalizeVideoModelPrices cleans and canonicalizes a per-model resolution map.
// Keys become price families; tiers use 480p/720p/1080p. Negative prices dropped.
//
// Model keys are walked in sorted order rather than in Go map order: several
// aliases can canonicalize onto the same family, and an unordered walk would
// make the winning price for a conflicting tier vary between processes.
// Unrecognized tiers are dropped with a warning instead of silently collapsing
// into the 480p bucket.
func NormalizeVideoModelPrices(in map[string]map[string]float64) map[string]map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	modelKeys := make([]string, 0, len(in))
	for modelKey := range in {
		modelKeys = append(modelKeys, modelKey)
	}
	sort.Strings(modelKeys)
	out := make(map[string]map[string]float64)
	for _, modelKey := range modelKeys {
		tierPrices := in[modelKey]
		if len(tierPrices) == 0 {
			continue
		}
		family := CanonicalGrokImagineVideoPriceFamily(modelKey)
		if family == "" {
			key := strings.ToLower(strings.TrimSpace(modelKey))
			switch key {
			case VideoPriceFamilyGrokImagineVideo, VideoPriceFamilyGrokImagineVideo15:
				family = key
			default:
				if key == "" {
					continue
				}
				family = key
			}
		}
		normalizedTiers := out[family]
		if normalizedTiers == nil {
			normalizedTiers = make(map[string]float64)
		}
		tierKeys := make([]string, 0, len(tierPrices))
		for tierKey := range tierPrices {
			tierKeys = append(tierKeys, tierKey)
		}
		sort.Strings(tierKeys)
		for _, tierKey := range tierKeys {
			price := tierPrices[tierKey]
			if price < 0 {
				continue
			}
			tier, ok := LookupVideoBillingResolution(tierKey)
			if !ok {
				slog.Warn("video_model_prices_unknown_resolution_dropped",
					"model_key", modelKey,
					"family", family,
					"resolution", tierKey)
				continue
			}
			if existing, exists := normalizedTiers[tier]; exists && existing != price {
				slog.Warn("video_model_prices_conflicting_tier_price",
					"model_key", modelKey,
					"family", family,
					"resolution", tier,
					"previous_price", existing,
					"price", price)
			}
			normalizedTiers[tier] = price
		}
		if len(normalizedTiers) > 0 {
			out[family] = normalizedTiers
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// LookupVideoModelPrice returns a per-second price from a model×resolution map, or nil.
func LookupVideoModelPrice(prices map[string]map[string]float64, model, resolution string) *float64 {
	if len(prices) == 0 {
		return nil
	}
	family := CanonicalVideoPriceFamily(model)
	if family == "" {
		family = strings.ToLower(strings.TrimSpace(model))
	}
	if family == "" {
		return nil
	}
	tierPrices, ok := prices[family]
	if !ok || len(tierPrices) == 0 {
		return nil
	}
	tier := NormalizeVideoBillingResolutionOrDefault(resolution)
	if price, ok := tierPrices[tier]; ok {
		p := price
		return &p
	}
	return nil
}

// NormalizeVideoPricePlanFamilyKey canonicalizes a groups.video_price_plans map
// key onto the family key used by CanonicalVideoPriceFamily. A model with no
// reserved family (e.g. MiniMax Hailuo) keeps its lower-cased model id, which is
// exactly the fallback key VideoPricePlanForGroup looks up.
func NormalizeVideoPricePlanFamilyKey(family string) string {
	trimmed := strings.TrimSpace(family)
	if trimmed == "" {
		return ""
	}
	if canonical := CanonicalVideoPriceFamily(trimmed); canonical != "" {
		return canonical
	}
	return strings.ToLower(trimmed)
}

// NormalizeVideoPricePlans cleans and canonicalizes the per-family metering plans
// stored in groups.video_price_plans (PRD-06/K-4).
//
// Fail closed by construction: an unsupported mode is dropped, and a plan whose
// declared mode carries no usable price is dropped as well — a mode without a
// price is unconfigured, never free. Negative prices are removed. Resolution
// tiers are canonicalized; an unrecognized tier is dropped with a warning rather
// than collapsed into 480p, which would silently move a 1080p price onto the low
// tier.
func NormalizeVideoPricePlans(in map[string]domain.VideoPricePlan) map[string]domain.VideoPricePlan {
	if len(in) == 0 {
		// Return an empty (non-nil) map: the column is JSONB NOT NULL, and "no plan
		// configured" must persist as {} rather than depending on how ent serializes
		// a nil map. Callers gate on emptiness, never on SQL NULL.
		return map[string]domain.VideoPricePlan{}
	}
	families := make([]string, 0, len(in))
	for family := range in {
		families = append(families, family)
	}
	// Deterministic order: two aliases can canonicalize onto the same family and an
	// unordered walk would make the surviving plan vary between processes.
	sort.Strings(families)
	out := make(map[string]domain.VideoPricePlan, len(families))
	for _, family := range families {
		key := NormalizeVideoPricePlanFamilyKey(family)
		if key == "" {
			continue
		}
		plan := in[family]
		if !plan.Mode.IsValid() {
			slog.Warn("video_price_plans_invalid_mode_dropped", "family", family, "mode", string(plan.Mode))
			continue
		}
		normalized := domain.VideoPricePlan{Mode: plan.Mode}
		switch plan.Mode {
		case domain.VideoBillingModePerRequest:
			if plan.PerRequestPrice == nil || *plan.PerRequestPrice < 0 {
				slog.Warn("video_price_plans_per_request_price_missing_dropped", "family", key)
				continue
			}
			price := *plan.PerRequestPrice
			normalized.PerRequestPrice = &price
		case domain.VideoBillingModePerSecond:
			normalized.PerSecondPrices = normalizeVideoPlanPerSecondPrices(key, plan.PerSecondPrices)
			if len(normalized.PerSecondPrices) == 0 {
				continue
			}
		case domain.VideoBillingModePerToken:
			normalized.PerTokenPrices = normalizeVideoPlanPerTokenPrices(key, plan.PerTokenPrices)
			if len(normalized.PerTokenPrices) == 0 {
				continue
			}
		}
		out[key] = normalized
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeVideoPlanPerSecondPrices(family string, in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	tiers := make([]string, 0, len(in))
	for tier := range in {
		tiers = append(tiers, tier)
	}
	sort.Strings(tiers)
	out := make(map[string]float64, len(tiers))
	for _, rawTier := range tiers {
		price := in[rawTier]
		if price < 0 {
			continue
		}
		tier, ok := LookupVideoBillingResolution(rawTier)
		if !ok {
			slog.Warn("video_price_plans_unknown_resolution_dropped",
				"family", family, "resolution", rawTier, "mode", "per_second")
			continue
		}
		out[tier] = price
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeVideoPlanPerTokenPrices(family string, in map[string]domain.VideoTokenPrice) map[string]domain.VideoTokenPrice {
	if len(in) == 0 {
		return nil
	}
	tiers := make([]string, 0, len(in))
	for tier := range in {
		tiers = append(tiers, tier)
	}
	sort.Strings(tiers)
	out := make(map[string]domain.VideoTokenPrice, len(tiers))
	for _, rawTier := range tiers {
		tier, ok := LookupVideoBillingResolution(rawTier)
		if !ok {
			slog.Warn("video_price_plans_unknown_resolution_dropped",
				"family", family, "resolution", rawTier, "mode", "per_token")
			continue
		}
		price := in[rawTier]
		normalized := domain.VideoTokenPrice{}
		if price.WithoutInputVideo != nil && *price.WithoutInputVideo >= 0 {
			v := *price.WithoutInputVideo
			normalized.WithoutInputVideo = &v
		}
		if price.WithInputVideo != nil && *price.WithInputVideo >= 0 {
			v := *price.WithInputVideo
			normalized.WithInputVideo = &v
		}
		if normalized.WithoutInputVideo == nil && normalized.WithInputVideo == nil {
			continue
		}
		out[tier] = normalized
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// VideoModelHasBundledVideoTariff reports whether the model carries a bundled
// per-second tariff with a documented upstream default (today only xAI Grok
// Imagine). Models without one must not inherit Grok defaults: their duration and
// resolution are operator/upstream facts, and guessing them mis-bills.
func VideoModelHasBundledVideoTariff(model string) bool {
	_, ok := getDefaultGrokImagineVideoPrice(model, VideoBillingResolution480P)
	return ok
}

// SeedanceLegacyPerSecondPlan derives a per_second plan from the legacy
// per-model / flat video price columns for a Seedance model.
//
// Why this exists (B-4): GroupHasSeedanceVideoPrice lets a group with only those
// legacy slots pass the create-time gate. If settlement could not price them the
// task would be accepted and then recorded at zero cost, which is strictly worse
// than rejecting it. Synthesizing the per_second plan keeps "passes the gate" and
// "can settle" the same set of configurations. A nil result means "no usable
// slot", and the caller must fail closed.
func SeedanceLegacyPerSecondPlan(group *Group, model string) *domain.VideoPricePlan {
	if group == nil || CanonicalVideoPriceFamily(model) != VideoPriceFamilySeedance {
		return nil
	}
	prices := map[string]float64{}
	normalized := NormalizeVideoModelPrices(group.VideoModelPrices)
	for _, tier := range VideoBillingResolutionTiers {
		if price := LookupVideoModelPrice(normalized, VideoPriceFamilySeedance, tier); price != nil {
			prices[tier] = *price
		}
	}
	if group.VideoPrice480P != nil && *group.VideoPrice480P >= 0 {
		prices[VideoBillingResolution480P] = *group.VideoPrice480P
	}
	if group.VideoPrice720P != nil && *group.VideoPrice720P >= 0 {
		prices[VideoBillingResolution720P] = *group.VideoPrice720P
	}
	if group.VideoPrice1080P != nil && *group.VideoPrice1080P >= 0 {
		prices[VideoBillingResolution1080P] = *group.VideoPrice1080P
	}
	if len(prices) == 0 {
		return nil
	}
	return &domain.VideoPricePlan{Mode: domain.VideoBillingModePerSecond, PerSecondPrices: prices}
}
// ---- PRD-06/K-4 video price plans (per_request / per_second / per_token) ----

// VideoPricePlanForGroup resolves the operator-set video billing plan for a
// request model from groups.video_price_plans (service.Group.VideoPricePlans).
//
// Fail closed: it returns nil when the model maps to no known price family, the
// family has no plan, or the plan declares a mode outside the three supported
// ones. A nil result means "not priceable" and callers must reject the request
// rather than fall back to the image tariff, the flat video_price_* columns, or
// another model's plan.
func VideoPricePlanForGroup(group *Group, model string) *domain.VideoPricePlan {
	if group == nil || len(group.VideoPricePlans) == 0 {
		return nil
	}
	family := CanonicalVideoPriceFamily(model)
	if family == "" {
		// Operators may also key a plan by the raw model id; only an exact,
		// already-normalized key is accepted (no fuzzy guessing).
		family = strings.ToLower(strings.TrimSpace(model))
	}
	if family == "" {
		return nil
	}
	plan, ok := group.VideoPricePlans[family]
	if !ok {
		return nil
	}
	if !plan.Mode.IsValid() {
		slog.Warn("video_price_plan_invalid_mode",
			"model", model,
			"family", family,
			"mode", string(plan.Mode))
		return nil
	}
	return &plan
}

// SeedanceCompletionRequiresTokens reports whether settling one Ark Seedance task
// needs usage.completion_tokens.
//
// Only the per_token mode reconciles on tokens. per_request and per_second plans
// are priced from the request/status facts, so a task must not be dropped just
// because the upstream omitted the token figure. Without any plan the legacy token
// settlement is preserved exactly, which does require the token usage.
func SeedanceCompletionRequiresTokens(group *Group, model string) bool {
	plan := VideoPricePlanForGroup(group, model)
	if plan == nil {
		return true
	}
	return plan.Mode == domain.VideoBillingModePerToken
}

// GroupHasVideoPricePlan reports whether the group declares a usable plan for the
// model (valid mode plus at least one priced unit). It is the fail-closed gate
// used before an upstream request is issued.
func GroupHasVideoPricePlan(group *Group, model string) bool {
	plan := VideoPricePlanForGroup(group, model)
	if plan == nil {
		return false
	}
	return VideoPlanHasAnyPrice(plan)
}

// VideoPlanHasAnyPrice reports whether any tier of the plan carries a price for
// its declared mode. A plan whose mode is set but whose prices are all missing is
// treated as unconfigured, never as free.
func VideoPlanHasAnyPrice(plan *domain.VideoPricePlan) bool {
	if plan == nil || !plan.Mode.IsValid() {
		return false
	}
	switch plan.Mode {
	case domain.VideoBillingModePerRequest:
		return plan.PerRequestPrice != nil && *plan.PerRequestPrice >= 0
	case domain.VideoBillingModePerSecond:
		for _, price := range plan.PerSecondPrices {
			if price >= 0 {
				return true
			}
		}
		return false
	case domain.VideoBillingModePerToken:
		for _, price := range plan.PerTokenPrices {
			if price.WithInputVideo != nil && *price.WithInputVideo >= 0 {
				return true
			}
			if price.WithoutInputVideo != nil && *price.WithoutInputVideo >= 0 {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// VideoPlanPerRequestUnitPrice returns the per-request unit price, or nil when the
// plan is not a per-request plan / has no price configured.
func VideoPlanPerRequestUnitPrice(plan *domain.VideoPricePlan) *float64 {
	if plan == nil || plan.Mode != domain.VideoBillingModePerRequest || plan.PerRequestPrice == nil {
		return nil
	}
	if *plan.PerRequestPrice < 0 {
		return nil
	}
	price := *plan.PerRequestPrice
	return &price
}

// VideoPlanPerSecondUnitPrice returns the per-second unit price for a resolution
// tier. Missing or negative tiers return nil: a 1080p request must never be
// silently billed at the 720p rate.
func VideoPlanPerSecondUnitPrice(plan *domain.VideoPricePlan, resolution string) *float64 {
	if plan == nil || plan.Mode != domain.VideoBillingModePerSecond {
		return nil
	}
	tier := NormalizeVideoBillingResolutionOrDefault(resolution)
	price, ok := plan.PerSecondPrices[tier]
	if !ok || price < 0 {
		return nil
	}
	return &price
}

// VideoPlanTokenUnitPrice returns the per-million-token unit price for the
// (resolution, input-contains-video) tier. Ark prices these two dimensions
// independently, so a missing cell fails closed instead of borrowing the other
// cell or another resolution.
func VideoPlanTokenUnitPrice(plan *domain.VideoPricePlan, resolution string, hasInputVideo bool) *float64 {
	if plan == nil || plan.Mode != domain.VideoBillingModePerToken {
		return nil
	}
	tier := NormalizeVideoBillingResolutionOrDefault(resolution)
	prices, ok := plan.PerTokenPrices[tier]
	if !ok {
		return nil
	}
	price := prices.WithoutInputVideo
	if hasInputVideo {
		price = prices.WithInputVideo
	}
	if price == nil || *price < 0 {
		return nil
	}
	value := *price
	return &value
}
