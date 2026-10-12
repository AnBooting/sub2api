package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIBudgetExhaustedFailoverExit_Returns503NoAvailableGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))

	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), false)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	errBody, ok := envelope["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "api_error", errBody["type"])
	// Chat Completions 错误体不含 code 字段，只验证状态与 message。
	require.NotNil(t, errBody["message"])
	_, hasOpsError := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, hasOpsError, "预算耗尽不得记录 Ops 上游错误（上游从未被调用）")
}

func TestAnthropicBudgetExhaustedFailoverExit_Returns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(nil))

	(&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), false)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	errBody, ok := envelope["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "api_error", errBody["type"])
	require.Equal(t, "no_available_group", errBody["code"])
	_, hasOpsError := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, hasOpsError, "预算耗尽不得记录 Ops 上游错误（上游从未被调用）")
}

func TestGatewayHandlerBudgetExhaustedFailoverExit_Returns503NoAvailableGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(nil))

	(&GatewayHandler{}).handleFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), service.PlatformAnthropic, false)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	errBody, ok := envelope["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "api_error", errBody["type"])
	require.Equal(t, "no_available_group", errBody["code"])
	_, hasOpsError := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, hasOpsError, "预算耗尽不得记录 Ops 上游错误（上游从未被调用）")
}

func TestResponsesBudgetExhaustedFailoverExit_Returns503NoAvailableGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))

	(&GatewayHandler{}).handleResponsesFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), false)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	errBody, ok := envelope["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "no_available_group", errBody["code"])
	_, hasOpsError := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, hasOpsError, "预算耗尽不得记录 Ops 上游错误（上游从未被调用）")
}

func TestChatCompletionsBudgetExhaustedFailoverExit_Returns503NoAvailableGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))

	(&GatewayHandler{}).handleCCFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), false)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	errBody, ok := envelope["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "api_error", errBody["type"])
	require.Equal(t, "no_available_group", errBody["code"])
	_, hasOpsError := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, hasOpsError, "预算耗尽不得记录 Ops 上游错误（上游从未被调用）")
}

func TestGeminiBudgetExhaustedFailoverExit_Returns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.0-flash:generateContent", bytes.NewReader(nil))

	(&GatewayHandler{}).handleGeminiFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError())

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "request attempt budget exhausted")
	_, hasOpsError := c.Get(service.OpsUpstreamErrorsKey)
	require.False(t, hasOpsError, "预算耗尽不得记录 Ops 上游错误（上游从未被调用）")
}
