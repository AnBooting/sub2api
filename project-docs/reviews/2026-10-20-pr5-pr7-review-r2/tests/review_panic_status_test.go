package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestReviewPanicAccessLogMatchesReturnedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	router := gin.New()
	// Same registration order as cmd/server and server/middleware/logger.go.
	router.Use(Recovery(), Logger())
	router.GET("/review-panic", func(c *gin.Context) { panic("review-only") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/review-panic", nil))
	require.Equal(t, http.StatusInternalServerError, response.Code)
	accessEvents := 0
	for _, event := range sink.list() {
		if event != nil && event.Message == "http request completed" {
			accessEvents++
			require.Equal(t, fmt.Sprint(response.Code), fmt.Sprint(event.Fields["status_code"]),
				"panic access log must reflect the returned HTTP status")
		}
	}
	require.Equal(t, 1, accessEvents, "panic must emit exactly one access log")
}
