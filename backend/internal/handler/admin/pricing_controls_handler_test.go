package admin

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// noPricingNetwork 禁止测试期间真的去拉定价。
//
// 它返回 error 而不是 panic：打开远程拉取后 handler 会通过 RefreshNativeCNY 起一个
// **后台 goroutine**，goroutine 里的 panic 会直接干掉整个测试二进制
// （是否触发取决于调度 → CI 偶发红，根因就是这里）。
//
// 改成返回 error 后守卫意图不变：
//   - 前台路径真拉了 → handler 报错 → 现有断言（200 OK / 控件状态 / invalidations 次数）立刻红；
//   - 后台 goroutine 拉了 → 定价同步记录一次失败（可见日志），测试不崩、也不跨用例干扰
//     （测试用 t.Cleanup(pricing.Stop) 等它退出）。
type noPricingNetwork struct{}

func (noPricingNetwork) FetchPricingJSON(context.Context, string) ([]byte, error) {
	return nil, errors.New("pricing network must be disabled")
}

func (noPricingNetwork) FetchHashText(context.Context, string) (string, error) {
	return "", errors.New("pricing network must be disabled")
}

func TestPricingControlHTTPDisableAndPublicCacheInvalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	pricing := service.NewPricingService(cfg, noPricingNetwork{})
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
}

func TestDeepseekHolidayHTTPValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pricing := service.NewPricingService(&config.Config{}, noPricingNetwork{})
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
}

func TestPricingControlHolidayHTTPValidationAndOmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pricing := service.NewPricingService(&config.Config{}, noPricingNetwork{})
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
}
