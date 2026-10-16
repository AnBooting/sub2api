package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"
)

const SettingKeyPricingControls = "model_pricing_controls"

var ErrPricingPullDisabled = errors.New("remote model pricing pull is disabled")

// PricingControls is persisted before use, including before startup fetches.
type PricingControls struct {
	RemoteEnabled    bool                `json:"remote_enabled"`
	NativeCNYEnabled bool                `json:"native_cny_enabled"`
	DeepSeekHolidays map[string][]string `json:"deepseek_holidays"`

	// UpdateIntervalHours 是管理员可配的定价同步周期（小时），PRD-01/K-6。
	// 0 表示「未覆盖」，此时回落到配置文件 pricing.update_interval_hours。
	// 指针语义在这里不合适：0 与「显式设为 0」都不合法，用 0 表示未设置即可，
	// 校验层会拒绝任何非正值。
	UpdateIntervalHours int `json:"update_interval_hours,omitempty"`
}

func (s *PricingService) controlsLocked() PricingControls {
	if s.controls != nil {
		clone := *s.controls
		clone.DeepSeekHolidays = cloneDeepSeekHolidays(clone.DeepSeekHolidays)
		return clone
	}
	c := PricingControls{RemoteEnabled: true, DeepSeekHolidays: map[string][]string{}}
	if s.cfg != nil {
		if s.cfg.Pricing.RemoteEnabled != nil {
			c.RemoteEnabled = *s.cfg.Pricing.RemoteEnabled
		}
		c.NativeCNYEnabled = s.cfg.Pricing.NativeCNYEnabled
	}
	return c
}

func (s *PricingService) GetControls() PricingControls {
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	return s.controlsLocked()
}

func (s *PricingService) RemotePullEnabled() bool { return s.GetControls().RemoteEnabled }

func (s *PricingService) RefreshNativeCNY() {
	s.controlsMu.Lock()
	if s.stopping || !s.controlsLocked().RemoteEnabled || !s.controlsLocked().NativeCNYEnabled {
		s.controlsMu.Unlock()
		return
	}
	s.wg.Add(1)
	s.controlsMu.Unlock()
	go func() {
		defer s.wg.Done()
		_ = s.syncNativeCNY(true) // The sync method records failures and alerts once.
	}()
}

func (s *PricingService) GetPricingControlStatus() map[string]any {
	// LOCK ORDER: controlsMu must be acquired before mu, never after. Resolve every
	// controls-derived value (including the effective interval, which reads controls
	// internally) BEFORE taking mu.RLock() below. Taking mu first and then reaching
	// for controls would invert the order used by the pricing writers
	// (controlsMu → mu in savePricingData / syncNativeCNY) and deadlock.
	controls := s.GetControls()
	holidayDates := s.DeepseekHolidayDates()
	years, calendarError := s.deepseekCalendarStatus()
	calendarMissing := !s.deepseekCalendarAt(time.Now()).years[time.Now().In(deepSeekLocation).Year()]
	effectiveIntervalHours := s.PricingSyncIntervalHours()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"remote_enabled": controls.RemoteEnabled, "native_cny_enabled": controls.NativeCNYEnabled,
		"update_interval_hours":          controls.UpdateIntervalHours,
		"effective_interval_hours":       effectiveIntervalHours,
		"deepseek_holidays":              controls.DeepSeekHolidays,
		"deepseek_holiday_dates":         holidayDates,
		"deepseek_calendar_year_missing": calendarMissing,
		"deepseek_calendar_years":        years, "deepseek_calendar_error": calendarError,
		"usd_model_count": len(s.pricingData), "usd_last_updated": s.lastUpdated,
		"usd_error":       s.usdPricingError,
		"cny_model_count": len(s.nativeCNYCards), "cny_last_updated": s.nativeCNYUpdated, "cny_error": s.nativeCNYError,
	}
}

func (s *PricingService) loadControls(ctx context.Context) error {
	if s.settingsRepo == nil {
		return nil
	}
	values, err := s.settingsRepo.GetMultiple(ctx, []string{SettingKeyPricingControls, SettingKeyDeepseekHolidays})
	if err != nil {
		return err
	}
	s.calendarWriteMu.Lock()
	loadingLocked := true
	defer func() {
		if loadingLocked {
			s.calendarWriteMu.Unlock()
		}
	}()
	calendar := defaultDeepseekCalendar
	var calendarErr error
	var savedFields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(values[SettingKeyPricingControls]), &savedFields)
	_, hasYearCalendar := savedFields["deepseek_holidays"]
	if raw := values[SettingKeyDeepseekHolidays]; raw != "" && !hasYearCalendar {
		var dates []string
		if err := json.Unmarshal([]byte(raw), &dates); err != nil {
			calendarErr = err
		} else if len(dates) > 0 {
			if parsed, err := newDeepseekCalendar(dates); err != nil {
				calendarErr = err
			} else {
				calendar = parsed
			}
		}
	}
	if raw := values[SettingKeyPricingControls]; raw != "" {
		var controls PricingControls
		if err := json.Unmarshal([]byte(raw), &controls); err != nil {
			var fields map[string]json.RawMessage
			if parseErr := json.Unmarshal([]byte(raw), &fields); parseErr != nil {
				return parseErr
			}
			if _, hasCalendar := fields["deepseek_holidays"]; !hasCalendar {
				return err
			}
			delete(fields, "deepseek_holidays")
			clean, _ := json.Marshal(fields)
			if parseErr := json.Unmarshal(clean, &controls); parseErr != nil {
				return parseErr
			}
			calendarErr = err
			controls.DeepSeekHolidays = calendarByYear(defaultDeepseekCalendar)
		}
		if controls.DeepSeekHolidays != nil {
			if err := validateDeepSeekHolidays(controls.DeepSeekHolidays); err != nil {
				calendarErr = err
				calendar = defaultDeepseekCalendar
				controls.DeepSeekHolidays = calendarByYear(calendar)
			} else {
				calendar = deepseekCalendarFromYears(controls.DeepSeekHolidays)
			}
		} else {
			if hasYearCalendar {
				calendarErr = ErrInvalidDeepseekHolidays
			}
			controls.DeepSeekHolidays = calendarByYear(calendar)
		}
		s.controlsMu.Lock()
		s.controls = &controls
		s.controlsMu.Unlock()
	} else {
		// Persist configuration defaults so checkout and public settings share
		// the exact same currency mode as the pricing engine.
		controls := s.GetControls()
		controls.DeepSeekHolidays = calendarByYear(calendar)
		body, err := json.Marshal(controls)
		if err != nil {
			return err
		}
		if err := s.settingsRepo.Set(ctx, SettingKeyPricingControls, string(body)); err != nil {
			return err
		}
		s.controlsMu.Lock()
		s.controls = &controls
		s.controlsMu.Unlock()
	}
	s.calendarMu.Lock()
	s.holidayCalendar = calendar
	s.calendarError = ""
	if calendarErr != nil {
		s.calendarError = "Invalid saved DeepSeek holiday calendar; built-in dates restored"
	}
	s.calendarMu.Unlock()
	s.calendarWriteMu.Unlock()
	loadingLocked = false
	if calendarErr != nil {
		slog.Warn("Invalid saved DeepSeek holiday calendar; using built-in dates", "error", calendarErr)
	}
	if calendarErr != nil {
		s.recordPricingSyncResult("DeepSeek calendar", ErrInvalidDeepseekHolidays)
	}
	s.checkDeepseekCalendarCoverage(time.Now())
	return nil
}

// PricingSyncIntervalHoursBounds are the accepted administrator-configured
// pricing sync periods, in hours. The lower bound keeps a mis-typed value from
// turning the sync loop into a hot loop against the upstream price sources; the
// upper bound keeps the setting meaningful (a longer period is "disable", which
// already exists as a separate switch).
const (
	PricingSyncIntervalMinHours = 1
	PricingSyncIntervalMaxHours = 24 * 30
)

// ErrInvalidPricingSyncInterval reports an out-of-range sync period.
var ErrInvalidPricingSyncInterval = errors.New("pricing sync interval is out of range")

// ValidatePricingSyncIntervalHours accepts 0 as "not overridden" and otherwise
// requires a value inside the documented bounds.
func ValidatePricingSyncIntervalHours(hours int) error {
	if hours == 0 {
		return nil
	}
	if hours < PricingSyncIntervalMinHours || hours > PricingSyncIntervalMaxHours {
		return ErrInvalidPricingSyncInterval
	}
	return nil
}

// PricingSyncIntervalHours resolves the effective pricing sync period:
// administrator setting first, configuration file as the default.
//
// PRD-01/K-6 requires the period to be operator-configurable from the admin page
// rather than only from the config file. Every consumer must go through this
// resolver so the three read sites cannot drift apart.
func (s *PricingService) PricingSyncIntervalHours() int {
	if s == nil {
		return 0
	}
	if override := s.GetControls().UpdateIntervalHours; override > 0 {
		return override
	}
	if s.cfg != nil && s.cfg.Pricing.UpdateIntervalHours > 0 {
		return s.cfg.Pricing.UpdateIntervalHours
	}
	return 0
}

func (s *PricingService) UpdateControls(ctx context.Context, controls PricingControls) error {
	return s.updateControls(ctx, controls, false)
}

func (s *PricingService) updateControls(ctx context.Context, controls PricingControls, calendarOnly bool) error {
	if err := validateDeepSeekHolidays(controls.DeepSeekHolidays); err != nil {
		return err
	}
	if err := ValidatePricingSyncIntervalHours(controls.UpdateIntervalHours); err != nil {
		return err
	}
	if controls.DeepSeekHolidays != nil {
		controls.DeepSeekHolidays = cloneDeepSeekHolidays(controls.DeepSeekHolidays)
	}
	s.calendarWriteMu.Lock()
	defer s.calendarWriteMu.Unlock()
	s.controlsMu.Lock()
	if calendarOnly {
		current := s.controlsLocked()
		current.DeepSeekHolidays = controls.DeepSeekHolidays
		controls = current
	}
	calendarChanged := controls.DeepSeekHolidays != nil
	// nil means the caller omitted the calendar. Preserve the latest version
	// inside the same serialized write that persists switches and publishes memory.
	// A non-nil empty map is an explicit clear. Never merge a handler snapshot.
	if controls.DeepSeekHolidays == nil {
		controls.DeepSeekHolidays = s.controlsLocked().DeepSeekHolidays
	}
	// 0 means the caller omitted the interval; preserve the stored override so a
	// switch-only update cannot silently drop the administrator's period.
	if controls.UpdateIntervalHours == 0 {
		controls.UpdateIntervalHours = s.controlsLocked().UpdateIntervalHours
	}
	intervalChanged := s.controlsLocked().UpdateIntervalHours != controls.UpdateIntervalHours
	s.controlsMu.Unlock()
	if s.settingsRepo != nil {
		body, err := json.Marshal(controls)
		if err != nil {
			return err
		}
		if err := s.settingsRepo.Set(ctx, SettingKeyPricingControls, string(body)); err != nil {
			return err
		}
	}
	s.controlsMu.Lock()
	s.remoteGeneration++
	for _, cancel := range s.remoteCancels {
		cancel()
	}
	s.controls = &controls
	if calendarChanged {
		s.calendarMu.Lock()
		s.holidayCalendar = deepseekCalendarFromYears(controls.DeepSeekHolidays)
		s.calendarError = ""
		s.calendarMu.Unlock()
	}
	s.controlsMu.Unlock()
	// Hot update (PRD-01/K-6): rearm the sync timer so the new period applies at the
	// next boundary rather than after the previously scheduled period elapses.
	if intervalChanged {
		s.notifySyncIntervalChanged()
	}
	return nil
}

// Every outbound request must pass this gate. Generation guards also stop
// clients that ignore cancellation from committing a stale response.
func (s *PricingService) beginRemoteRequest(timeout time.Duration) (context.Context, func(), uint64, error) {
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	if s.stopping || !s.controlsLocked().RemoteEnabled {
		return nil, nil, 0, ErrPricingPullDisabled
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	if s.remoteCancels == nil {
		s.remoteCancels = make(map[uint64]context.CancelFunc)
	}
	s.remoteRequestID++
	id := s.remoteRequestID
	s.remoteCancels[id] = cancel
	finish := func() {
		cancel()
		s.controlsMu.Lock()
		delete(s.remoteCancels, id)
		s.controlsMu.Unlock()
	}
	return ctx, finish, s.remoteGeneration, nil
}
