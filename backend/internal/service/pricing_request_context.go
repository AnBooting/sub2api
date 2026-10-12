package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// PriceScope identifies a customer sell price, not an account purchase cost.
// AccountID is zero until a real customer account-price override is supported.
type PriceScope struct {
	GroupID      int64
	BillingModel string
	ChannelID    int64
	AccountID    int64
}

type requestBillingCandidate struct {
	group                 *Group
	multiplier            float64
	tokenMultiplier       float64
	mapping               ChannelMappingResult
	allowedUpstreamModels []string
	accountUpstreamModels map[int64]string
	accountLongContext    map[int64]bool
}

// RequestBillingContext owns a complete frozen pricing/mapping bundle. No live
// repository or group pointer escapes it; execution authorization is not frozen.
type RequestBillingContext struct {
	requestID        string
	startedAt        time.Time
	requestedGroupID *int64
	requestedModel   string
	normalGroupIDs   []int64
	haGroupIDs       []int64
	candidates       map[int64]*requestBillingCandidate
	billing          *BillingService
	resolver         *ModelPricingResolver
	version          string
}

func (r *RequestBillingContext) RequestID() string {
	if r == nil {
		return ""
	}
	return r.requestID
}
func (r *RequestBillingContext) StartedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.startedAt
}
func (r *RequestBillingContext) RequestedGroupID() *int64 {
	if r == nil {
		return nil
	}
	return cloneGroupValuePointer(r.requestedGroupID)
}
func (r *RequestBillingContext) NormalGroupIDs() []int64 {
	if r == nil {
		return nil
	}
	return append([]int64(nil), r.normalGroupIDs...)
}
func (r *RequestBillingContext) HAGroupIDs() []int64 {
	if r == nil {
		return nil
	}
	return append([]int64(nil), r.haGroupIDs...)
}

// LockedRequestPricing is opaque. It never returns its mutable resolver, group,
// resolved ladder or catalog. Calculate only accepts usage/forwarding facts.
type LockedRequestPricing struct {
	request      *RequestBillingContext
	scope        PriceScope
	candidate    *requestBillingCandidate
	resolved     *ResolvedPricing
	availability error
	multiplier   float64
}

func (p *LockedRequestPricing) Availability() error {
	if p == nil {
		return ErrModelPricingUnavailable
	}
	return p.availability
}
func (p *LockedRequestPricing) Source() string {
	if p == nil || p.resolved == nil {
		return ""
	}
	return p.resolved.Source
}
func (p *LockedRequestPricing) Version() string {
	if p == nil || p.request == nil {
		return ""
	}
	return p.request.version
}
func (p *LockedRequestPricing) EffectiveMultiplier() float64 {
	if p == nil {
		return 0
	}
	return p.multiplier
}

// BaseMultiplier 返回冻结 bundle 的基础费率倍率（不含高峰因子），与 legacy
// 按次媒体/搜索/音频的口径一致（这些维度不叠加高峰因子）。
func (p *LockedRequestPricing) BaseMultiplier() float64 {
	if p == nil || p.candidate == nil {
		return 0
	}
	return p.candidate.multiplier
}

// Mode 返回冻结解析结果的计费模式（token/per_request/image/video）。
// 按次计数维度据此判断该走冻结按次卡还是 token 计费。
func (p *LockedRequestPricing) Mode() BillingMode {
	if p == nil || p.resolved == nil {
		return ""
	}
	return p.resolved.Mode
}

// FrozenBilling 返回请求冻结时点的计费服务快照，仅供锁价消费者做纯算术计费
// （WebSearch/Search/Audio 等按次公式）。快照在请求建立时克隆，不随实时目录
// 或分组变更；调用方不得用它做任何仓储回源取价。
func (p *LockedRequestPricing) FrozenBilling() *BillingService {
	if p == nil || p.request == nil {
		return nil
	}
	return p.request.billing
}
func (p *LockedRequestPricing) Period() DeepSeekPricingPeriod {
	if p == nil || p.request == nil {
		return DeepSeekPricingPeriod{}
	}
	if !isDeepSeekModel(p.scope.BillingModel) {
		return DeepSeekPricingPeriod{}
	}
	return p.request.billing.deepSeekPricingPeriodAt(p.request.startedAt)
}
func (p *LockedRequestPricing) Calculate(input CostInput) (*CostBreakdown, error) {
	if err := p.Availability(); err != nil {
		return nil, err
	}
	input.Ctx = context.Background()
	input.Model = p.scope.BillingModel
	input.GroupID = nil
	if p.scope.GroupID != 0 {
		id := p.scope.GroupID
		input.GroupID = &id
	}
	input.Group = p.candidate.group
	input.PricingAt = p.request.startedAt
	input.RateMultiplier = p.multiplier
	input.Resolver = p.request.resolver
	input.Resolved = p.resolved
	// Caller-supplied live account switches cannot replace request-start rules.
	accountPolicy := false
	if input.SelectedAccountID != 0 {
		policy, known := p.candidate.accountLongContext[input.SelectedAccountID]
		if !known {
			return nil, fmt.Errorf("%w: selected account policy was not frozen", ErrModelPricingUnavailable)
		}
		accountPolicy = policy
	}
	input.LongContextBillingEnabled = &accountPolicy
	cost, err := p.request.billing.CalculateCostUnified(input)
	if err != nil {
		return nil, err
	}
	if cost != nil {
		cost.PricingSnapshot = cloneRequestModelPricing(cost.PricingSnapshot)
	}
	return cost, nil
}

func (r *RequestBillingContext) PricingFor(scope PriceScope) (*LockedRequestPricing, error) {
	if r == nil {
		return nil, ErrModelPricingUnavailable
	}
	candidate, ok := r.candidates[scope.GroupID]
	if !ok || scope.AccountID != 0 || scope.ChannelID != candidate.mapping.ChannelID {
		return nil, fmt.Errorf("%w: price scope was not frozen", ErrModelPricingUnavailable)
	}
	scope.BillingModel = strings.TrimSpace(scope.BillingModel)
	if scope.BillingModel == "" {
		return nil, fmt.Errorf("%w: empty billing model", ErrModelPricingUnavailable)
	}
	if !r.billingModelAllowed(candidate, scope.BillingModel) {
		return nil, fmt.Errorf("%w: model is outside frozen billing source", ErrModelPricingUnavailable)
	}
	var groupID *int64
	if scope.GroupID != 0 {
		id := scope.GroupID
		groupID = &id
	}
	resolved := r.resolver.Resolve(context.Background(), PricingInput{Model: scope.BillingModel, GroupID: groupID, Group: candidate.group})
	locked := &LockedRequestPricing{request: r, scope: scope, candidate: candidate, resolved: resolved, multiplier: candidate.multiplier}
	if resolved != nil && (resolved.Mode == "" || resolved.Mode == BillingModeToken) {
		locked.multiplier = candidate.tokenMultiplier
	}
	locked.availability = requestResolvedPricingAvailability(r.billing, scope.BillingModel, resolved)
	return locked, nil
}

// MappingFor returns request-start customer mapping and source POD values.
func (r *RequestBillingContext) MappingFor(groupID int64) (ChannelMappingResult, error) {
	if r == nil {
		return ChannelMappingResult{}, ErrModelPricingUnavailable
	}
	candidate, ok := r.candidates[groupID]
	if !ok {
		return ChannelMappingResult{}, fmt.Errorf("%w: unknown mapping group", ErrModelPricingUnavailable)
	}
	return candidate.mapping, nil
}

// UpstreamModelFor locks the selected account's exact model relationship,
// not merely membership in the group's allowed result set.
func (r *RequestBillingContext) UpstreamModelFor(groupID, accountID int64) (string, error) {
	if r == nil {
		return "", ErrModelPricingUnavailable
	}
	candidate, ok := r.candidates[groupID]
	if !ok {
		return "", fmt.Errorf("%w: unknown upstream group", ErrModelPricingUnavailable)
	}
	model, ok := candidate.accountUpstreamModels[accountID]
	if !ok {
		return "", fmt.Errorf("%w: account mapping was not frozen", ErrModelPricingUnavailable)
	}
	return model, nil
}

func (r *RequestBillingContext) billingModelAllowed(candidate *requestBillingCandidate, model string) bool {
	equal := func(a, b string) bool {
		return normalizeChannelPricingModelName(a) == normalizeChannelPricingModelName(b)
	}
	switch candidate.mapping.BillingModelSource {
	case BillingModelSourceRequested:
		return equal(model, r.requestedModel)
	case BillingModelSourceUpstream:
		return requestUpstreamModelAllowed(candidate, model)
	case BillingModelSourceResponse:
		return equal(model, candidate.mapping.MappedModel) || r.strictModelIdentified(candidate, model)
	default:
		baseline := candidate.mapping.MappedModel
		if baseline == "" {
			baseline = r.requestedModel
		}
		return equal(model, baseline)
	}
}

// ScopeFor is the sole frozen source policy decision. Actual model declarations
// are facts from forwarding; they cannot change the requested model or rules.
func (r *RequestBillingContext) ScopeFor(groupID int64, upstreamModel, responseModel string) (PriceScope, error) {
	mapping, err := r.MappingFor(groupID)
	if err != nil {
		return PriceScope{}, err
	}
	candidate := r.candidates[groupID]
	source := mapping.BillingModelSource
	model := r.requestedModel
	switch source {
	case BillingModelSourceRequested:
	case BillingModelSourceUpstream:
		upstreamModel = strings.TrimSpace(upstreamModel)
		if !requestUpstreamModelAllowed(candidate, upstreamModel) {
			return PriceScope{}, fmt.Errorf("%w: upstream model was not frozen", ErrModelPricingUnavailable)
		}
		model = upstreamModel
	case BillingModelSourceResponse:
		// Response model is unknowable before forwarding. Empty uses the frozen
		// baseline for preflight, nonempty must have a strictly identified card.
		if responseModel = strings.TrimSpace(responseModel); responseModel != "" {
			if !r.strictModelIdentified(candidate, responseModel) {
				return PriceScope{}, fmt.Errorf("%w: response model lacks frozen identified pricing", ErrModelPricingUnavailable)
			}
			model = responseModel
		} else if mapping.MappedModel != "" {
			model = mapping.MappedModel
		}
	default:
		if mapping.MappedModel != "" {
			model = mapping.MappedModel
		}
	}
	scope := PriceScope{GroupID: groupID, BillingModel: model, ChannelID: mapping.ChannelID}
	locked, err := r.PricingFor(scope)
	if err != nil {
		return PriceScope{}, err
	}
	if err = locked.Availability(); err != nil {
		return PriceScope{}, err
	}
	return scope, nil
}

func requestUpstreamModelAllowed(candidate *requestBillingCandidate, model string) bool {
	if model == "" {
		return false
	}
	if model == candidate.mapping.MappedModel {
		return true
	}
	for _, allowed := range candidate.allowedUpstreamModels {
		if model == allowed {
			return true
		}
	}
	return false
}
func (r *RequestBillingContext) strictModelIdentified(candidate *requestBillingCandidate, model string) bool {
	if matchGroupModelPricing(candidate.group, model) != nil {
		return true
	}
	if channels := r.resolver.channelService; channels != nil && candidate.mapping.ChannelID != 0 {
		if channels.GetChannelModelPricing(context.Background(), candidate.group.ID, model) != nil {
			return true
		}
	}
	if pricing := r.billing.pricingService; pricing != nil {
		if pricing.RequiresNativeCNY(model) {
			_, ok := pricing.nativeCNYCards[nativeModelKey(model)]
			return ok
		}
	}
	return r.billing.HasIdentifiedTokenPricing(model)
}

func requestResolvedPricingAvailability(billing *BillingService, model string, resolved *ResolvedPricing) error {
	missing := func() error {
		return fmt.Errorf("%w: no frozen customer price for %s", ErrModelPricingUnavailable, model)
	}
	if resolved == nil {
		return missing()
	}
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo {
		if resolved.channelPricing == nil {
			return missing()
		}
		base := resolved.channelPricing.PerRequestPrice
		if base != nil {
			if err := validRequestPrice(*base); err != nil {
				return err
			}
		}
		for _, tier := range resolved.RequestTiers {
			price := tier.PerRequestPrice
			if price == nil {
				price = base
			}
			if price == nil {
				return missing()
			}
			if err := validRequestPrice(*price); err != nil {
				return err
			}
		}
		if base != nil || len(resolved.RequestTiers) > 0 {
			return nil
		}
		return missing()
	}
	price := resolved.BasePricing
	if price == nil {
		return missing()
	}
	if price.tokenPricingError != nil {
		return price.tokenPricingError
	}
	if err := tokenPriceCompletenessError(price); err != nil {
		return err
	}
	if price.NativeCard != nil {
		return nil
	}
	if resolved.channelPricing != nil && resolved.channelPricing.InputPrice != nil && resolved.channelPricing.OutputPrice != nil {
		return manualTokenPricingError(resolved.channelPricing)
	}
	if !billing.HasIdentifiedTokenPricing(model) {
		return missing()
	}
	return nil
}
func validRequestPrice(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("%w: invalid per-request price", ErrModelPricingUnavailable)
	}
	return nil
}

func (d inflightEstimateDeps) prepareRequestBilling(ctx context.Context, key *APIKey, req InflightEstimateRequest) (context.Context, *RequestBillingContext, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if key == nil || d.billing == nil || strings.TrimSpace(req.Model) == "" {
		return ctx, nil, fmt.Errorf("%w: cannot freeze missing request pricing inputs", ErrModelPricingUnavailable)
	}
	// Scope zero is synthesized only for a truly ungrouped legacy key. It is
	// never accepted as an explicit legacy group id or a configured chain link.
	if key.GroupID != nil && *key.GroupID <= 0 {
		return ctx, nil, fmt.Errorf("%w: explicit group id must be positive", ErrModelPricingUnavailable)
	}
	for _, link := range key.Groups {
		if link.GroupID <= 0 {
			return ctx, nil, fmt.Errorf("%w: configured group id must be positive", ErrModelPricingUnavailable)
		}
	}
	if previous := GatewayRequestBillingFromContext(ctx); previous != nil {
		return ctx, previous, nil
	}
	startedAt := timezone.Now()
	billing := cloneRequestBillingService(d.billing)
	var liveChannels *ChannelService
	if d.resolver != nil {
		liveChannels = d.resolver.channelService
	}
	channels, err := cloneRequestChannelService(ctx, liveChannels)
	if err != nil {
		return ctx, nil, err
	}
	resolver := NewModelPricingResolver(channels, billing)
	resolver.pricingAt = startedAt
	request := &RequestBillingContext{requestID: "locked:" + generateRequestID(), startedAt: startedAt, requestedGroupID: cloneGroupValuePointer(key.GroupID), requestedModel: strings.TrimSpace(req.Model), candidates: map[int64]*requestBillingCandidate{}, billing: billing, resolver: resolver}
	links := CloneAPIKeyGroupLinks(key.Groups)
	if len(links) == 0 && key.GroupID != nil {
		links = []APIKeyGroupLink{{GroupID: *key.GroupID, Role: APIKeyGroupRoleNormal, Position: 0, Group: cloneAPIKeyGroup(key.Group)}}
	}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Role == links[j].Role {
			return links[i].Position < links[j].Position
		}
		return links[i].Role == APIKeyGroupRoleNormal
	})
	if len(links) == 0 {
		request.normalGroupIDs = []int64{0}
		links = []APIKeyGroupLink{{Role: APIKeyGroupRoleNormal}}
	}
	for _, link := range links {
		if link.Role != APIKeyGroupRoleNormal && link.Role != APIKeyGroupRoleHA {
			return ctx, nil, fmt.Errorf("%w: invalid candidate role", ErrModelPricingUnavailable)
		}
		if link.GroupID != 0 && (link.Group == nil || link.Group.ID != link.GroupID) {
			return ctx, nil, fmt.Errorf("%w: candidate group snapshot missing", ErrModelPricingUnavailable)
		}
		if _, duplicate := request.candidates[link.GroupID]; duplicate {
			return ctx, nil, fmt.Errorf("%w: duplicate candidate group", ErrModelPricingUnavailable)
		}
		rate := 1.0
		if d.cfg != nil {
			rate = d.cfg.Default.RateMultiplier
		}
		if link.Group != nil {
			rate = link.Group.RateMultiplier
			if key.User != nil && d.userGroupRate != nil {
				rate = d.userGroupRate(ctx, key.User.ID, link.GroupID, rate)
			}
		}
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
			return ctx, nil, fmt.Errorf("%w: invalid candidate multiplier", ErrModelPricingUnavailable)
		}
		mapping := ChannelMappingResult{MappedModel: request.requestedModel}
		if channels != nil && link.GroupID != 0 {
			mapping = channels.ResolveChannelMapping(ctx, link.GroupID, request.requestedModel)
		}
		if link.Group != nil {
			link.Group.AccountGroups = nil
		}
		candidate := &requestBillingCandidate{group: link.Group, multiplier: rate, tokenMultiplier: rate, mapping: mapping}
		if link.Group != nil {
			candidate.tokenMultiplier *= link.Group.PeakMultiplierAt(startedAt)
		}
		// Read only mapped model strings from the scheduler snapshot. No account
		// credential pointer or supplier price is retained in the request bundle.
		if d.accountMappingSnapshot != nil {
			candidateKey := &APIKey{User: key.User, Group: link.Group}
			if link.GroupID != 0 {
				id := link.GroupID
				candidateKey.GroupID = &id
			}
			results, err := d.accountMappingSnapshot(ctx, candidateKey, mapping.MappedModel)
			if err != nil {
				return ctx, nil, err
			}
			candidate.accountUpstreamModels = make(map[int64]string, len(results))
			candidate.accountLongContext = make(map[int64]bool, len(results))
			seen := map[string]bool{}
			for accountID, policy := range results {
				model := policy.Model
				candidate.accountUpstreamModels[accountID] = model
				candidate.accountLongContext[accountID] = policy.LongContextBillingEnabled
				if !seen[model] {
					seen[model] = true
					candidate.allowedUpstreamModels = append(candidate.allowedUpstreamModels, model)
				}
			}
			sort.Strings(candidate.allowedUpstreamModels)
		}
		request.candidates[link.GroupID] = candidate
		if link.GroupID != 0 {
			if link.Role == APIKeyGroupRoleHA {
				request.haGroupIDs = append(request.haGroupIDs, link.GroupID)
			} else {
				request.normalGroupIDs = append(request.normalGroupIDs, link.GroupID)
			}
		}
	}
	request.version, err = requestCatalogVersion(billing, channels, request.candidates)
	if err != nil {
		return ctx, nil, err
	}
	return withGatewayRequestBilling(ctx, request), request, nil
}
