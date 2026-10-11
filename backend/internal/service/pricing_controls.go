package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const SettingKeyPricingControls = "model_pricing_controls"

var ErrPricingPullDisabled = errors.New("remote model pricing pull is disabled")

// PricingControls is persisted before use, including before startup fetches.
type PricingControls struct {
	RemoteEnabled    bool `json:"remote_enabled"`
	NativeCNYEnabled bool `json:"native_cny_enabled"`
}

func (s *PricingService) controlsLocked() PricingControls {
	if s.controls != nil {
		return *s.controls
	}
	c := PricingControls{RemoteEnabled: true}
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
	controls := s.GetControls()
	holidays := s.DeepseekHolidayDates()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"remote_enabled": controls.RemoteEnabled, "native_cny_enabled": controls.NativeCNYEnabled,
		"deepseek_holidays": holidays,
		"usd_model_count":   len(s.pricingData), "usd_last_updated": s.lastUpdated,
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
	if raw := values[SettingKeyDeepseekHolidays]; raw != "" {
		var dates []string
		if err := json.Unmarshal([]byte(raw), &dates); err != nil {
			return err
		}
		calendar, err := newDeepseekCalendar(dates)
		if err != nil {
			return err
		}
		s.holidayCalendar = calendar
	}
	if raw := values[SettingKeyPricingControls]; raw != "" {
		var controls PricingControls
		if err := json.Unmarshal([]byte(raw), &controls); err != nil {
			return err
		}
		s.controls = &controls
	} else {
		// Persist configuration defaults so checkout and public settings share
		// the exact same currency mode as the pricing engine.
		body, err := json.Marshal(s.controlsLocked())
		if err != nil {
			return err
		}
		if err := s.settingsRepo.Set(ctx, SettingKeyPricingControls, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *PricingService) UpdateControls(ctx context.Context, controls PricingControls) error {
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	if s.settingsRepo != nil {
		body, err := json.Marshal(controls)
		if err != nil {
			return err
		}
		if err := s.settingsRepo.Set(ctx, SettingKeyPricingControls, string(body)); err != nil {
			return err
		}
	}
	s.remoteGeneration++
	for _, cancel := range s.remoteCancels {
		cancel()
	}
	s.controls = &controls
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
