package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type noPricingNetwork struct{}

func (noPricingNetwork) FetchPricingJSON(context.Context, string) ([]byte, error) {
	panic("pricing network must be disabled")
}
func (noPricingNetwork) FetchHashText(context.Context, string) (string, error) {
	panic("pricing network must be disabled")
}

func TestPricingControlHTTPDisableAndPublicCacheInvalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	pricing := service.NewPricingService(cfg, noPricingNetwork{})
	settings := &service.SettingService{}
	invalidations := 0
	settings.SetOnUpdateCallback(func() { invalidations++ })
	h := NewChannelHandler(nil, nil, pricing, settings)
	router := gin.New()
	router.PUT("/pricing/settings", h.UpdatePricingControls)
	router.POST("/pricing/refresh", h.RefreshPricing)
	saved := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/pricing/settings", strings.NewReader(`{"remote_enabled":false,"native_cny_enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(saved, req)
	require.Equal(t, http.StatusOK, saved.Code)
	require.False(t, pricing.GetControls().RemoteEnabled)
	require.True(t, pricing.GetControls().NativeCNYEnabled)
	require.Equal(t, 1, invalidations)
	blocked := httptest.NewRecorder()
	router.ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/pricing/refresh", nil))
	require.Equal(t, http.StatusConflict, blocked.Code)
}
