//go:build unit

package service

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPR2CalendarFallbackAndMissingYearAlerts(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{bad json`, `["2026-02-30"]`, `["2025-01-01"]`} {
		t.Run(raw, func(t *testing.T) {
			s := NewPricingService(&config.Config{}, nil)
			repo := alertTestSettings()
			repo.values[SettingKeyDeepseekHolidays] = raw
			s.settingsRepo = repo
			s.alertSender = &pricingAlertSpy{}
			require.NoError(t, s.loadControls(context.Background()))
			at := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
			require.Equal(t, 1.0, deepseekPeakMultiplierWithCalendar(at, s.deepseekCalendarAt(at)))
			if raw == `{bad json` || raw == `["2026-02-30"]` {
				require.NotEmpty(t, s.GetPricingControlStatus()["deepseek_calendar_error"])
			}
		})
	}
	// Missing years must never charge a potential holiday at 2x. Each service
	// instance independently alerts, including when it starts after another.
	for i := 0; i < 2; i++ {
		s := NewPricingService(&config.Config{}, nil)
		s.settingsRepo = alertTestSettings()
		spy := &pricingAlertSpy{}
		s.alertSender = spy
		at := time.Date(2027, 10, 1, 2, 0, 0, 0, time.UTC)
		require.Equal(t, 1.0, deepseekPeakMultiplierWithCalendar(at, s.deepseekCalendarAt(at)))
		s.checkDeepseekCalendarCoverage(at)
		s.checkDeepseekCalendarCoverage(at)
		require.Equal(t, []string{"DeepSeek calendar 2027"}, spy.calls)
		require.Contains(t, s.GetPricingControlStatus()["deepseek_calendar_years"], 2026)
	}
}

func TestPR2CalendarResetAndRejectMissingCurrentYear(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	s.settingsRepo = alertTestSettings()
	year := time.Now().In(time.FixedZone("Beijing", 8*3600)).Year()
	require.NoError(t, s.UpdateDeepseekHolidays(context.Background(), []string{strconv.Itoa(year) + "-12-01"}))
	before := s.DeepseekHolidayDates()
	require.ErrorIs(t, s.UpdateDeepseekHolidays(context.Background(), []string{strconv.Itoa(year+1) + "-01-01"}), ErrInvalidDeepseekHolidays)
	require.Equal(t, before, s.DeepseekHolidayDates())
	require.NoError(t, s.UpdateDeepseekHolidays(context.Background(), nil))
	require.Equal(t, defaultDeepseekCalendar.dates, s.DeepseekHolidayDates())
}

type blockedCalendarSettings struct {
	SettingRepository
	started, release chan struct{}
}

func (r *blockedCalendarSettings) Set(context.Context, string, string) error {
	close(r.started)
	<-r.release
	return errors.New("database unavailable")
}

func TestPR2CalendarDatabaseWriteDoesNotBlockBilling(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	r := &blockedCalendarSettings{started: make(chan struct{}), release: make(chan struct{})}
	s.settingsRepo = r
	done := make(chan error, 1)
	year := time.Now().In(time.FixedZone("Beijing", 8*3600)).Year()
	go func() {
		done <- s.UpdateDeepseekHolidays(context.Background(), []string{strconv.Itoa(year) + "-12-01"})
	}()
	<-r.started
	read := make(chan []string, 1)
	go func() { read <- s.DeepseekHolidayDates() }()
	select {
	case dates := <-read:
		require.Equal(t, defaultDeepseekCalendar.dates, dates)
	case <-time.After(time.Second):
		t.Error("calendar reader blocked on database write")
	}
	close(r.release)
	require.Error(t, <-done)
	require.Equal(t, defaultDeepseekCalendar.dates, s.DeepseekHolidayDates())
}

func TestPR2CalendarReloadConcurrentWithBillingReads(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	s.settingsRepo = alertTestSettings()
	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.DeepseekHolidayDates()
			_, _ = s.deepseekCalendarStatus()
		}
	}()
	for i := 0; i < 100; i++ {
		require.NoError(t, s.loadControls(context.Background()))
		require.NoError(t, s.UpdateDeepseekHolidays(context.Background(), nil))
	}
	close(stop)
	readers.Wait()
}

type partiallyFailingPricingSender struct{ calls int }

func (s *partiallyFailingPricingSender) SendPricingSyncAlert(_ context.Context, recipient, _, _ string) error {
	s.calls++
	if recipient == "failed@example.test" {
		return errors.New("delivery failed")
	}
	return nil
}

func TestPR2PartialAlertDeliveryRetainsSuppression(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	repo := alertTestSettings()
	repo.values[SettingKeyOpsEmailNotificationConfig] = `{"alert":{"enabled":true,"recipients":["admin@example.test","failed@example.test"],"min_severity":"warning","rate_limit_per_hour":100}}`
	s.settingsRepo = repo
	sender := &partiallyFailingPricingSender{}
	s.alertSender = sender
	s.recordUSDPricingSyncResult(errors.New("failure"))
	s.recordUSDPricingSyncResult(errors.New("failure"))
	require.Equal(t, 2, sender.calls, "do not resend to successful recipient in the same outage window")
}

func TestPR2HashFailureDoesNotClearOutage(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.HashURL = "https://example.com/hash"
	s := NewPricingService(cfg, failedPricingReviewClient{})
	s.settingsRepo = alertTestSettings()
	spy := &pricingAlertSpy{}
	s.alertSender = spy
	s.recordUSDPricingSyncResult(errors.New("prior outage"))
	require.Error(t, s.syncWithRemote())
	require.NotEmpty(t, s.GetPricingControlStatus()["usd_error"])
	require.Len(t, spy.calls, 1, "same outage retains suppression")
	require.NoError(t, s.UpdateControls(context.Background(), PricingControls{}))
	require.NoError(t, s.syncWithRemote())
	require.NotEmpty(t, s.GetPricingControlStatus()["usd_error"], "disabled pull is not recovery")
}

func TestPR2PricingDiskFailureIsReported(t *testing.T) {
	for _, target := range []string{"prices", "hash"} {
		t.Run(target, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Pricing.DataDir = t.TempDir()
			cfg.Pricing.RemoteURL = "https://example.com/prices.json"
			s := NewPricingService(cfg, &controlsPricingClient{})
			s.settingsRepo = alertTestSettings()
			spy := &pricingAlertSpy{}
			s.alertSender = spy
			old := &LiteLLMModelPricing{InputCostPerToken: 7e-6}
			s.pricingData["last-good"] = old
			path := s.getPricingFilePath()
			if target == "hash" {
				path = s.getHashFilePath()
			}
			require.NoError(t, os.Mkdir(path, 0700))
			require.Error(t, s.downloadPricingData())
			require.Same(t, old, s.pricingData["last-good"])
			require.NotEmpty(t, s.GetPricingControlStatus()["usd_error"])
			require.Equal(t, []string{"USD"}, spy.calls)
			entries, err := os.ReadDir(cfg.Pricing.DataDir)
			require.NoError(t, err)
			for _, entry := range entries {
				require.NotContains(t, entry.Name(), ".pricing-")
			}
		})
	}
}

func TestPR2USDSyncDoesNotDuplicateFailedDelivery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Pricing.HashURL = "https://example.com/hash"
	cfg.Pricing.RemoteURL = "https://example.com/prices.json"
	s := NewPricingService(cfg, &controlsPricingClient{})
	s.settingsRepo = alertTestSettings()
	spy := &pricingAlertSpy{fail: true}
	s.alertSender = spy
	require.NoError(t, os.Mkdir(s.getPricingFilePath(), 0700))
	require.Error(t, s.syncWithRemote())
	require.Equal(t, []string{"USD"}, spy.calls)
}

type pricingDetailSpy struct{ details []string }

func (s *pricingDetailSpy) SendPricingSyncAlert(_ context.Context, _, _, detail string) error {
	s.details = append(s.details, detail)
	return nil
}

func TestPR2PricingAlertDoesNotExposeURLOrResponse(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	s.settingsRepo = alertTestSettings()
	spy := &pricingDetailSpy{}
	s.alertSender = spy
	s.recordUSDPricingSyncResult(&url.Error{Op: "Get", URL: "https://user:secret@private.internal/prices?token=secret", Err: &net.DNSError{Err: "response body SECRET", Name: "private.internal"}})
	require.Equal(t, []string{"USD: DNS failure"}, spy.details)
	s.recordPricingSyncResult("CNY", errors.New("https://private.internal: response SECRET"))
	require.Equal(t, "CNY: source unavailable or invalid data", spy.details[1])
}

func TestPR2ChannelInheritanceUsesFrozenInstantInBothGateways(t *testing.T) {
	bs := newTestBillingService()
	price, gid := 1e-6, int64(1)
	key := &APIKey{GroupID: &gid, Group: &Group{ID: gid, ModelPricing: []ChannelModelPricing{{Models: []string{"deepseek-v4-pro"}, InputPrice: &price}}}}
	r := NewModelPricingResolver(nil, bs)
	shared := &GatewayService{billingService: bs, resolver: r}
	openai := &OpenAIGatewayService{billingService: bs, resolver: r}
	for _, at := range []time.Time{deepseekProRoutesToFlashAt.Add(-time.Minute), deepseekProRoutesToFlashAt.Add(time.Minute)} {
		ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, at)
		want := deepseekFlashOffPeakOutputPrice
		if at.Before(deepseekProRoutesToFlashAt) {
			want = deepseekProOffPeakOutputPrice
		}
		for _, resolved := range []*ResolvedPricing{shared.resolveChannelPricing(ctx, "deepseek-v4-pro", key), openai.resolveOpenAIChannelPricing(ctx, "deepseek-v4-pro", key)} {
			require.Equal(t, at, resolved.pricingAt)
			require.Equal(t, want, resolved.BasePricing.OutputPricePerToken)
		}
		// A detached context must still use the explicit usage instant.
		cost := shared.calculateRecordUsageCost(context.Background(), &ForwardResult{Usage: ClaudeUsage{OutputTokens: 100}}, key, "deepseek-v4-pro", 1, 1, at)
		require.NotNil(t, cost)
		require.InDelta(t, want*100, cost.OutputCost, 1e-12)
		cost, err := openai.calculateOpenAIRecordUsageCost(context.Background(), &OpenAIForwardResult{}, key, []string{"deepseek-v4-pro"}, 1, 1, 1, 1, UsageTokens{OutputTokens: 100}, "", nil, at)
		require.NoError(t, err)
		require.InDelta(t, want*100, cost.OutputCost, 1e-12)
	}
}

func TestPR2InflightUsesRequestInstantAcrossPeakWindows(t *testing.T) {
	bs := newNativeBillingTest(t)
	d := inflightEstimateDeps{billing: bs, resolver: NewModelPricingResolver(nil, bs)}
	key := &APIKey{User: &User{ID: 1}}
	for _, hour := range []int{1, 4} {
		at := time.Date(2026, 10, 12, hour, 0, 0, 0, time.UTC)
		req := InflightEstimateRequest{Model: "deepseek-flash", MaxTokens: 100, PricingAt: at}
		want := 4e-4
		if hour == 1 {
			want *= 2
		}
		got, ok := d.estimate(context.Background(), key, req)
		require.True(t, ok)
		require.InDelta(t, want, got, 1e-12)
		req.PricingAt = time.Time{}
		ctx := context.WithValue(context.Background(), gatewayTokenRequestPricingAtCtxKey{}, at)
		got, ok = d.estimate(ctx, key, req)
		require.True(t, ok)
		require.InDelta(t, want, got, 1e-12)
	}
}
