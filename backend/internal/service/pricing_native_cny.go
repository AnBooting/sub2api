package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const nativeCNYDefaultURL = "https://basellm.github.io/llm-metadata/api/all.json"
const nativeCNYSupplementDefaultURL = "https://raw.githubusercontent.com/szp2005/llm-prices-cn/main/prices.json"

// Runtime validation also protects old/imported cards that bypass save-time checks.
func validateManualCNYTokenPricing(p *ChannelModelPricing) error {
	if p == nil || p.Currency != "CNY" {
		return nil
	}
	for _, price := range []*float64{p.InputPrice, p.OutputPrice, p.CacheReadPrice, p.CacheWritePrice} {
		if price == nil || math.IsNaN(*price) || math.IsInf(*price, 0) || *price < 0 {
			return fmt.Errorf("%w: manual CNY token pricing requires explicit input, output, cache read and cache write prices", ErrModelPricingUnavailable)
		}
	}
	return nil
}

// NativeRates are always original CNY per million tokens, never converted USD.
type NativeRates struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
	Reasoning  *float64 `json:"reasoning,omitempty"`
}
type NativeTier struct {
	NativeRates
	Tier struct {
		Size int    `json:"size"`
		Type string `json:"type"`
	} `json:"tier"`
}
type NativeWindow struct {
	NativeRates
	Weekdays   []int        `json:"weekdays"`
	HourRanges []string     `json:"hours"`
	Tiers      []NativeTier `json:"tiers,omitempty"`
}
type NativeSchedule struct {
	Timezone string         `json:"timezone"`
	Windows  []NativeWindow `json:"windows"`
}
type NativeCNYCard struct {
	holidayCalendar *deepseekHolidayCalendar
	NativeRates
	Tiers     []NativeTier    `json:"tiers,omitempty"`
	Schedule  *NativeSchedule `json:"schedule,omitempty"`
	Provider  string          `json:"provider"`
	Source    string          `json:"source"`
	FetchedAt time.Time       `json:"fetched_at"`
}

func nativeModelKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, name)
}

func isNativeCNModel(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// These generic words need explicit model families, not arbitrary prefixes.
	for _, family := range []string{"spark-lite", "spark-pro", "spark-max", "spark-ultra", "spark-x1"} {
		if name == family || strings.HasPrefix(name, family+"-") {
			return true
		}
	}
	step := strings.TrimPrefix(name, "step")
	step = strings.TrimPrefix(step, "-")
	if strings.HasPrefix(name, "step") && len(step) > 0 && step[0] >= '0' && step[0] <= '9' {
		return true
	}
	for _, prefix := range []string{"deepseek", "qwen", "qwq", "qvq", "glm", "kimi", "moonshot", "minimax", "doubao", "ernie", "hunyuan", "mimo", "baichuan", "longcat", "bailing", "sensenova"} {
		if name == prefix || strings.HasPrefix(name, prefix+"-") || strings.HasPrefix(name, prefix+"_") ||
			(strings.HasPrefix(name, prefix) && len(name) > len(prefix) && name[len(prefix)] >= '0' && name[len(prefix)] <= '9') {
			return true
		}
	}
	return false
}

func (s *PricingService) RequiresNativeCNY(model string) bool {
	if s == nil || !s.GetControls().NativeCNYEnabled {
		return false
	}
	s.mu.RLock()
	card := s.nativeCNYCards[nativeModelKey(model)]
	s.mu.RUnlock()
	return card != nil || isNativeCNModel(model)
}

func validNativeRates(r NativeRates, complete bool) bool {
	if complete && (r.Input == nil || r.Output == nil) {
		return false
	}
	for _, p := range []*float64{r.Input, r.Output, r.CacheRead, r.CacheWrite, r.Reasoning} {
		if p != nil && (math.IsNaN(*p) || math.IsInf(*p, 0) || *p < 0) {
			return false
		}
	}
	// The gateway counts reasoning tokens in output; distinct rates require a
	// complete manual card rather than silently charging the cheaper rate.
	if r.Reasoning != nil && r.Output != nil && *r.Reasoning != *r.Output {
		return false
	}
	return true
}
func validNativeCard(c *NativeCNYCard) bool {
	if !validNativeRates(c.NativeRates, true) {
		return false
	}
	validTiers := func(tiers []NativeTier) bool {
		for _, tier := range tiers {
			if tier.Tier.Type != "context" || tier.Tier.Size < 0 || !validNativeRates(tier.NativeRates, false) {
				return false
			}
		}
		return true
	}
	if !validTiers(c.Tiers) {
		return false
	}
	if c.Schedule != nil {
		if _, err := time.LoadLocation(c.Schedule.Timezone); err != nil {
			return false
		}
		for _, w := range c.Schedule.Windows {
			if !validNativeRates(w.NativeRates, false) || !validTiers(w.Tiers) || len(w.Weekdays) == 0 || len(w.HourRanges) == 0 {
				return false
			}
			for _, d := range w.Weekdays {
				if d < 0 || d > 6 {
					return false
				}
			}
			for _, hours := range w.HourRanges {
				parts := strings.Split(hours, "-")
				if len(parts) != 2 {
					return false
				}
				if _, ok := nativeMinute(parts[0]); !ok {
					return false
				}
				if _, ok := nativeMinute(parts[1]); !ok {
					return false
				}
			}
		}
	}
	return true
}

func parseNativeCNYCatalog(body []byte, source string, at time.Time) (map[string]*NativeCNYCard, error) {
	var providers map[string]struct {
		Subscription bool `json:"subscription"`
		Models       map[string]struct {
			ID   string `json:"id"`
			Cost struct {
				NativeRates
				Currency        string          `json:"currency"`
				Tiers           []NativeTier    `json:"tiers"`
				Schedule        *NativeSchedule `json:"schedule"`
				CurrencyOptions map[string]struct {
					NativeRates
					Tiers    []NativeTier    `json:"tiers"`
					Schedule *NativeSchedule `json:"schedule"`
				} `json:"currency_options"`
			} `json:"cost"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, err
	}
	result := make(map[string]*NativeCNYCard)
	// Only native domestic endpoints; regional/international USD estimates are ignored.
	for _, providerID := range []string{"deepseek", "zhipuai", "alibaba-cn", "moonshotai-cn", "minimax-cn", "volcengine", "xiaomi", "stepfun", "stepfun-ai", "bailing", "longcat", "sensenova", "tencent-tokenhub"} {
		provider := providers[providerID]
		if provider.Subscription {
			continue
		}
		for id, model := range provider.Models {
			if model.ID != "" {
				id = model.ID
			}
			if !isNativeCNModel(id) {
				continue
			}
			c := &NativeCNYCard{NativeRates: model.Cost.NativeRates, Tiers: model.Cost.Tiers, Schedule: model.Cost.Schedule, Provider: providerID, Source: source, FetchedAt: at}
			if model.Cost.Currency != "CNY" {
				alt, ok := model.Cost.CurrencyOptions["CNY"]
				if !ok {
					continue
				}
				c.NativeRates, c.Tiers, c.Schedule = alt.NativeRates, alt.Tiers, alt.Schedule
			}
			if validNativeCard(c) {
				result[nativeModelKey(id)] = c
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("native catalog has no explicit valid CNY token prices")
	}
	return result, nil
}

func parseNativeCNYSupplement(body []byte, source string, at time.Time) (map[string]*NativeCNYCard, error) {
	var feed struct {
		Models []struct {
			ID       string   `json:"id"`
			Vendor   string   `json:"vendor_id"`
			Currency string   `json:"billing_currency"`
			Retired  bool     `json:"retired"`
			Input    *float64 `json:"input_price_orig_per_m"`
			Output   *float64 `json:"output_price_orig_per_m"`
			Cache    *float64 `json:"cached_input_price_orig_per_m"`
			Tiers    []struct {
				UpTo   *int     `json:"up_to_input_tokens"`
				Input  *float64 `json:"input_price_orig_per_m"`
				Output *float64 `json:"output_price_orig_per_m"`
				Cache  *float64 `json:"cached_input_price_orig_per_m"`
			} `json:"price_tiers"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	result := make(map[string]*NativeCNYCard)
	for _, model := range feed.Models {
		if model.Currency != "CNY" || model.Retired || !isNativeCNModel(model.ID) {
			continue
		}
		c := &NativeCNYCard{NativeRates: NativeRates{Input: model.Input, Output: model.Output, CacheRead: model.Cache}, Provider: model.Vendor, Source: source, FetchedAt: at}
		for i, tier := range model.Tiers {
			r := NativeRates{Input: tier.Input, Output: tier.Output, CacheRead: tier.Cache}
			if i == 0 {
				c.NativeRates = r
				continue
			}
			prev := model.Tiers[i-1].UpTo
			if prev == nil {
				continue
			}
			t := NativeTier{NativeRates: r}
			t.Tier.Type = "context"
			t.Tier.Size = *prev
			c.Tiers = append(c.Tiers, t)
		}
		if validNativeCard(c) {
			result[nativeModelKey(model.ID)] = c
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("supplement has no valid original CNY prices")
	}
	return result, nil
}

func mergeNativeRates(base, patch NativeRates) NativeRates {
	if patch.Input != nil {
		base.Input = patch.Input
	}
	if patch.Output != nil {
		base.Output = patch.Output
	}
	if patch.CacheRead != nil {
		base.CacheRead = patch.CacheRead
	}
	if patch.CacheWrite != nil {
		base.CacheWrite = patch.CacheWrite
	}
	return base
}
func nativeMinute(clock string) (int, bool) {
	if clock == "24:00" {
		return 1440, true
	}
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}
func (c *NativeCNYCard) ratesAt(contextTokens int, at time.Time) NativeRates {
	rates, tiers := c.NativeRates, c.Tiers
	if c.Schedule != nil && !(strings.EqualFold(c.Provider, "deepseek") && deepseekOffPeakDay(at, c.holidayCalendar)) {
		loc, _ := time.LoadLocation(c.Schedule.Timezone)
		now := at.In(loc)
		minute := now.Hour()*60 + now.Minute()
		for _, w := range c.Schedule.Windows {
			day := false
			for _, d := range w.Weekdays {
				if d == int(now.Weekday()) {
					day = true
				}
			}
			if !day {
				continue
			}
			matched := false
			for _, hours := range w.HourRanges {
				parts := strings.Split(hours, "-")
				start, _ := nativeMinute(parts[0])
				end, _ := nativeMinute(parts[1])
				if (start < end && minute >= start && minute < end) || (start > end && (minute >= start || minute < end)) {
					matched = true
				}
			}
			if matched {
				rates = mergeNativeRates(rates, w.NativeRates)
				if len(w.Tiers) > 0 {
					tiers = w.Tiers
				}
				break
			}
		}
	}
	ordered := append([]NativeTier(nil), tiers...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Tier.Size < ordered[j].Tier.Size })
	for _, tier := range ordered {
		if contextTokens > tier.Tier.Size {
			rates = mergeNativeRates(rates, tier.NativeRates)
		}
	}
	return rates
}

func (s *PricingService) nativeCNYPath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "model_pricing_cny.json")
}

func nativePerToken(value *float64) *float64 {
	if value == nil {
		return nil
	}
	price := *value / 1e6
	return &price
}
func (s *PricingService) recordNativeCNYError(err error) {
	if errors.Is(err, ErrPricingPullDisabled) || errors.Is(err, context.Canceled) {
		return
	}
	s.mu.Lock()
	s.nativeCNYError = err.Error()
	s.mu.Unlock()
	s.recordPricingSyncResult("CNY", err)
}
func (s *PricingService) loadNativeCNYCache() {
	body, err := os.ReadFile(s.nativeCNYPath())
	if err != nil {
		return
	}
	var cards map[string]*NativeCNYCard
	if json.Unmarshal(body, &cards) != nil {
		return
	}
	for key, c := range cards {
		if c == nil || !validNativeCard(c) {
			delete(cards, key)
		}
	}
	s.mu.Lock()
	s.nativeCNYCards = cards
	for _, c := range cards {
		if c.FetchedAt.After(s.nativeCNYUpdated) {
			s.nativeCNYUpdated = c.FetchedAt
		}
	}
	s.mu.Unlock()
}
func (s *PricingService) syncNativeCNY(force bool) (resultErr error) {
	if !s.GetControls().NativeCNYEnabled {
		return nil
	}
	s.mu.RLock()
	last := s.nativeCNYUpdated
	s.mu.RUnlock()
	interval := time.Duration(s.cfg.Pricing.UpdateIntervalHours) * time.Hour
	if interval < time.Hour {
		interval = 24 * time.Hour
	}
	if !force && time.Since(last) < interval {
		return nil
	}
	var supplementErr error
	defer func() {
		if resultErr != nil {
			s.recordNativeCNYError(resultErr)
		} else {
			s.recordPricingSyncResult("CNY", nil)
			s.recordPricingSyncResult("CNY supplement", supplementErr)
		}
	}()
	ctx, finish, generation, err := s.beginRemoteRequest(30 * time.Second)
	if err != nil {
		return err
	}
	defer finish()
	address := s.cfg.Pricing.NativeCNYURL
	if address == "" {
		address = nativeCNYDefaultURL
	}
	address, err = s.validatePricingURL(address)
	if err != nil {
		return err
	}
	body, err := s.remoteClient.FetchPricingJSON(ctx, address)
	if err != nil {
		return err
	}
	when := time.Now()
	cards, err := parseNativeCNYCatalog(body, address, when)
	if err != nil {
		return err
	}
	supplement := s.cfg.Pricing.NativeCNYSupplementURL
	if supplement == "" {
		supplement = nativeCNYSupplementDefaultURL
	}
	supplement, err = s.validatePricingURL(supplement)
	if err != nil {
		supplementErr = err
	} else {
		body, err = s.remoteClient.FetchPricingJSON(ctx, supplement)
		if err != nil {
			supplementErr = err
		} else {
			var extra map[string]*NativeCNYCard
			extra, err = parseNativeCNYSupplement(body, supplement, when)
			if err != nil {
				supplementErr = err
			} else {
				for key, c := range extra {
					if _, exists := cards[key]; !exists {
						cards[key] = c
					}
				}
			}
		}
	}
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	if !s.controlsLocked().RemoteEnabled || generation != s.remoteGeneration || ctx.Err() != nil {
		return ErrPricingPullDisabled
	}
	if supplementErr != nil {
		s.mu.RLock()
		for key, c := range s.nativeCNYCards {
			if c.Source == supplement {
				if _, exists := cards[key]; !exists {
					cards[key] = c
				}
			}
		}
		s.mu.RUnlock()
	}
	body, err = json.Marshal(cards)
	if err != nil {
		return err
	}
	if err = os.WriteFile(s.nativeCNYPath(), body, 0600); err != nil {
		return err
	}
	s.mu.Lock()
	s.nativeCNYCards = cards
	s.nativeCNYUpdated = when
	s.nativeCNYError = ""
	if supplementErr != nil {
		s.nativeCNYError = supplementErr.Error()
	}
	s.mu.Unlock()
	return nil
}

func (s *PricingService) nativePricingAt(model string, at time.Time) (*ModelPricing, error) {
	s.mu.RLock()
	card := s.nativeCNYCards[nativeModelKey(model)]
	s.mu.RUnlock()
	if card == nil {
		return nil, fmt.Errorf("%w: no original CNY price for %s", ErrModelPricingUnavailable, model)
	}
	if at.IsZero() {
		at = time.Now()
	}
	clonedCard := *card
	clonedCard.holidayCalendar = s.deepseekCalendar()
	card = &clonedCard
	rates := card.ratesAt(0, at)
	p := &ModelPricing{Currency: "CNY", PricingSource: card.Source, NativeCard: card, NativePricingAt: at, CacheCreationPriceExplicit: true}
	applyNativeRates(p, rates)
	return p, nil
}
func applyNativeRates(p *ModelPricing, r NativeRates) {
	p.InputPricePerToken = *r.Input / 1e6
	p.OutputPricePerToken = *r.Output / 1e6
	p.CacheReadPricePerToken = 0
	if r.CacheRead != nil {
		p.CacheReadPricePerToken = *r.CacheRead / 1e6
	}
	p.CacheCreationPricePerToken = 0
	if r.CacheWrite != nil {
		p.CacheCreationPricePerToken = *r.CacheWrite / 1e6
	}
	// Defaults for providers without independently priced image input tokens.
	p.ImageInputPricePerToken = 0
	p.ImageCacheReadPricePerToken = 0
}

var missingNativeCachePriceWarned sync.Map

func resolveNativeTokenPricing(p *ModelPricing, tokens UsageTokens, at time.Time) (*ModelPricing, error) {
	if p.NativeCard == nil {
		return p, nil
	}
	if at.IsZero() {
		at = p.NativePricingAt
	}
	if at.IsZero() {
		at = time.Now()
	}
	r := p.NativeCard.ratesAt(tokens.InputTokens+tokens.CacheReadTokens+tokens.CacheCreationTokens, at)
	if (tokens.CacheReadTokens > 0 && r.CacheRead == nil) || ((tokens.CacheCreationTokens > 0 || tokens.CacheCreation5mTokens > 0 || tokens.CacheCreation1hTokens > 0) && r.CacheWrite == nil) {
		key := fmt.Sprintf("%s:%t:%t", p.NativeCard.Provider, r.CacheRead == nil, r.CacheWrite == nil)
		if _, seen := missingNativeCachePriceWarned.LoadOrStore(key, struct{}{}); !seen {
			slog.Warn("CNY pricing unavailable: usage includes cache tokens without explicit price; refusing to assume free caching",
				"provider", p.NativeCard.Provider, "cache_read_price_missing", r.CacheRead == nil, "cache_write_price_missing", r.CacheWrite == nil)
		}
		return nil, fmt.Errorf("%w: CNY cache price is missing", ErrModelPricingUnavailable)
	}
	cloned := *p
	applyNativeRates(&cloned, r)
	return &cloned, nil
}
