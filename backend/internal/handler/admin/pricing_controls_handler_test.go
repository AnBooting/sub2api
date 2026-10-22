package admin

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// noPricingNetwork 是单元测试的「禁止联网」哨兵：任何一次出站定价请求都会
// 被记下来，让测试能断言服务确实没有联网。它同时 panic，是为了让同步路径上的
// 越界请求立刻暴露；后台刷新路径上的 panic 由服务自己收敛成一次可见的同步失败
// （见 service.PricingService），那里靠 attemptCount 断言。
type noPricingNetwork struct {
	mu       sync.Mutex
	attempts int
}

func (n *noPricingNetwork) FetchPricingJSON(context.Context, string) ([]byte, error) {
	n.mu.Lock()
	n.attempts++
	n.mu.Unlock()
	panic("pricing network must be disabled")
}
func (n *noPricingNetwork) FetchHashText(context.Context, string) (string, error) {
	n.mu.Lock()
	n.attempts++
	n.mu.Unlock()
	panic("pricing network must be disabled")
}

// attemptCount 返回出站定价请求的次数。
func (n *noPricingNetwork) attemptCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.attempts
}

func TestPricingControlHTTPDisableAndPublicCacheInvalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	net := &noPricingNetwork{}
	pricing := service.NewPricingService(cfg, net)
	// The service owns background refresh tasks: stop it so no goroutine can
	// outlive this test case.
	t.Cleanup(pricing.Stop)
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
	require.Zero(t, net.attemptCount(), "the disabled remote pull must not reach the network")
}

func TestDeepseekHolidayHTTPValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	net := &noPricingNetwork{}
	pricing := service.NewPricingService(&config.Config{}, net)
	t.Cleanup(pricing.Stop)
	h := NewChannelHandler(nil, nil, pricing, nil)
	router := gin.New()
	router.PUT("/holidays", h.UpdateDeepseekHolidays)
	for _, body := range []string{`{}`, `{"dates":null}`, `{"dates":["2026-02-30"]}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/holidays", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	for _, body := range []string{`{"dates":["2026-10-02","2026-10-01"]}`, `{"dates":[]}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/holidays", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}
	require.Contains(t, pricing.DeepseekHolidayDates(), "2026-10-01", "empty list restores built-in holidays")
	require.Zero(t, net.attemptCount(), "updating the holiday calendar must not reach the network")
}

func TestPricingControlHolidayHTTPValidationAndOmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	net := &noPricingNetwork{}
	pricing := service.NewPricingService(&config.Config{}, net)
	t.Cleanup(pricing.Stop)
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
	// Synthetic holiday fixture only; no production calendar is installed.
	require.Equal(t, http.StatusOK, put(`{"remote_enabled":false,"native_cny_enabled":true,"deepseek_holidays":{"2026":["2026-10-12"],"2027":[]}}`).Code)
	require.Equal(t, http.StatusOK, put(`{"remote_enabled":false,"native_cny_enabled":false}`).Code)
	require.Equal(t, []string{"2026-10-12"}, pricing.GetControls().DeepSeekHolidays["2026"])
	for _, calendar := range []string{
		`{"2026":["2026-02-30"]}`, `{"2026":["2027-01-01"]}`,
		`{"2026":["2026-10-12","2026-10-12"]}`, `{"bad":[]}`, `{"+123":[]}`, `{"2026":null}`, `null`, `[]`, `true`,
	} {
		body := `{"remote_enabled":false,"native_cny_enabled":false,"deepseek_holidays":` + calendar + "}"
		require.Equal(t, http.StatusBadRequest, put(body).Code)
		require.Equal(t, []string{"2026-10-12"}, pricing.GetControls().DeepSeekHolidays["2026"])
	}
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/pricing/settings", nil))
	var payload struct {
		Data struct {
			Holidays map[string][]string `json:"deepseek_holidays"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &payload))
	require.Equal(t, []string{"2026-10-12"}, payload.Data.Holidays["2026"])
	require.Empty(t, payload.Data.Holidays["2027"])
	require.Equal(t, http.StatusOK, put(`{"remote_enabled":false,"native_cny_enabled":false,"deepseek_holidays":{}}`).Code)
	require.Empty(t, pricing.GetControls().DeepSeekHolidays)
	require.NotNil(t, pricing.GetControls().DeepSeekHolidays)
	result = httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/pricing/settings", nil))
	require.Contains(t, result.Body.String(), `"deepseek_holidays":{}`)
	require.Zero(t, net.attemptCount(), "a switch-only update with the remote pull disabled must not reach the network")
}
