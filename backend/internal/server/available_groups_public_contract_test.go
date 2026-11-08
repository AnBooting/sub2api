//go:build unit

package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAvailableGroupsPublicFieldsAndLocalization(t *testing.T) {
	deps := newContractDeps(t)
	price := 0.3
	deps.groupRepo.SetActive([]service.Group{{
		ID: 10, Name: "原名", Description: "原描述", Platform: service.PlatformAnthropic,
		I18n:   domain.GroupI18n{"en": {Name: "Translated", Description: "English description"}},
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard,
		RateMultiplier: 1.5, PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "18:00", PeakRateMultiplier: 2,
		AllowLive: true, AudioRealtimePricePerMin: &price, AllowHighAvailability: true,
		RPMLimit: 100, MaxReasoningEffort: "high", ClaudeCodeOnly: true, DailyLimitUSD: &price, ImagePrice1K: &price,
	}})
	deps.userSubRepo.SetActiveByUserID(1, nil)
	for _, test := range []struct{ language, name, description string }{
		{"en-US,en;q=0.9", "Translated", "English description"},
		{"zh", "原名", "原描述"},
	} {
		t.Run(test.language, func(t *testing.T) {
			status, raw := doRequest(t, deps.router, http.MethodGet, "/api/v1/groups/available", "", map[string]string{"Accept-Language": test.language})
			require.Equal(t, http.StatusOK, status)
			var body struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(raw), &body))
			require.Len(t, body.Data, 1)
			group := body.Data[0]
			fields := []string{"id", "name", "description", "platform", "rate_multiplier", "subscription_type", "status", "peak_rate_enabled", "peak_start", "peak_end", "peak_rate_multiplier", "deepseek_pricing_state", "allow_live", "audio_realtime_price_per_min", "allow_high_availability"}
			require.Len(t, group, len(fields))
			for _, field := range fields {
				require.Contains(t, group, field)
			}
			for _, field := range []string{"rpm_limit", "max_reasoning_effort", "image_price_1k", "daily_limit_usd", "claude_code_only", "created_at", "i18n"} {
				require.NotContains(t, group, field)
			}
			require.Equal(t, test.name, group["name"])
			require.Equal(t, test.description, group["description"])
			require.Equal(t, true, group["allow_high_availability"])
			require.Equal(t, true, group["allow_live"])
			require.Equal(t, true, group["peak_rate_enabled"])
			require.Equal(t, 2.0, group["peak_rate_multiplier"])
			require.Equal(t, price, group["audio_realtime_price_per_min"])
		})
	}
}
