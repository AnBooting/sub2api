//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// N-03：空 messages / 空 input 必须在网关入口被 400 拒绝，不得转发上游、不得计费。
// 六个入口逐一断言：400 + 上游零调用 + 账本零命令（与缺价红线同一套断言风格）。

func TestGatewayEmptyMessagesRejectedBeforeUpstream(t *testing.T) {
	bodies := []string{
		// 缺字段 / 空数组 / 非数组（字符串）三种形态。
		`{"model":"client-model","stream":false}`,
		`{"model":"client-model","messages":[],"stream":false}`,
		`{"model":"client-model","messages":"hello","stream":false}`,
	}
	t.Run("chat_completions_anthropic_branch", func(t *testing.T) {
		for _, body := range bodies {
			f := newSharedLockedFixture(t, true, "")
			ccBody := strings.Replace(body, `"stream":false`, `"max_completion_tokens":256,"stream":false`, 1)
			rec := f.serveChat(t, ccBody)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "messages is required")
			require.Empty(t, f.upstream.callList())
			require.Empty(t, f.ledger.commands)
		}
	})
	t.Run("messages_anthropic_entry", func(t *testing.T) {
		for _, body := range bodies {
			f := newSharedLockedFixture(t, true, "")
			msgBody := strings.Replace(body, `"stream":false`, `"max_tokens":256,"stream":false`, 1)
			rec := f.serve(t, "/v1/messages", msgBody, func(h *GatewayHandler, c *gin.Context) { h.Messages(c) })
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "messages is required")
			require.Empty(t, f.upstream.callList())
			require.Empty(t, f.ledger.commands)
		}
	})
	t.Run("chat_completions_openai_native_branch", func(t *testing.T) {
		for _, body := range bodies {
			f := newOpenAICCLockedFixture(t, true, "")
			ccBody := strings.Replace(body, `"stream":false`, `"max_completion_tokens":256,"stream":false`, 1)
			rec := serveOpenAICCLockedBody(t, f, ccBody)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "messages is required")
			require.Empty(t, f.upstream.callList())
			require.Empty(t, f.ledger.commands)
		}
	})
	t.Run("messages_openai_dispatch_entry", func(t *testing.T) {
		for _, body := range bodies {
			f := newLockedHTTPFixture(t, false)
			f.key.Group.AllowMessagesDispatch = true
			msgBody := strings.Replace(body, `"stream":false`, `"max_tokens":256,"stream":false`, 1)
			rec := serveOpenAIMessagesBody(t, f, msgBody)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "messages is required")
			require.Empty(t, f.upstream.calls)
			require.Empty(t, f.ledger.commands)
		}
	})
}

func TestGatewayEmptyResponsesInputRejectedBeforeUpstream(t *testing.T) {
	bodies := []string{
		`{"model":"client-model","stream":false}`,
		`{"model":"client-model","input":"","stream":false}`,
		`{"model":"client-model","input":"   ","stream":false}`,
		`{"model":"client-model","input":[],"stream":false}`,
	}
	t.Run("responses_anthropic_branch", func(t *testing.T) {
		for _, body := range bodies {
			f := newSharedLockedFixture(t, true, "")
			respBody := strings.Replace(body, `"stream":false`, `"max_output_tokens":256,"stream":false`, 1)
			rec := f.serveResponses(t, respBody)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "input is required")
			require.Empty(t, f.upstream.callList())
			require.Empty(t, f.ledger.commands)
		}
	})
	t.Run("responses_openai_native_branch", func(t *testing.T) {
		for _, body := range bodies {
			f := newLockedHTTPFixture(t, false)
			rec, _ := f.requestBody(t, body, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), "input is required")
			require.Empty(t, f.upstream.calls)
			require.Empty(t, f.ledger.commands)
		}
	})
}

// serveOpenAICCLockedBody 用任意请求体驱动 OpenAI 原生 Chat Completions 入口
// （夹具自带的 serve 只接受常量请求体）。
func serveOpenAICCLockedBody(t *testing.T, f *openaiCCLockedFixture, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	f.handler.ChatCompletions(c)
	return recorder
}

// serveOpenAIMessagesBody 用任意请求体驱动 OpenAI 分支的 /v1/messages 调度入口。
func serveOpenAIMessagesBody(t *testing.T, f *lockedHTTPFixture, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.WithValue(context.Background(), ctxkey.Group, f.group)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/messages", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), f.key)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 10})
	f.handler.Messages(c)
	return recorder
}
