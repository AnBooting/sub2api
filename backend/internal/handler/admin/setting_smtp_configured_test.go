package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// I-01：管理端设置响应必须真实下发只读字段 smtp_configured。
//
// 前端已按该字段实现"SMTP 未配置"的阻断式提示；若后端不下发，
// 前端拿到的永远是零值 false（看起来"永远未配置"）或干脆 undefined，
// 于是契约变成假的。本条测试直接走 GetSettings handler，断言线上 JSON 里有该键，
// 且它的值与发码路径共用同一个判据（EmailService.IsConfigured）。
func TestAdminSettingsExposesSMTPConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name     string
		settings map[string]string
		want     bool
	}{
		{
			name:     "smtp fully configured",
			settings: map[string]string{service.SettingKeySMTPHost: "smtp.example.com", service.SettingKeySMTPFrom: "noreply@example.com"},
			want:     true,
		},
		{
			name:     "smtp host missing",
			settings: map[string]string{service.SettingKeySMTPFrom: "noreply@example.com"},
			want:     false,
		},
		{
			name:     "smtp from missing",
			settings: map[string]string{service.SettingKeySMTPHost: "smtp.example.com"},
			want:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			repo := &settingHandlerRepoStub{values: tc.settings}
			settingSvc := service.NewSettingService(repo, cfg)
			emailSvc := service.NewEmailService(repo, nil)
			handler := &SettingHandler{settingService: settingSvc, emailService: emailSvc}

			router := gin.New()
			router.GET("/admin/settings", handler.GetSettings)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/settings", nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var body struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())

			raw, ok := body.Data["smtp_configured"]
			require.True(t, ok, "响应必须包含 smtp_configured 字段（前端已按此实现）")
			require.Equal(t, tc.want, raw, "smtp_configured 必须反映真实可发信状态")

			// 同一判据：设置页说不可用时，发码必须也拒绝（不允许两处口径不一致）。
			ok2, _ := emailSvc.IsConfigured(httptest.NewRequest(http.MethodGet, "/", nil).Context())
			require.Equal(t, tc.want, ok2)
		})
	}
}
