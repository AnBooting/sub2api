package dto

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// UserFacingGroup is the whitelist for /groups/available. Embedded and admin
// responses continue to use Group and must retain their independent contracts.
type UserFacingGroup struct {
	ID                       int64                    `json:"id"`
	Name                     string                   `json:"name"`
	Description              string                   `json:"description"`
	Platform                 string                   `json:"platform"`
	RateMultiplier           float64                  `json:"rate_multiplier"`
	SubscriptionType         string                   `json:"subscription_type"`
	Status                   string                   `json:"status"`
	PeakRateEnabled          bool                     `json:"peak_rate_enabled"`
	PeakStart                string                   `json:"peak_start"`
	PeakEnd                  string                   `json:"peak_end"`
	PeakRateMultiplier       float64                  `json:"peak_rate_multiplier"`
	DeepSeekPricingState     *DeepSeekPricingStateDTO `json:"deepseek_pricing_state,omitempty"`
	AllowLive                bool                     `json:"allow_live"`
	AudioRealtimePricePerMin *float64                 `json:"audio_realtime_price_per_min"`
	AllowHighAvailability    bool                     `json:"allow_high_availability"`
}

func UserFacingGroupFromService(g *service.Group) *UserFacingGroup {
	if g == nil {
		return nil
	}
	return &UserFacingGroup{
		ID: g.ID, Name: g.Name, Description: g.Description, Platform: g.Platform,
		RateMultiplier: g.RateMultiplier, SubscriptionType: g.SubscriptionType, Status: g.Status,
		PeakRateEnabled: g.PeakRateEnabled, PeakStart: g.PeakStart, PeakEnd: g.PeakEnd,
		PeakRateMultiplier: g.PeakRateMultiplier, DeepSeekPricingState: deepSeekPricingStateDTOFromService(g),
		AllowLive: g.AllowLive, AudioRealtimePricePerMin: g.AudioRealtimePricePerMin,
		AllowHighAvailability: g.AllowHighAvailability,
	}
}

// Localized follows the shared language selection without exposing translations.
func (g *UserFacingGroup) Localized(i18n domain.GroupI18n, acceptLanguage string) *UserFacingGroup {
	if g == nil {
		return nil
	}
	g.Name, g.Description = i18n.Localize(acceptLanguage, g.Name, g.Description)
	return g
}
