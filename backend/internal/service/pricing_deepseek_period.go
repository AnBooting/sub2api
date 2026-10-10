package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Only administrator-maintained statutory holidays are recorded. Weekend make-up
// workdays are intentionally not a separate calendar: weekends remain offpeak.
var ErrInvalidDeepSeekHolidays = errors.New("invalid DeepSeek holiday calendar")

var deepSeekMissingCalendarWarnings sync.Map

func cloneDeepSeekHolidays(holidays map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(holidays))
	for year, days := range holidays {
		clone[year] = append([]string{}, days...)
	}
	return clone
}

func validateDeepSeekHolidays(holidays map[string][]string) error {
	for year, days := range holidays {
		if days == nil {
			return fmt.Errorf("%w: year %q requires an array, not null", ErrInvalidDeepSeekHolidays, year)
		}
		y, err := strconv.Atoi(year)
		if err != nil || len(year) != 4 || y < 1 || y > 9999 || fmt.Sprintf("%04d", y) != year {
			return fmt.Errorf("%w: invalid year %q", ErrInvalidDeepSeekHolidays, year)
		}
		seen := make(map[string]bool, len(days))
		for _, day := range days {
			date, err := time.Parse("2006-01-02", day)
			if err != nil || date.Format("2006-01-02") != day || date.Year() != y || seen[day] {
				return fmt.Errorf("%w: invalid or duplicate date %q in %s", ErrInvalidDeepSeekHolidays, day, year)
			}
			seen[day] = true
		}
	}
	return nil
}

// A complete base card is the offpeak/default window. The peak windows are
// half-open and use the same chooser for native absolute and manual prices.
func chooseDeepSeekPricingPeriod(at time.Time, holidays map[string][]string) DeepSeekPricingPeriod {
	local := at.In(deepSeekLocation)
	year := fmt.Sprintf("%04d", local.Year())
	days, known := holidays[year]
	period := DeepSeekPricingPeriod{Label: "offpeak", Timezone: "Asia/Shanghai", CalendarYearMissing: !known}
	for _, day := range days {
		if day == local.Format("2006-01-02") {
			return period
		}
	}
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return period
	}
	minute := local.Hour()*60 + local.Minute()
	switch {
	case minute >= 9*60 && minute < 12*60:
		period.Label, period.Window = "peak", "09:00-12:00"
	case minute >= 14*60 && minute < 18*60:
		period.Label, period.Window = "peak", "14:00-18:00"
	}
	return period
}

func (p DeepSeekPricingPeriod) multiplier() float64 {
	if p.Label == "peak" {
		return 2
	}
	return 1
}

func warnMissingDeepSeekCalendar(period DeepSeekPricingPeriod, at time.Time) {
	if !period.CalendarYearMissing {
		return
	}
	year := at.In(deepSeekLocation).Year()
	if _, seen := deepSeekMissingCalendarWarnings.LoadOrStore(year, struct{}{}); !seen {
		slog.Warn("DeepSeek holiday calendar missing; using weekday/weekend pricing without guessing holidays", "year", year)
	}
}

func (s *BillingService) deepSeekPricingPeriodAt(at time.Time) DeepSeekPricingPeriod {
	var holidays map[string][]string
	if s != nil && s.pricingService != nil {
		holidays = s.pricingService.GetControls().DeepSeekHolidays
	}
	period := chooseDeepSeekPricingPeriod(at, holidays)
	warnMissingDeepSeekCalendar(period, at)
	return period
}

func scaleNativeRates(rates NativeRates, multiplier float64) NativeRates {
	scale := func(p *float64) *float64 {
		if p == nil {
			return nil
		}
		value := *p * multiplier
		return &value
	}
	return NativeRates{Input: scale(rates.Input), Output: scale(rates.Output), CacheRead: scale(rates.CacheRead), CacheWrite: scale(rates.CacheWrite), Reasoning: scale(rates.Reasoning)}
}

func scaleNativeTiers(tiers []NativeTier, multiplier float64) []NativeTier {
	clone := append([]NativeTier(nil), tiers...)
	for i := range clone {
		clone[i].NativeRates = scaleNativeRates(clone[i].NativeRates, multiplier)
	}
	return clone
}

func scaleDeepSeekTokenPricing(pricing *ModelPricing, multiplier float64) *ModelPricing {
	cloned := *pricing
	for _, price := range []*float64{
		&cloned.InputPricePerToken, &cloned.OutputPricePerToken, &cloned.CacheReadPricePerToken,
		&cloned.CacheCreationPricePerToken, &cloned.CacheCreation5mPrice, &cloned.CacheCreation1hPrice,
		&cloned.InputPricePerTokenPriority, &cloned.OutputPricePerTokenPriority,
		&cloned.CacheReadPricePerTokenPriority, &cloned.CacheCreationPricePerTokenPriority,
		&cloned.ImageInputPricePerToken, &cloned.ImageCacheReadPricePerToken, &cloned.ImageOutputPricePerToken,
	} {
		*price *= multiplier
	}
	return &cloned
}

var deepSeekLocation = time.FixedZone("Asia/Shanghai", 8*3600)

type DeepSeekPricingPeriod struct {
	Label               string `json:"label"`
	Timezone            string `json:"timezone"`
	Window              string `json:"window"`
	CalendarYearMissing bool   `json:"calendar_year_missing"`
}
