package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

// Only fixed error categories leave the service. Wrapped URL errors and
// remote response bodies may contain internal hosts, credentials or tokens.
func pricingAlertDetail(source string, err error) string {
	category := "source unavailable or invalid data"
	var dns *net.DNSError
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var pathErr *os.PathError
	var network net.Error
	switch {
	case errors.Is(err, ErrDeepseekCalendarUnavailable):
		category = "billing year unavailable; peak pricing disabled"
	case errors.Is(err, ErrInvalidDeepseekHolidays):
		category = "invalid saved calendar; built-in dates restored"
	case errors.Is(err, context.DeadlineExceeded):
		category = "timeout"
	case errors.As(err, &dns):
		category = "DNS failure"
	case errors.As(err, &network) && network.Timeout():
		category = "timeout"
	case errors.As(err, &syntax), errors.As(err, &typeErr):
		category = "invalid pricing data"
	case errors.As(err, &pathErr):
		category = "cache persistence failure"
	}
	return fmt.Sprintf("%s: %s", source, category)
}

type pricingAlertSender interface {
	SendPricingSyncAlert(context.Context, string, string, string) error
}

func (s *EmailService) SendPricingSyncAlert(ctx context.Context, to, source, detail string) error {
	rule := &OpsAlertRule{Name: "Model pricing sync: " + source, Severity: "P1", Description: detail, MetricType: "pricing_sync"}
	event := &OpsAlertEvent{Status: "firing", FiredAt: time.Now().UTC(), Description: detail}
	if s.notificationEmailService != nil {
		err := s.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event: NotificationEmailEventOpsAlert, RecipientEmail: to, RecipientName: emailRecipientName(to),
			SourceType: "pricing_sync", SourceID: source + ":" + event.FiredAt.Format(time.RFC3339Nano), Variables: opsAlertEmailVariables(rule, event),
		})
		if err == nil || !shouldFallbackNotificationEmail(err) {
			return err
		}
	}
	return s.SendEmail(ctx, to, "[Ops Alert] "+rule.Name,
		"<p>Model pricing synchronization failed. Last known good prices are retained.</p><p>"+html.EscapeString(detail)+"</p>")
}

func (s *PricingService) recordPricingSyncResult(source string, err error) {
	if errors.Is(err, ErrPricingPullDisabled) || errors.Is(err, context.Canceled) {
		return
	}
	s.alertsMu.Lock()
	if s.syncAlertTimes == nil {
		s.syncAlertTimes = make(map[string]time.Time)
	}
	if err == nil {
		delete(s.syncAlertTimes, source)
		s.alertsMu.Unlock()
		return
	}
	if last := s.syncAlertTimes[source]; !last.IsZero() && time.Since(last) < time.Hour {
		s.alertsMu.Unlock()
		return
	}
	now := time.Now()
	s.syncAlertTimes[source] = now
	if s.alertEmailLimiter == nil {
		s.alertEmailLimiter = newSlidingWindowLimiter(0, time.Hour)
	}
	limiter := s.alertEmailLimiter
	s.alertsMu.Unlock()
	slog.Warn("Model pricing sync failed; retaining last known good prices", "source", source, "error", err)
	if s.alertSender == nil || s.settingsRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Reuse Ops recipients, severity threshold, rate limiter and email templates.
	cfg, cfgErr := getOpsEmailNotificationConfig(ctx, s.settingsRepo)
	if cfgErr != nil {
		slog.Warn("Could not read pricing alert email settings", "error", cfgErr)
		s.alertsMu.Lock()
		if s.syncAlertTimes[source] == now {
			delete(s.syncAlertTimes, source)
		}
		s.alertsMu.Unlock()
		return
	}
	if !cfg.Alert.Enabled || !shouldSendOpsAlertEmailByMinSeverity(cfg.Alert.MinSeverity, "P1") {
		return
	}
	limiter.SetLimit(cfg.Alert.RateLimitPerHour)
	failed, succeeded := false, false
	for _, recipient := range cfg.Alert.Recipients {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" || !limiter.Allow(time.Now().UTC()) {
			continue
		}
		if sendErr := s.alertSender.SendPricingSyncAlert(ctx, recipient, source, pricingAlertDetail(source, err)); sendErr != nil {
			failed = true
			slog.Warn("Pricing sync alert email delivery failed", "source", source, "error", sendErr)
		} else {
			succeeded = true
		}
	}
	if failed && !succeeded {
		s.alertsMu.Lock()
		if s.syncAlertTimes[source] == now {
			delete(s.syncAlertTimes, source)
		}
		s.alertsMu.Unlock()
	}
}

func (s *PricingService) recordUSDPricingSyncResult(err error) {
	if errors.Is(err, ErrPricingPullDisabled) || errors.Is(err, context.Canceled) {
		return
	}
	s.mu.Lock()
	s.usdPricingError = ""
	if err != nil {
		s.usdPricingError = err.Error()
	}
	s.mu.Unlock()
	s.recordPricingSyncResult("USD", err)
}
