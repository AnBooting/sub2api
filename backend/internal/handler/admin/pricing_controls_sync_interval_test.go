package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// PRD-01/K-6: the pricing sync period must be administrator-configurable and the
// status must report the period actually in effect, not just the stored override.
func TestPricingControlSyncIntervalHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.UpdateIntervalHours = 24
	pricing := service.NewPricingService(cfg, noPricingNetwork{})
	h := NewChannelHandler(nil, nil, pricing, nil)
	router := gin.New()
	router.PUT("/pricing/settings", h.UpdatePricingControls)
	router.GET("/pricing/settings", h.GetPricingControls)

	put := func(body string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/pricing/settings", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(result, request)
		return result
	}

	// Before any override the config default is what is in effect.
	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/pricing/settings", nil))
	require.Equal(t, http.StatusOK, status.Code)
	var before map[string]any
	require.NoError(t, json.Unmarshal(status.Body.Bytes(), &before))
	require.EqualValues(t, 0, before["data"].(map[string]any)["update_interval_hours"], "no override stored yet")
	require.EqualValues(t, 24, before["data"].(map[string]any)["effective_interval_hours"], "config default is in effect")

	// A valid override is persisted and becomes the effective period.
	require.Equal(t, http.StatusOK, put(`{"remote_enabled":true,"native_cny_enabled":true,"update_interval_hours":6}`).Code)
	require.Equal(t, 6, pricing.GetControls().UpdateIntervalHours)
	require.Equal(t, 6, pricing.PricingSyncIntervalHours())

	// Omitting the field preserves the stored override: a switch-only update must
	// not silently drop the administrator's period.
	require.Equal(t, http.StatusOK, put(`{"remote_enabled":false,"native_cny_enabled":true}`).Code)
	require.Equal(t, 6, pricing.GetControls().UpdateIntervalHours, "omission preserves the override")
	require.Equal(t, 6, pricing.PricingSyncIntervalHours())
}

// Out-of-range periods are rejected with 400 and leave the stored value untouched.
func TestPricingControlSyncIntervalBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.UpdateIntervalHours = 12
	pricing := service.NewPricingService(cfg, noPricingNetwork{})
	h := NewChannelHandler(nil, nil, pricing, nil)
	router := gin.New()
	router.PUT("/pricing/settings", h.UpdatePricingControls)

	put := func(body string) int {
		result := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/pricing/settings", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(result, request)
		return result.Code
	}

	for _, body := range []string{
		`{"remote_enabled":true,"native_cny_enabled":true,"update_interval_hours":0}`,
		`{"remote_enabled":true,"native_cny_enabled":true,"update_interval_hours":-1}`,
		`{"remote_enabled":true,"native_cny_enabled":true,"update_interval_hours":721}`,
	} {
		require.Equal(t, http.StatusBadRequest, put(body), "rejected: %s", body)
	}
	require.Equal(t, 0, pricing.GetControls().UpdateIntervalHours, "a rejected write stores nothing")
	require.Equal(t, 12, pricing.PricingSyncIntervalHours(), "config default still applies")
}

// The resolver falls back to the config file when no override is stored, and the
// bounds are validated independently of HTTP.
func TestPricingSyncIntervalResolver(t *testing.T) {
	require.NoError(t, service.ValidatePricingSyncIntervalHours(0), "0 means not overridden")
	require.NoError(t, service.ValidatePricingSyncIntervalHours(1))
	require.NoError(t, service.ValidatePricingSyncIntervalHours(720))
	require.ErrorIs(t, service.ValidatePricingSyncIntervalHours(-1), service.ErrInvalidPricingSyncInterval)
	require.ErrorIs(t, service.ValidatePricingSyncIntervalHours(721), service.ErrInvalidPricingSyncInterval)

	cfg := &config.Config{}
	cfg.Pricing.UpdateIntervalHours = 48
	pricing := service.NewPricingService(cfg, noPricingNetwork{})
	require.Equal(t, 48, pricing.PricingSyncIntervalHours(), "config default when no override")

	require.NoError(t, pricing.UpdateControls(context.Background(), service.PricingControls{
		RemoteEnabled: true, NativeCNYEnabled: true, UpdateIntervalHours: 3,
	}))
	require.Equal(t, 3, pricing.PricingSyncIntervalHours(), "override wins over config")
}
