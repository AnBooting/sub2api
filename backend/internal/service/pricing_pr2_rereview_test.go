//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type blockedCalendarAlertSender struct {
	started, release chan struct{}
	calls            atomic.Int32
}

func (s *blockedCalendarAlertSender) SendPricingSyncAlert(context.Context, string, string, string) error {
	if s.calls.Add(1) == 1 {
		close(s.started)
	}
	<-s.release
	return errors.New("SMTP unavailable")
}

func TestPR2RereviewMissingYearLookupDoesNotWaitForAlert(t *testing.T) {
	bs := newNativeBillingTest(t)
	s := bs.pricingService
	s.settingsRepo = alertTestSettings()
	sender := &blockedCalendarAlertSender{started: make(chan struct{}), release: make(chan struct{})}
	s.alertSender = sender
	at := time.Date(2027, 10, 1, 2, 0, 0, 0, time.UTC)
	alertDone := make(chan struct{})
	go func() { s.checkDeepseekCalendarCoverage(at); close(alertDone) }()
	var release sync.Once
	defer func() { release.Do(func() { close(sender.release) }); <-alertDone }()
	<-sender.started
	lookupDone := make(chan struct{})
	go func() {
		defer close(lookupDone)
		for i := 0; i < 20; i++ {
			for _, model := range []string{"glm-test", "deepseek-flash"} {
				p, err := s.nativePricingAt(model, at)
				if err != nil {
					t.Error(err)
					return
				}
				if model == "glm-test" && p.NativeCard.holidayCalendar != nil {
					t.Error("non-DeepSeek pricing captured a calendar")
				}
			}
		}
	}()
	select {
	case <-lookupDone:
	case <-time.After(time.Second):
		t.Fatal("pricing lookup blocked on alert I/O")
	}
	require.EqualValues(t, 1, sender.calls.Load(), "lookups must not send alerts or launch additional workers")
	release.Do(func() { close(sender.release) })
	<-alertDone
	// Failed delivery releases suppression, but further pricing reads still do
	// no I/O; the next notification attempt belongs to the background task.
	_, err := s.nativePricingAt("deepseek-flash", at)
	require.NoError(t, err)
	require.EqualValues(t, 1, sender.calls.Load())
}

type transientPricingAlertSettings struct {
	*pricingReviewSettings
	fail bool
}

func (s *transientPricingAlertSettings) GetValue(ctx context.Context, key string) (string, error) {
	if s.fail {
		return "", errors.New("temporary database failure")
	}
	return s.pricingReviewSettings.GetValue(ctx, key)
}

func TestPR2RereviewConfigReadFailureAllowsNextAlertAttempt(t *testing.T) {
	s := NewPricingService(&config.Config{}, nil)
	repo := &transientPricingAlertSettings{pricingReviewSettings: alertTestSettings(), fail: true}
	s.settingsRepo = repo
	spy := &pricingAlertSpy{}
	s.alertSender = spy
	s.recordUSDPricingSyncResult(errors.New("outage"))
	require.NotContains(t, s.syncAlertTimes, "USD")
	repo.fail = false
	s.recordUSDPricingSyncResult(errors.New("outage"))
	require.Equal(t, []string{"USD"}, spy.calls)
}

func TestPR2RereviewGatewayCalendarFrozenThroughEstimateAndRecordUsage(t *testing.T) {
	for _, currency := range []string{"USD", "CNY"} {
		for _, gateway := range []string{"shared", "openai"} {
			for _, startHoliday := range []bool{false, true} {
				t.Run(currency+"/"+gateway+"/holiday="+map[bool]string{true: "true", false: "false"}[startHoliday], func(t *testing.T) {
					bs := newNativeBillingTest(t)
					if currency == "USD" {
						require.NoError(t, bs.pricingService.UpdateControls(context.Background(), PricingControls{}))
					}
					if startHoliday {
						require.NoError(t, bs.pricingService.UpdateDeepseekHolidays(context.Background(), []string{"2026-10-12"}))
					}
					usage := &openAIRecordUsageLogRepoStub{inserted: true}
					userRepo := &openAIRecordUsageUserRepoStub{}
					subRepo := &openAIRecordUsageSubRepoStub{}
					shared := newGatewayRecordUsageServiceForTest(usage, userRepo, subRepo)
					openai := newOpenAIRecordUsageServiceForTest(usage, userRepo, subRepo, nil)
					shared.billingService, openai.billingService = bs, bs
					shared.resolver = NewModelPricingResolver(nil, bs)
					openai.resolver = NewModelPricingResolver(nil, bs)
					gid := int64(1)
					user := &User{ID: 1, Balance: 100}
					key := &APIKey{ID: 1, User: user, GroupID: &gid, Group: &Group{ID: gid, Platform: PlatformDeepseek, RateMultiplier: 1}}
					account := &Account{ID: 1, Platform: PlatformDeepseek, Type: AccountTypeAPIKey}
					at := time.Date(2026, 10, 12, 2, 0, 0, 0, time.UTC)
					capture := func() context.Context {
						var ctx context.Context
						if gateway == "shared" {
							ctx, _ = shared.WithGatewayTokenRequestPricing(context.Background())
						} else {
							ctx, _ = openai.WithOpenAIRequestPricingContext(context.Background(), &gid)
						}
						return ctx
					}
					deps := shared.inflightEstimateDeps()
					if gateway == "openai" {
						deps = openai.inflightEstimateDeps()
					}
					oldCtx := capture()
					oldRules := RequestPricingSnapshotFromContext(oldCtx)
					require.NotNil(t, oldRules)
					resolved := shared.resolver.Resolve(oldCtx, PricingInput{Model: "deepseek-flash", Group: key.Group, GroupID: &gid, PricingAt: at})
					req := InflightEstimateRequest{Model: "deepseek-flash", BodyBytes: 4_000_000, MaxTokens: 1, PricingAt: at}
					before, ok := deps.estimate(oldCtx, key, req)
					require.True(t, ok)
					if startHoliday {
						require.NoError(t, bs.pricingService.UpdateDeepseekHolidays(context.Background(), nil))
					} else {
						require.NoError(t, bs.pricingService.UpdateDeepseekHolidays(context.Background(), []string{"2026-10-12"}))
					}
					after, ok := deps.estimate(oldCtx, key, req)
					require.True(t, ok)
					require.Equal(t, before, after, "reservation keeps admission calendar")
					base := .15
					if currency == "CNY" {
						base = 1
					}
					wantOld, wantNew := base*2, base
					if startHoliday {
						wantOld, wantNew = base, base*2
					}
					bd, err := bs.CalculateCostUnified(CostInput{Model: "deepseek-flash", Tokens: UsageTokens{InputTokens: 1_000_000}, PricingAt: at, Resolved: resolved, RateMultiplier: 1})
					require.NoError(t, err)
					require.InDelta(t, wantOld, bd.ActualCost, 1e-12, "already resolved prices retain their calendar")
					record := func(ctx context.Context, id string) float64 {
						// The worker receives a fresh context with only the whitelisted
						// snapshot; no request body or cancelled parent is required.
						worker := WithRequestPricingSnapshot(context.Background(), RequestPricingSnapshotFromContext(ctx))
						if gateway == "shared" {
							require.NoError(t, shared.RecordUsage(worker, &RecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: at,
								Result: &ForwardResult{RequestID: id, Model: "deepseek-flash", Usage: ClaudeUsage{InputTokens: 1_000_000}}}))
						} else {
							require.NoError(t, openai.RecordUsage(worker, &OpenAIRecordUsageInput{APIKey: key, User: user, Account: account, PricingAt: at,
								Result: &OpenAIForwardResult{RequestID: id, Model: "deepseek-flash", Usage: OpenAIUsage{InputTokens: 1_000_000}}}))
						}
						require.NotNil(t, usage.lastLog)
						return usage.lastLog.ActualCost
					}
					require.InDelta(t, wantOld, record(oldCtx, "old-request"), 1e-12)
					if gateway == "openai" {
						worker := WithRequestPricingSnapshot(context.Background(), oldRules)
						openai.RecordCyberPolicyUsageLog(worker, CyberPolicyUsageInput{APIKey: key, Account: account,
							RequestID: "old-cyber-request", Model: "deepseek-flash", InputTokens: 1_000_000, PricingAt: at})
						require.NotNil(t, usage.lastLog)
						require.Equal(t, "old-cyber-request", usage.lastLog.RequestID)
						require.InDelta(t, wantOld, usage.lastLog.ActualCost, 1e-12)
					}
					newCtx := capture()
					require.NotSame(t, oldRules, RequestPricingSnapshotFromContext(newCtx))
					require.InDelta(t, wantNew, record(newCtx, "new-request"), 1e-12)
					if gateway == "openai" {
						turnCtx, _ := openai.WithOpenAITurnPricingContext(oldCtx, &gid)
						require.InDelta(t, wantNew, record(turnCtx, "new-turn"), 1e-12)
					}
				})
			}
		}
	}
}
