//go:build unit

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayBudgetSettingsHandler(repo *settingHandlerRepoStub) *SettingHandler {
	svc := service.NewSettingService(repo, &config.Config{})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
}

func putSettings(t *testing.T, handler *SettingHandler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateSettings(c)
	return rec
}

// 硬约束：显式传非正值必须 400 且不写库。
func TestSettingHandler_GatewayRequestAttemptBudget_RejectsNonPositive(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		body map[string]any
	}{
		{name: "max zero", body: map[string]any{"gateway_max_attempts_per_request": 0}},
		{name: "max negative", body: map[string]any{"gateway_max_attempts_per_request": -3}},
		{name: "window zero", body: map[string]any{"gateway_request_budget_seconds": 0}},
		{name: "window negative", body: map[string]any{"gateway_request_budget_seconds": -600}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			handler := newGatewayBudgetSettingsHandler(repo)

			rec := putSettings(t, handler, tc.body)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, repo.lastUpdates, "non-positive budget must not be persisted")
		})
	}
}

func TestSettingHandler_GatewayRequestAttemptBudget_PersistsProvidedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{}}
	handler := newGatewayBudgetSettingsHandler(repo)

	rec := putSettings(t, handler, map[string]any{
		"gateway_max_attempts_per_request": 32,
		"gateway_request_budget_seconds":   900,
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "32", repo.values[service.SettingKeyGatewayMaxAttemptsPerRequest])
	require.Equal(t, "900", repo.values[service.SettingKeyGatewayRequestBudgetSeconds])
}

// 省略字段 = 保持现值（指针语义），不会被零值覆盖成非法预算。
func TestSettingHandler_GatewayRequestAttemptBudget_OmittedKeepsStoredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyGatewayMaxAttemptsPerRequest: "20",
		service.SettingKeyGatewayRequestBudgetSeconds:  "300",
	}}
	handler := newGatewayBudgetSettingsHandler(repo)

	rec := putSettings(t, handler, map[string]any{"frontend_url": "https://example.test"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "20", repo.values[service.SettingKeyGatewayMaxAttemptsPerRequest])
	require.Equal(t, "300", repo.values[service.SettingKeyGatewayRequestBudgetSeconds])
}

func TestSettingHandler_GatewayRequestAttemptBudget_GetEchoesStoredValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// GetSettings → GetAllSettings → parseSettings 会把 Grok 运行时模型映射发布到
	// xai 包的进程级全局状态（setting_parse.go 的 SetRuntimeModelMappingOptions）。
	// 本包内目前没有在其后读取该状态的用例，但这是与 service 包同一类的顺序依赖，
	// 按 account_wildcard_test.go 的既有做法保存并还原，避免将来复现。
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })

	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyGatewayMaxAttemptsPerRequest: "24",
		service.SettingKeyGatewayRequestBudgetSeconds:  "480",
	}}
	handler := newGatewayBudgetSettingsHandler(repo)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	handler.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			GatewayMaxAttemptsPerRequest int `json:"gateway_max_attempts_per_request"`
			GatewayRequestBudgetSeconds  int `json:"gateway_request_budget_seconds"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 24, resp.Data.GatewayMaxAttemptsPerRequest)
	require.Equal(t, 480, resp.Data.GatewayRequestBudgetSeconds)
}
