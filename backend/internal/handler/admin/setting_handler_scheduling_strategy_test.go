//go:build unit

package admin

import (
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

func newSchedulingStrategySettingsHandler(repo *settingHandlerRepoStub) *SettingHandler {
	svc := service.NewSettingService(repo, &config.Config{})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
}

// 硬约束：显式传非法值必须 400 且不写库（不静默回落）。
func TestSettingHandler_SchedulingStrategy_RejectsInvalidValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		body map[string]any
	}{
		{name: "empty", body: map[string]any{"scheduling_strategy": ""}},
		{name: "unknown", body: map[string]any{"scheduling_strategy": "turbo"}},
		{name: "global inherit not allowed", body: map[string]any{"scheduling_strategy": "inherit"}},
		{name: "numeric", body: map[string]any{"scheduling_strategy": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			handler := newSchedulingStrategySettingsHandler(repo)

			rec := putSettings(t, handler, tc.body)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, repo.lastUpdates, "invalid scheduling_strategy must not be persisted")
		})
	}
}

func TestSettingHandler_SchedulingStrategy_PersistsProvidedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, value := range []string{"stable", "balanced", "latency"} {
		t.Run(value, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{}}
			handler := newSchedulingStrategySettingsHandler(repo)

			rec := putSettings(t, handler, map[string]any{"scheduling_strategy": value})

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, value, repo.values[service.SettingKeySchedulingStrategy])
		})
	}
}

// 省略字段 = 保持现值（指针语义），不会被零值覆盖成空策略。
func TestSettingHandler_SchedulingStrategy_OmittedKeepsStoredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeySchedulingStrategy: "latency",
	}}
	handler := newSchedulingStrategySettingsHandler(repo)

	rec := putSettings(t, handler, map[string]any{"frontend_url": "https://example.test"})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "latency", repo.values[service.SettingKeySchedulingStrategy])
}

func TestSettingHandler_SchedulingStrategy_GetEchoesStoredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// GetSettings → GetAllSettings → parseSettings 会发布 Grok 运行时模型映射到
	// xai 包的进程级全局状态，按既有做法保存并还原。
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })

	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeySchedulingStrategy: "stable",
	}}
	handler := newSchedulingStrategySettingsHandler(repo)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	handler.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			SchedulingStrategy string `json:"scheduling_strategy"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "stable", resp.Data.SchedulingStrategy)
}

// 缺省时管理 API 回显默认 balanced（C-8）。
func TestSettingHandler_SchedulingStrategy_GetDefaultsToBalanced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })

	repo := &settingHandlerRepoStub{values: map[string]string{}}
	handler := newSchedulingStrategySettingsHandler(repo)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	handler.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			SchedulingStrategy string `json:"scheduling_strategy"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "balanced", resp.Data.SchedulingStrategy)
}
