package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	DomesticPricingStatusActive  = "active"
	DomesticPricingStatusPending = "pending_pricing"
	DomesticPricingSourceAdmin   = "admin_override"
)

var (
	ErrDomesticPricingPending     = errors.New("domestic model pricing pending")
	ErrDomesticPricingUnavailable = errors.New("domestic pricing catalog unavailable")
	ErrDomesticPricingInvalid     = errors.New("invalid domestic model pricing")
)

// DomesticPricingEntry is one provider-scoped price. The accounting currency
// remains USD, while built-in domestic entries use mainland price numbers.
// Pending entries deliberately use nil price fields so a missing price can
// never be confused with a legitimate zero-cost model.
type DomesticPricingEntry struct {
	Provider                    string    `json:"provider"`
	Model                       string    `json:"model"`
	InputPricePerToken          *float64  `json:"input_price_per_token"`
	OutputPricePerToken         *float64  `json:"output_price_per_token"`
	CacheReadPricePerToken      *float64  `json:"cache_read_price_per_token"`
	CacheWritePricePerToken     *float64  `json:"cache_write_price_per_token"`
	Currency                    string    `json:"currency"`
	Source                      string    `json:"source"`
	Version                     int64     `json:"version"`
	UpdatedAt                   time.Time `json:"updated_at"`
	Status                      string    `json:"status"`
	Override                    bool      `json:"override"`
	LongContextInputThreshold   int       `json:"long_context_input_threshold,omitempty"`
	LongContextInputMultiplier  float64   `json:"long_context_input_multiplier,omitempty"`
	LongContextOutputMultiplier float64   `json:"long_context_output_multiplier,omitempty"`
	PeakRateMultiplier          float64   `json:"peak_rate_multiplier,omitempty"`
	PeakSchedule                string    `json:"peak_schedule,omitempty"`
}

// DomesticPricingOverrideInput is accepted from the administrator API. Source,
// version, and timestamps are server-owned to preserve an auditable revision
// sequence.
type DomesticPricingOverrideInput struct {
	Provider                string
	Model                   string
	Source                  string
	InputPricePerToken      float64
	OutputPricePerToken     float64
	CacheReadPricePerToken  *float64
	CacheWritePricePerToken *float64
}

type DomesticPricingRepository interface {
	ListOverrides(ctx context.Context) ([]DomesticPricingEntry, error)
	GetOverride(ctx context.Context, provider, model string) (*DomesticPricingEntry, error)
	UpsertOverride(ctx context.Context, input DomesticPricingOverrideInput) (*DomesticPricingEntry, error)
	DeleteOverride(ctx context.Context, provider, model string) (bool, error)
}

type DomesticPricingService struct {
	repo DomesticPricingRepository
	now  func() time.Time
}

func NewDomesticPricingService(repo DomesticPricingRepository) *DomesticPricingService {
	return &DomesticPricingService{repo: repo, now: time.Now}
}

func normalizeDomesticPricingProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func isDomesticPricingProvider(provider string) bool {
	_, ok := supportedDomesticProviders[normalizeDomesticPricingProvider(provider)]
	return ok
}

func normalizeDomesticModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func domesticPricingKey(provider, model string) string {
	return normalizeDomesticPricingProvider(provider) + "\x00" + normalizeDomesticModel(model)
}

func usdPerToken(perMillion float64) *float64 {
	v := perMillion / 1_000_000
	return &v
}

func domesticNumericPrice(provider, model string, input, output float64, cacheRead, cacheWrite *float64, source string) DomesticPricingEntry {
	entry := DomesticPricingEntry{
		Provider:            provider,
		Model:               model,
		InputPricePerToken:  usdPerToken(input),
		OutputPricePerToken: usdPerToken(output),
		Currency:            "USD",
		Source:              source,
		Version:             20260809,
		UpdatedAt:           time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC),
		Status:              DomesticPricingStatusActive,
	}
	if cacheRead != nil {
		entry.CacheReadPricePerToken = usdPerToken(*cacheRead)
	}
	if cacheWrite != nil {
		entry.CacheWritePricePerToken = usdPerToken(*cacheWrite)
	}
	return entry
}

// Keep USD as the accounting currency, but use the domestic provider's
// published mainland numeric price for the provider-scoped catalog. Values are
// intentionally stored directly; no exchange-rate or multiplier is applied.
func officialDomesticPrice(provider, model string, input, output float64, cacheRead, cacheWrite *float64, source string) DomesticPricingEntry {
	return domesticNumericPrice(provider, model, input, output, cacheRead, cacheWrite, source)
}

func officialDeepSeekPrice(model string, input, output, cacheRead float64, source string) DomesticPricingEntry {
	entry := officialDomesticPrice(DomesticProviderDeepSeek, model, input, output, floatPtr(cacheRead), nil, source)
	entry.Version = 20260911
	entry.UpdatedAt = time.Date(2026, time.September, 11, 0, 0, 0, 0, time.UTC)
	entry.PeakRateMultiplier = 2
	entry.PeakSchedule = "工作日 09:00-12:00、14:00-18:00（北京时间）"
	return entry
}

func pendingDomesticPrice(provider, model, source string) DomesticPricingEntry {
	return DomesticPricingEntry{
		Provider:  provider,
		Model:     model,
		Currency:  "USD",
		Source:    source,
		Version:   20260809,
		UpdatedAt: time.Date(2026, time.August, 9, 0, 0, 0, 0, time.UTC),
		Status:    DomesticPricingStatusPending,
	}
}

func floatPtr(v float64) *float64 { return &v }

// builtInDomesticPricing contains the domestic-provider defaults shown while
// creating a group. The billing schema remains USD-based, but active entries
// use mainland numeric price values. Models that cannot be represented by the
// current flat-token schema stay pending until an administrator supplies an
// explicit override.
//
// Active entries follow the provider's mainland China official price sheet,
// with numbers used verbatim as USD (site currency policy: 1 CNY = 1 USD).
// DeepSeek base values are off-peak prices; billing applies the published peak
// schedule and multiplier at request time.
func builtInDomesticPricing() []DomesticPricingEntry {
	deepSeekSource := "https://api-docs.deepseek.com/zh-cn/quick_start/pricing"
	qwenSource := "https://www.alibabacloud.com/help/en/model-studio/model-pricing"
	bytePlusSource := "https://docs.byteplus.com/en/docs/ModelArk/1924470"
	kimiSource := "https://platform.kimi.com/docs/pricing"
	zhipuSource := "https://docs.z.ai/guides/overview/pricing"
	miniMaxSource := "https://platform.minimaxi.com/docs/guides/pricing-paygo.md"
	stepFunSource := "https://platform.stepfun.ai/docs/en/guides/pricing/details.md"

	entries := []DomesticPricingEntry{
		officialDeepSeekPrice("deepseek-v4-pro", 4.50, 13.50, 0.15, deepSeekSource),
		officialDeepSeekPrice("deepseek-flash", 1.00, 4.00, 0.02, deepSeekSource),

		officialDomesticPrice("qwen", "qwen-max", 11.52, 46.08, nil, nil, qwenSource),
		officialDomesticPrice("qwen", "qwen3.7-max", 18.00, 54.00, nil, nil, qwenSource),
		pendingDomesticPrice("qwen", "qwen3.7-plus", qwenSource+"#tiered-pricing"),
		pendingDomesticPrice("qwen", "qwen3.6-flash", qwenSource+"#tiered-pricing"),

		officialDomesticPrice("doubao", "skylark-pro", 2.88, 11.52, floatPtr(0.576), nil, bytePlusSource),
		pendingDomesticPrice("doubao", "seed-1.6", bytePlusSource+"#tiered-pricing"),
		pendingDomesticPrice("doubao", "seed-1.6-flash", bytePlusSource+"#tiered-pricing"),

		officialDomesticPrice("kimi", "kimi-k2.5", 4.32, 21.60, floatPtr(0.72), nil, kimiSource+"/chat-k25"),
		officialDomesticPrice("kimi", "kimi-k2.6", 6.84, 28.80, floatPtr(1.152), nil, kimiSource+"/chat-k26"),
		officialDomesticPrice("kimi", "kimi-k2.7-code", 6.50, 27.00, floatPtr(1.30), nil, kimiSource+"/chat-k27-code"),
		officialDomesticPrice("kimi", "kimi-k2.7-code-highspeed", 13.00, 54.00, floatPtr(2.60), nil, kimiSource+"/chat-k27-code"),
		domesticNumericPrice("kimi", "kimi-k3", 20.00, 100.00, floatPtr(2.00), nil, "https://platform.kimi.com/docs/pricing/chat-k3"),

		// GLM uses the mainland China official price sheet (open.bigmodel.cn/pricing).
		// Flash uses list price (the two-week 50%-off promo is not modeled).
		// GLM-5.1/5/5-Turbo use the [32k+) input tier (long-context typical usage).
		// glm-5.3-flashx is not on the mainland sheet; Z.AI catalog retained.
		domesticNumericPrice("zhipu", "glm-5.3-flashx", 1.00, 3.20, floatPtr(0.50), nil, zhipuSource+"#site-sku"),
		domesticNumericPrice("zhipu", "glm-5.3-flash", 0.80, 2.80, floatPtr(0.23), nil, "https://open.bigmodel.cn/pricing"),
		domesticNumericPrice("zhipu", "glm-5.3", 8.00, 28.00, floatPtr(2.00), nil, "https://open.bigmodel.cn/pricing"),
		domesticNumericPrice("zhipu", "glm-5.2", 8.00, 28.00, floatPtr(2.00), nil, "https://open.bigmodel.cn/pricing"),
		domesticNumericPrice("zhipu", "glm-5.1", 8.00, 28.00, floatPtr(2.00), nil, "https://open.bigmodel.cn/pricing"),
		domesticNumericPrice("zhipu", "glm-5", 6.00, 22.00, floatPtr(1.50), nil, "https://open.bigmodel.cn/pricing"),
		domesticNumericPrice("zhipu", "glm-5-turbo", 7.00, 26.00, floatPtr(1.80), nil, "https://open.bigmodel.cn/pricing"),
		domesticNumericPrice("zhipu", "glm-4.7", 0.60, 2.20, floatPtr(0.11), nil, zhipuSource),
		domesticNumericPrice("zhipu", "glm-4.7-flashx", 0.07, 0.40, floatPtr(0.01), nil, zhipuSource),
		domesticNumericPrice("zhipu", "glm-4.6", 0.60, 2.20, floatPtr(0.11), nil, zhipuSource),
		domesticNumericPrice("zhipu", "glm-4.5", 0.60, 2.20, floatPtr(0.11), nil, zhipuSource),
		domesticNumericPrice("zhipu", "glm-4.5-x", 2.20, 8.90, floatPtr(0.45), nil, zhipuSource),
		domesticNumericPrice("zhipu", "glm-4.5-air", 0.20, 1.10, floatPtr(0.03), nil, zhipuSource),
		domesticNumericPrice("zhipu", "glm-4.5-airx", 1.10, 4.50, floatPtr(0.22), nil, zhipuSource),
		pendingDomesticPrice("zhipu", "glm-4.7-flash", zhipuSource+"#free-model-not-zero-billable"),

		officialDomesticPrice("minimax", "minimax-m2.7", 2.10, 8.40, floatPtr(0.42), floatPtr(2.625), miniMaxSource),
		officialDomesticPrice("minimax", "minimax-m2.7-highspeed", 4.20, 16.80, floatPtr(0.42), floatPtr(2.625), miniMaxSource),
		officialDomesticPrice("minimax", "minimax-m2.5", 2.10, 8.40, floatPtr(0.21), floatPtr(2.625), miniMaxSource),
		officialDomesticPrice("minimax", "minimax-m2.5-highspeed", 4.20, 16.80, floatPtr(0.21), floatPtr(2.625), miniMaxSource),
		officialDomesticPrice("minimax", "minimax-m2.1", 2.10, 8.40, floatPtr(0.21), floatPtr(2.625), miniMaxSource),
		officialDomesticPrice("minimax", "minimax-m2.1-highspeed", 4.20, 16.80, floatPtr(0.21), floatPtr(2.625), miniMaxSource),
		officialDomesticPrice("minimax", "minimax-m2", 2.10, 8.40, floatPtr(0.21), floatPtr(2.625), miniMaxSource),
	}
	miniMaxM3 := officialDomesticPrice("minimax", "minimax-m3", 2.10, 8.40, floatPtr(0.42), nil, miniMaxSource)
	miniMaxM3.LongContextInputThreshold = 512_000
	miniMaxM3.LongContextInputMultiplier = 2
	miniMaxM3.LongContextOutputMultiplier = 2
	entries = append(entries, miniMaxM3)

	entries = append(entries,
		officialDomesticPrice("stepfun", "step-3.7-flash", 1.44, 8.28, floatPtr(0.288), nil, stepFunSource),
		officialDomesticPrice("stepfun", "step-3.5-flash", 0.72, 2.16, floatPtr(0.144), nil, stepFunSource),
		officialDomesticPrice("stepfun", "step-3.5-flash-2603", 0.72, 2.16, floatPtr(0.144), nil, stepFunSource),

		// These providers do not currently publish a direct international USD
		// rate that this service can verify. They remain visible but unbillable.
		pendingDomesticPrice("baichuan", "baichuan-m3-plus", "https://platform.baichuan-ai.com/prices"),
		pendingDomesticPrice("yi", "yi-large", "https://platform.lingyiwanwu.com/"),
		pendingDomesticPrice("custom", "*", "https://platform.openai.com/docs/api-reference/introduction"),
	)
	return entries
}

func validateDomesticPricingEntry(entry *DomesticPricingEntry) error {
	if entry == nil || !isDomesticPricingProvider(entry.Provider) || normalizeDomesticModel(entry.Model) == "" {
		return ErrDomesticPricingInvalid
	}
	if !isOfficialPricingURL(entry.Source) {
		return fmt.Errorf("%w: source must be an official http(s) pricing URL", ErrDomesticPricingInvalid)
	}
	if entry.Status != DomesticPricingStatusActive {
		return fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingPending, entry.Provider, entry.Model)
	}
	if entry.InputPricePerToken == nil || entry.OutputPricePerToken == nil ||
		!isFinitePositive(*entry.InputPricePerToken) || !isFinitePositive(*entry.OutputPricePerToken) {
		return fmt.Errorf("%w: input and output prices must be positive", ErrDomesticPricingInvalid)
	}
	if !isFiniteOptionalPositive(entry.CacheReadPricePerToken) || !isFiniteOptionalPositive(entry.CacheWritePricePerToken) {
		return fmt.Errorf("%w: configured cache prices must be positive", ErrDomesticPricingInvalid)
	}
	return nil
}

func isFinitePositive(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func isFiniteOptionalPositive(v *float64) bool {
	return v == nil || isFinitePositive(*v)
}

func isOfficialPricingURL(raw string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	return err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

func validateDomesticPricingOverride(input DomesticPricingOverrideInput) (DomesticPricingOverrideInput, error) {
	input.Provider = normalizeDomesticPricingProvider(input.Provider)
	input.Model = normalizeDomesticModel(input.Model)
	if !isDomesticPricingProvider(input.Provider) {
		return input, fmt.Errorf("%w: unknown provider %q", ErrDomesticPricingInvalid, input.Provider)
	}
	if input.Model == "" || input.Model == "*" {
		return input, fmt.Errorf("%w: concrete model is required", ErrDomesticPricingInvalid)
	}
	input.Source = strings.TrimSpace(input.Source)
	if !isOfficialPricingURL(input.Source) {
		return input, fmt.Errorf("%w: source must be an official http(s) pricing URL", ErrDomesticPricingInvalid)
	}
	if !isFinitePositive(input.InputPricePerToken) || !isFinitePositive(input.OutputPricePerToken) {
		return input, fmt.Errorf("%w: input and output prices must be positive USD/token values", ErrDomesticPricingInvalid)
	}
	if !isFiniteOptionalPositive(input.CacheReadPricePerToken) || !isFiniteOptionalPositive(input.CacheWritePricePerToken) {
		return input, fmt.Errorf("%w: configured cache prices must be positive USD/token values", ErrDomesticPricingInvalid)
	}
	return input, nil
}

func (s *DomesticPricingService) ListCatalog(ctx context.Context) ([]DomesticPricingEntry, error) {
	byKey := make(map[string]DomesticPricingEntry)
	for _, entry := range builtInDomesticPricing() {
		byKey[domesticPricingKey(entry.Provider, entry.Model)] = entry
	}
	if s != nil && s.repo != nil {
		overrides, err := s.repo.ListOverrides(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDomesticPricingUnavailable, err)
		}
		for _, override := range overrides {
			override.Provider = normalizeDomesticPricingProvider(override.Provider)
			override.Model = normalizeDomesticModel(override.Model)
			override.Currency = "USD"
			override.Status = DomesticPricingStatusActive
			override.Override = true
			if err := validateDomesticPricingEntry(&override); err != nil {
				return nil, fmt.Errorf("invalid persisted domestic pricing override %s/%s: %w", override.Provider, override.Model, err)
			}
			byKey[domesticPricingKey(override.Provider, override.Model)] = override
		}
	}

	result := make([]DomesticPricingEntry, 0, len(byKey))
	for _, entry := range byKey {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		return result[i].Model < result[j].Model
	})
	return result, nil
}

// Resolve returns only an exact provider+model match. It never consults the
// generic LiteLLM/OpenAI catalog and therefore cannot silently apply another
// provider's price to a domestic model with the same or similar name.
// domesticModelAliases 是国产 provider 命名空间内唯一的模型别名表。
// v1.0.34: 别名统一收口在 domestic 目录（此前 channel 层有 kimi-k3→k3 硬编码，
// 又被 domestic 精确匹配门禁抵消，两头不一致）。归一方向：短名 → 目录规范名。
var domesticModelAliases = map[string]map[string]string{
	"kimi": {"k3": "kimi-k3"},
	// DeepSeek's dated group-list model IDs use the same pricing tiers as the
	// corresponding v4 family models. Keep the aliases provider-scoped so the
	// exact-model, fail-closed billing boundary remains intact.
	"deepseek": {
		"deepseek-v4-pro-0813":                     "deepseek-v4-pro",
		"deepseek-v4.1-flash":                      "deepseek-flash",
		"deepseek-v4.1-flash-0910":                 "deepseek-flash",
		"deepseek-v4-1-flash":                      "deepseek-flash",
		"deepseek-v4-flash":                        "deepseek-flash",
		"deepseek-v4-flash-0731":                   "deepseek-flash",
		"deepseek-v4-flash-vision":                 "deepseek-flash",
		"deepseek-v4-flash-vision-exp":             "deepseek-flash",
		"deepseek-ai/deepseek-v4-flash-vision-exp": "deepseek-flash",
		"deepseek-chat":                            "deepseek-flash",
		"deepseek-reasoner":                        "deepseek-flash",
		"deepseek-v3.2":                            "deepseek-flash",
	},
}

// resolveDomesticModelAlias 在 provider 命名空间内归一模型别名，无别名时原样返回。
func resolveDomesticModelAlias(provider, model string) string {
	if aliases, ok := domesticModelAliases[provider]; ok {
		if canonical, ok := aliases[model]; ok {
			return canonical
		}
	}
	return model
}

func (s *DomesticPricingService) Resolve(ctx context.Context, provider, model string) (*DomesticPricingEntry, error) {
	provider = normalizeDomesticPricingProvider(provider)
	model = normalizeDomesticModel(model)
	model = resolveDomesticModelAlias(provider, model)
	if !isDomesticPricingProvider(provider) || model == "" {
		return nil, fmt.Errorf("%w: provider=%q model=%q", ErrDomesticPricingPending, provider, model)
	}

	if s != nil && s.repo != nil {
		override, err := s.repo.GetOverride(ctx, provider, model)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDomesticPricingUnavailable, err)
		}
		if override != nil {
			override.Provider = provider
			override.Model = model
			override.Currency = "USD"
			override.Status = DomesticPricingStatusActive
			override.Override = true
			if err := validateDomesticPricingEntry(override); err != nil {
				return nil, err
			}
			return override, nil
		}
	}

	for _, entry := range builtInDomesticPricing() {
		if domesticPricingKey(entry.Provider, entry.Model) != domesticPricingKey(provider, model) {
			continue
		}
		if err := validateDomesticPricingEntry(&entry); err != nil {
			return nil, err
		}
		return &entry, nil
	}
	return nil, fmt.Errorf("%w: provider=%s model=%s", ErrDomesticPricingPending, provider, model)
}

func (s *DomesticPricingService) UpsertOverride(ctx context.Context, input DomesticPricingOverrideInput) (*DomesticPricingEntry, error) {
	if s == nil || s.repo == nil {
		return nil, ErrDomesticPricingUnavailable
	}
	normalized, err := validateDomesticPricingOverride(input)
	if err != nil {
		return nil, err
	}
	entry, err := s.repo.UpsertOverride(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, ErrDomesticPricingUnavailable
	}
	entry.Provider = normalized.Provider
	entry.Model = normalized.Model
	entry.Source = normalized.Source
	entry.Currency = "USD"
	entry.Status = DomesticPricingStatusActive
	entry.Override = true
	if err := validateDomesticPricingEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *DomesticPricingService) DeleteOverride(ctx context.Context, provider, model string) (bool, error) {
	if s == nil || s.repo == nil {
		return false, ErrDomesticPricingUnavailable
	}
	provider = normalizeDomesticPricingProvider(provider)
	model = normalizeDomesticModel(model)
	if !isDomesticPricingProvider(provider) || model == "" || model == "*" {
		return false, ErrDomesticPricingInvalid
	}
	return s.repo.DeleteOverride(ctx, provider, model)
}

func (entry *DomesticPricingEntry) modelPricing() (*ModelPricing, error) {
	if err := validateDomesticPricingEntry(entry); err != nil {
		return nil, err
	}
	pricing := &ModelPricing{
		InputPricePerToken:          *entry.InputPricePerToken,
		OutputPricePerToken:         *entry.OutputPricePerToken,
		LongContextInputThreshold:   entry.LongContextInputThreshold,
		LongContextInputMultiplier:  entry.LongContextInputMultiplier,
		LongContextOutputMultiplier: entry.LongContextOutputMultiplier,
	}
	if entry.CacheReadPricePerToken != nil {
		pricing.CacheReadPricePerToken = *entry.CacheReadPricePerToken
	}
	if entry.CacheWritePricePerToken != nil {
		pricing.CacheCreationPricePerToken = *entry.CacheWritePricePerToken
		pricing.CacheCreation5mPrice = *entry.CacheWritePricePerToken
		pricing.CacheCreation1hPrice = *entry.CacheWritePricePerToken
		pricing.CacheCreationPriceExplicit = true
	}
	return pricing, nil
}

func (s *DomesticPricingService) modelPricing(entry *DomesticPricingEntry) (*ModelPricing, error) {
	pricing, err := entry.modelPricing()
	if err != nil || entry.Override || entry.PeakRateMultiplier <= 1 || !isDeepSeekPeakTime(s.currentTime(), entry.Provider) {
		return pricing, err
	}

	multiplier := entry.PeakRateMultiplier
	pricing.InputPricePerToken *= multiplier
	pricing.OutputPricePerToken *= multiplier
	pricing.CacheReadPricePerToken *= multiplier
	pricing.CacheCreationPricePerToken *= multiplier
	pricing.CacheCreation5mPrice *= multiplier
	pricing.CacheCreation1hPrice *= multiplier
	return pricing, nil
}

func (s *DomesticPricingService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func isDeepSeekPeakTime(at time.Time, provider string) bool {
	if normalizeDomesticPricingProvider(provider) != DomesticProviderDeepSeek {
		return false
	}
	beijing := at.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	if beijing.Weekday() == time.Saturday || beijing.Weekday() == time.Sunday {
		return false
	}
	minute := beijing.Hour()*60 + beijing.Minute()
	return (minute >= 9*60 && minute < 12*60) || (minute >= 14*60 && minute < 18*60)
}
