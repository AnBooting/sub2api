//go:build unit

package admin

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReviewPR18SettingsSavePreservesSMTPConfigured(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeySMTPHost: "smtp.example.com", service.SettingKeySMTPFrom: "noreply@example.com"})
	h.emailService = service.NewEmailService(repo, nil)
	router := gin.New()
	router.GET("/settings", h.GetSettings)
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/settings", nil))
	put := doUpdateSettings(t, h, map[string]any{"site_name": "Review SMTP"}, nil)
	var before, after struct {
		Data struct {
			Configured bool `json:"smtp_configured"`
		} `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(put.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	t.Logf("GET status=%d smtp_configured=%t; PUT status=%d smtp_configured=%t; SMTP host=%s from=%s", get.Code, before.Data.Configured, put.Code, after.Data.Configured, repo.values[service.SettingKeySMTPHost], repo.values[service.SettingKeySMTPFrom])
	if get.Code != 200 || put.Code != 200 || !before.Data.Configured {
		t.Fatal("invalid test setup")
	}
	if !after.Data.Configured {
		t.Error("saving unrelated settings incorrectly returns smtp_configured=false")
	}
}
