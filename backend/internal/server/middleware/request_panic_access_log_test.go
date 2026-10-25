package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// accessLogEvents 返回 "http request completed" 事件。
func accessLogEvents(sink *testLogSink) []string {
	var statuses []string
	for _, event := range sink.list() {
		if event != nil && event.Message == "http request completed" {
			statuses = append(statuses, fmt.Sprint(event.Fields["status_code"]))
		}
	}
	return statuses
}

// TestPanicAccessLogMatchesReturnedStatus 锁定生产顺序 Recovery → Logger：
// panic 请求只能有一条访问日志，且状态必须是 Recovery 写入的 500。
func TestPanicAccessLogMatchesReturnedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	router := gin.New()
	router.Use(Recovery(), Logger())
	router.GET("/panic", func(*gin.Context) { panic("review-only") })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, []string{"500"}, accessLogEvents(sink))
}

// TestNormalAccessLogWrittenOnce 锁定正常请求仍然只记录一次且状态正确。
func TestNormalAccessLogWrittenOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	router := gin.New()
	router.Use(Recovery(), Logger())
	router.GET("/ok", func(c *gin.Context) { c.Status(http.StatusCreated) })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ok", nil))

	require.Equal(t, http.StatusCreated, response.Code)
	require.Equal(t, []string{"201"}, accessLogEvents(sink))
}

// TestPanicWithoutLoggerStillResponds500 锁定 Logger 未挂载时 Recovery 不改变响应。
func TestPanicWithoutLoggerStillResponds500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Recovery())
	router.GET("/panic", func(*gin.Context) { panic("review-only") })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))

	require.Equal(t, http.StatusInternalServerError, response.Code)
}
