package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type pricingReviewSettings struct {
	SettingRepository
	values map[string]string
}

func (r *pricingReviewSettings) GetValue(_ context.Context, key string) (string, error) {
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}
func (r *pricingReviewSettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string)
	for _, k := range keys {
		values[k] = r.values[k]
	}
	return values, nil
}
func (r *pricingReviewSettings) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

type pricingAlertSpy struct {
	calls []string
	fail  bool
}

func (s *pricingAlertSpy) SendPricingSyncAlert(_ context.Context, _, source, _ string) error {
	s.calls = append(s.calls, source)
	if s.fail {
		return errors.New("SMTP unavailable")
	}
	return nil
}
func alertTestSettings() *pricingReviewSettings {
	return &pricingReviewSettings{values: map[string]string{SettingKeyOpsEmailNotificationConfig: `{"alert":{"enabled":true,"recipients":["admin@example.test"],"min_severity":"warning","rate_limit_per_hour":100}}`}}
}

func TestPricingSyncAlertSuppressionRecoveryAndCancellation(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	s.settingsRepo = alertTestSettings()
	sender := &pricingAlertSpy{}
	s.alertSender = sender
	s.recordNativeCNYError(errors.New("first failure"))
	s.recordNativeCNYError(errors.New("different message, same ongoing outage"))
	require.Equal(t, []string{"CNY"}, sender.calls)
	s.recordPricingSyncResult("CNY", nil)
	s.recordNativeCNYError(errors.New("new outage"))
	require.Equal(t, []string{"CNY", "CNY"}, sender.calls)
	s.recordUSDPricingSyncResult(ErrPricingPullDisabled)
	s.recordUSDPricingSyncResult(context.Canceled)
	require.Len(t, sender.calls, 2)
	s.recordUSDPricingSyncResult(errors.New("USD sync failed"))
	require.Equal(t, "USD", sender.calls[2])
	require.NotEmpty(t, s.GetPricingControlStatus()["usd_error"])
	s.recordUSDPricingSyncResult(nil)
	require.Empty(t, s.GetPricingControlStatus()["usd_error"])
}

func TestPricingSyncAlertRetriesFailedEmailAndHonorsSettings(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	s.settingsRepo = alertTestSettings()
	sender := &pricingAlertSpy{fail: true}
	s.alertSender = sender
	s.recordPricingSyncResult("USD", errors.New("sync failed"))
	sender.fail = false
	s.recordPricingSyncResult("USD", errors.New("sync failed"))
	require.Len(t, sender.calls, 2)
	s.settingsRepo.(*pricingReviewSettings).values[SettingKeyOpsEmailNotificationConfig] = `{"alert":{"enabled":false}}`
	s.recordPricingSyncResult("CNY", errors.New("sync failed"))
	require.Len(t, sender.calls, 2)
}

type failedPricingReviewClient struct{}

func (failedPricingReviewClient) FetchPricingJSON(context.Context, string) ([]byte, error) {
	return nil, errors.New("price source unreachable")
}
func (failedPricingReviewClient) FetchHashText(context.Context, string) (string, error) {
	return "", errors.New("hash source unreachable")
}

func TestPricingSyncFailureRetainsLastGoodAndAlerts(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Pricing.RemoteURL = "https://example.com/usd.json"
	cfg.Pricing.NativeCNYURL = "https://example.com/cny.json"
	s := NewPricingService(cfg, failedPricingReviewClient{})
	s.settingsRepo = alertTestSettings()
	sender := &pricingAlertSpy{}
	s.alertSender = sender
	cards, err := parseNativeCNYCatalog([]byte(nativeCatalogFixture), "fixture", time.Now())
	require.NoError(t, err)
	s.nativeCNYCards = cards
	usd := &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6}
	s.pricingData["test-model"] = usd
	require.NoError(t, s.UpdateControls(context.Background(), PricingControls{RemoteEnabled: true, NativeCNYEnabled: true}))
	require.Error(t, s.syncNativeCNY(true))
	require.Same(t, cards["glmtest"], s.nativeCNYCards["glmtest"])
	require.Error(t, s.downloadPricingData())
	require.Same(t, usd, s.pricingData["test-model"])
	require.Equal(t, []string{"CNY", "USD"}, sender.calls)
}

func TestPricingManualRefreshDoesNotDuplicateFailedAlertDelivery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Pricing.NativeCNYURL = "https://example.com/cny.json"
	s := NewPricingService(cfg, failedPricingReviewClient{})
	s.settingsRepo = alertTestSettings()
	sender := &pricingAlertSpy{fail: true}
	s.alertSender = sender
	require.NoError(t, s.UpdateControls(context.Background(), PricingControls{RemoteEnabled: true, NativeCNYEnabled: true}))
	require.Error(t, s.ForceUpdate())
	require.Equal(t, []string{"CNY"}, sender.calls)
	require.Error(t, s.ForceUpdate())
	require.Equal(t, []string{"CNY", "CNY"}, sender.calls)
}

func TestDeepseekHolidaySettingsPersistAndReload(t *testing.T) {
	repo := alertTestSettings()
	s := NewPricingService(&config.Config{}, nil)
	s.settingsRepo = repo
	require.NoError(t, s.UpdateDeepseekHolidays(context.Background(), []string{"2026-10-02", "2026-10-01", "2026-10-01"}))
	fresh := NewPricingService(&config.Config{}, nil)
	fresh.settingsRepo = repo
	require.NoError(t, fresh.loadControls(context.Background()))
	require.Equal(t, []string{"2026-10-01", "2026-10-02"}, fresh.DeepseekHolidayDates())
	before := repo.values[SettingKeyDeepseekHolidays]
	require.Error(t, fresh.UpdateDeepseekHolidays(context.Background(), []string{"2026-02-30"}))
	require.Equal(t, before, repo.values[SettingKeyDeepseekHolidays])
}
