package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPricingHolidayRouteRequiresStepUpAndAuditsReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pricing := service.NewPricingService(&config.Config{}, nil)
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Channel: admin.NewChannelHandler(nil, nil, pricing, nil)}}
	r := gin.New()
	var audit map[string]any
	r.Use(func(c *gin.Context) {
		c.Next()
		if value, ok := c.Get("audit_extra"); ok {
			audit, ok = value.(map[string]any)
			require.True(t, ok, "audit extra must be an object")
		}
	})
	gateCalls := 0
	gate := middleware.StepUpAuthMiddleware(func(c *gin.Context) {
		gateCalls++
		if c.GetHeader("X-Test-Step-Up") != "verified" {
			c.AbortWithStatus(http.StatusForbidden)
		}
	})
	registerChannelRoutes(r.Group("/api/v1/admin"), h, gate)
	before := pricing.DeepseekHolidayDates()
	for _, verified := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/channels/pricing/holidays", strings.NewReader(`{"dates":[]}`))
		req.Header.Set("Content-Type", "application/json")
		if verified {
			req.Header.Set("X-Test-Step-Up", "verified")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if verified {
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, before, audit["old_dates"])
			require.Equal(t, pricing.DeepseekHolidayDates(), audit["new_dates"])
		} else {
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Nil(t, audit)
			require.Equal(t, before, pricing.DeepseekHolidayDates())
		}
	}
	require.Equal(t, 2, gateCalls)
}

func TestPricingSettingsCalendarCannotBypassHolidayStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pricing := service.NewPricingService(&config.Config{}, nil)
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Channel: admin.NewChannelHandler(nil, nil, pricing, nil)}}
	r := gin.New()
	var audit map[string]any
	r.Use(func(c *gin.Context) {
		c.Next()
		if extra, ok := c.Get("audit_extra"); ok {
			audit, ok = extra.(map[string]any)
			require.True(t, ok, "audit extra must be an object")
		}
	})
	gateCalls := 0
	gate := middleware.StepUpAuthMiddleware(func(c *gin.Context) {
		gateCalls++
		if c.GetHeader("X-Test-Step-Up") != "verified" {
			c.AbortWithStatus(http.StatusForbidden)
		}
	})
	registerChannelRoutes(r.Group("/api/v1/admin"), h, gate)
	before := pricing.DeepseekHolidayDates()
	for _, verified := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/channels/pricing/settings", strings.NewReader(`{"remote_enabled":false,"native_cny_enabled":false,"deepseek_holidays":{"2026":["2026-10-12"],"2027":[]}}`))
		req.Header.Set("Content-Type", "application/json")
		if verified {
			req.Header.Set("X-Test-Step-Up", "verified")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if verified {
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, before, audit["old_dates"])
			require.Equal(t, []string{"2026-10-12"}, audit["new_dates"])
			require.Contains(t, audit, "new_calendar", "empty maintained years must be audited too")
		} else {
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Equal(t, before, pricing.DeepseekHolidayDates())
			require.Nil(t, audit)
		}
	}
	require.Equal(t, 2, gateCalls)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/channels/pricing/settings", strings.NewReader(`{"remote_enabled":false,"native_cny_enabled":false,"update_interval_hours":6}`))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 2, gateCalls, "switch/interval-only saves retain their existing authentication flow")
}
