package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// I-02：/setup/status 必须报告真实初始化状态，不得硬编码 needs_setup=false。
// 该端点是匿名的，只允许暴露布尔与阶段枚举。
func TestSetupStatusReportsRealInstallState(t *testing.T) {
	tests := []struct {
		name       string
		skipSetup  string
		markerFile string
		wantSetup  bool
		wantStep   string
	}{
		{name: "fresh data dir needs setup", skipSetup: "false", wantSetup: true, wantStep: "welcome"},
		{name: "config file exists installed", skipSetup: "false", markerFile: setup.ConfigFileName, wantSetup: false, wantStep: "completed"},
		{name: "install lock exists installed", skipSetup: "false", markerFile: setup.InstallLockFile, wantSetup: false, wantStep: "completed"},
		{name: "skip setup bypasses detection", skipSetup: "true", wantSetup: false, wantStep: "completed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			t.Setenv("DATA_DIR", dataDir)
			t.Setenv("SKIP_SETUP", tc.skipSetup)
			if tc.markerFile != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dataDir, tc.markerFile), nil, 0o600))
			}

			gin.SetMode(gin.TestMode)
			router := gin.New()
			RegisterCommonRoutes(router)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var payload struct {
				Code int               `json:"code"`
				Data setup.SetupStatus `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload), rec.Body.String())
			require.Equal(t, 0, payload.Code)
			require.Equal(t, tc.wantSetup, payload.Data.NeedsSetup, rec.Body.String())
			require.Equal(t, tc.wantStep, payload.Data.Step, rec.Body.String())
		})
	}
}
