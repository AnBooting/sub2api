package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
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
	want := PricingControls{NativeCNYEnabled: true, RemoteEnabled: false}
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
