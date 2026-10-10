package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type pricingSettingsRepo struct {
	SettingRepository
	values map[string]string
	err    error
}

func (r *pricingSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return r.values, r.err
}
func (r *pricingSettingsRepo) Set(_ context.Context, key, value string) error {
	if r.err != nil {
		return r.err
	}
	r.values[key] = value
	return nil
}

func TestPricingControlsPersistAcrossRestartAndFailedSave(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	repo := &pricingSettingsRepo{values: map[string]string{}}
	client := &controlsPricingClient{}
	first := NewPricingService(cfg, client)
	first.settingsRepo = repo
	want := PricingControls{NativeCNYEnabled: true, RemoteEnabled: false, DeepSeekHolidays: map[string][]string{}}
	require.NoError(t, first.UpdateControls(context.Background(), want))
	next := NewPricingService(cfg, client)
	next.settingsRepo = repo
	require.NoError(t, next.loadControls(context.Background()))
	require.Equal(t, want, next.GetControls())
	require.ErrorIs(t, next.ForceUpdate(), ErrPricingPullDisabled)
	repo.err = errors.New("database unavailable")
	require.Error(t, next.UpdateControls(context.Background(), PricingControls{RemoteEnabled: true}))
	require.Equal(t, want, next.GetControls())
	require.Error(t, next.loadControls(context.Background()))
	require.Zero(t, client.calls.Load())
}

func TestNativeCNYRechargeRatioIsOne(t *testing.T) {
	svc := &PaymentConfigService{}
	values := map[string]string{SettingBalanceRechargeMult: "0.14", SettingKeyPricingControls: `{"native_cny_enabled":true,"remote_enabled":false}`}
	require.Equal(t, 1.0, svc.parsePaymentConfig(values).BalanceRechargeMultiplier)
	values[SettingKeyPricingControls] = `{"native_cny_enabled":false}`
	require.Equal(t, 0.14, svc.parsePaymentConfig(values).BalanceRechargeMultiplier)
}

// The calendar save is held while an older switch-only request is submitted.
// UpdateControls owns its persistence lock throughout this barrier. No sleep or
// scheduler timing assertion is used: the old payload must omit the calendar.
type calendarBlockingSettingsRepo struct {
	pricingSettingsRepo
	entered   chan struct{}
	release   chan struct{}
	blockOnce sync.Once
}

func (r *calendarBlockingSettingsRepo) Set(ctx context.Context, key, value string) error {
	var payload PricingControls
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return err
	}
	if len(payload.DeepSeekHolidays) > 0 {
		r.blockOnce.Do(func() {
			close(r.entered)
			select {
			case <-r.release:
			case <-ctx.Done():
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.pricingSettingsRepo.Set(ctx, key, value)
}

func TestPricingControlsConcurrentOmissionRetainsNewestCalendar(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := &calendarBlockingSettingsRepo{pricingSettingsRepo: pricingSettingsRepo{values: map[string]string{}}, entered: make(chan struct{}), release: make(chan struct{})}
	pricing := NewPricingService(&config.Config{}, nil)
	pricing.settingsRepo = repo
	// This is the old UI payload constructed before the new calendar request.
	oldSwitch := PricingControls{RemoteEnabled: false, NativeCNYEnabled: false}
	require.Nil(t, oldSwitch.DeepSeekHolidays)
	calendar := map[string][]string{"2026": {"2026-10-12"}}
	calendarDone, switchDone := make(chan error, 1), make(chan error, 1)
	go func() {
		calendarDone <- pricing.UpdateControls(ctx, PricingControls{RemoteEnabled: true, NativeCNYEnabled: true, DeepSeekHolidays: calendar})
	}()
	select {
	case <-repo.entered:
	case <-ctx.Done():
		t.Fatal("calendar save did not reach the persistence barrier")
	}
	switchStarted := make(chan struct{})
	go func() { close(switchStarted); switchDone <- pricing.UpdateControls(ctx, oldSwitch) }()
	<-switchStarted
	// Release the new calendar's persistence only after the old switch request
	// is active. Its omitted calendar is merged against the post-commit version.
	close(repo.release)
	select {
	case err := <-calendarDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("calendar update did not finish")
	}
	select {
	case err := <-switchDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("switch update did not finish")
	}
	want := PricingControls{RemoteEnabled: false, NativeCNYEnabled: false, DeepSeekHolidays: calendar}
	require.Equal(t, want, pricing.GetControls())
	var persisted PricingControls
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyPricingControls]), &persisted))
	require.Equal(t, want, persisted, "database and memory retain the newly saved holidays")
	restarted := NewPricingService(&config.Config{}, nil)
	restarted.settingsRepo = repo
	require.NoError(t, restarted.loadControls(ctx))
	require.Equal(t, want, restarted.GetControls())
	// The explicit empty object remains a deliberate clear, not omission.
	require.NoError(t, pricing.UpdateControls(ctx, PricingControls{DeepSeekHolidays: map[string][]string{}}))
	require.Empty(t, pricing.GetControls().DeepSeekHolidays)
	persisted = PricingControls{}
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyPricingControls]), &persisted))
	require.Empty(t, persisted.DeepSeekHolidays)
}
