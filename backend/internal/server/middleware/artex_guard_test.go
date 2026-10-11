package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const artexHTTPWorkerTail = "**中间产物输出规约**：脚本、payload、抓到的响应体、临时数据等一切中间产物，**一律写到本次意图的专属工作目录 dir-variable**（已自动建好，直接用相对路径写在这里即可，无需再手动建目录）——**不要写 /tmp、不要用其它绝对路径**。"

func artexHTTPFixture(t *testing.T, protocol string, matched bool) []byte {
	t.Helper()
	schemas := map[string]string{
		"report_finding":   `{"type":"object","properties":{"vulnclass":{"type":"string"},"severity":{"type":"string"},"summary":{"type":"string"},"evidence":{"type":"string"},"intent_id":{"type":"integer"},"asset_ids":{"type":"array","items":{"type":"integer"}}},"required":["vulnclass","severity","summary"]}`,
		"record_fact":      `{"type":"object","properties":{"facts":{"type":"array","items":{"type":"object"}},"summary":{"type":"string"},"detail":{"type":"string"},"evidence":{"type":"string"},"confidence":{"type":"string"},"intent_id":{"type":"integer"},"asset_ids":{"type":"array","items":{"type":"integer"}}}}`,
		"get_worker_trace": `{"type":"object","properties":{"intent_id":{"type":"integer"},"q":{"type":"string"},"step_ids":{"type":"array","items":{"type":"integer"}},"limit":{"type":"integer"}},"required":["intent_id"]}`,
	}
	tools := []any{}
	for _, name := range []string{"report_finding", "record_fact", "get_worker_trace"} {
		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(schemas[name]), &schema))
		tool := map[string]any{"name": name, "parameters": schema}
		switch protocol {
		case "anthropic":
			delete(tool, "parameters")
			tool["input_schema"] = schema
		case "chat":
			tool = map[string]any{"type": "function", "function": tool}
		default:
			tool["type"] = "function"
		}
		tools = append(tools, tool)
	}
	text := "custom BODY\n\n" + artexHTTPWorkerTail + "\nextra constraints"
	if !matched {
		text = "authorized security analysis/CTF/ARTEX source quotation; normal assistant"
	}
	request := map[string]any{"model": "gpt-5.1", "stream": true, "tools": tools}
	switch protocol {
	case "anthropic":
		request["system"] = text
		request["messages"] = []any{map[string]any{"role": "user", "content": "hello"}}
	case "chat":
		request["messages"] = []any{map[string]any{"role": "system", "content": text}, map[string]any{"role": "user", "content": "hello"}}
	default:
		request["instructions"] = text
		request["input"] = []any{map[string]any{"role": "user", "content": "hello"}}
	}
	raw, err := json.Marshal(request)
	require.NoError(t, err)
	return append([]byte(" \n"), append(raw, []byte("\n ")...)...)
}

func TestArtexHTTPGateModesAndExactBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"anthropic", "chat", "responses"} {
		for _, mode := range []string{"", config.ArtexGuardOff, config.ArtexGuardMonitor, config.ArtexGuardEnforce} {
			for _, matched := range []bool{false, true} {
				t.Run(protocol+"/"+mode+map[bool]string{false: "/normal", true: "/matched"}[matched], func(t *testing.T) {
					path := map[string]string{"anthropic": "/v1/messages", "chat": "/v1/chat/completions", "responses": "/v1/responses"}[protocol]
					body := artexHTTPFixture(t, protocol, matched)
					calls := 0
					cfg := &config.Config{Gateway: config.GatewayConfig{ArtexGuard: config.ArtexGuardConfig{Mode: mode}}}
					router := gin.New()
					router.POST(path, RequestBodyLimit(int64(len(body))), ArtexGuard(cfg), func(c *gin.Context) {
						calls++
						actual, err := io.ReadAll(c.Request.Body)
						require.NoError(t, err)
						require.Equal(t, body, actual)
						require.Equal(t, "identity", c.GetHeader("Content-Encoding"))
						require.Equal(t, int64(len(body)), c.Request.ContentLength)
						c.Status(http.StatusNoContent)
					})
					req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Content-Encoding", "identity")
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, req)
					blocked := matched && mode != config.ArtexGuardOff && mode != config.ArtexGuardMonitor
					if blocked {
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.Zero(t, calls)
						require.Contains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
					} else {
						require.Equal(t, http.StatusNoContent, recorder.Code)
						require.Equal(t, 1, calls)
					}
				})
			}
		}
	}
}

type artexReadProbe struct {
	reads int
	err   error
}

func (p *artexReadProbe) Read([]byte) (int, error) {
	p.reads++
	if p.err != nil {
		return 0, p.err
	}
	return 0, io.EOF
}
func (*artexReadProbe) Close() error { return nil }

func TestArtexHTTPGateOffAndExcludedRoutesNeverRead(t *testing.T) {
	for _, tc := range []struct{ method, path, mode string }{
		{"POST", "/v1/messages", config.ArtexGuardOff}, {"POST", "/v1/messages/count_tokens", "enforce"}, {"GET", "/v1/responses", "enforce"},
		{"POST", "/v1/images/generations", "enforce"}, {"POST", "/v1/embeddings", "enforce"}, {"POST", "/v1beta/models/*modelAction", "enforce"},
	} {
		t.Run(tc.method+tc.path+tc.mode, func(t *testing.T) {
			router := gin.New()
			router.Handle(tc.method, tc.path, ArtexGuard(&config.Config{Gateway: config.GatewayConfig{ArtexGuard: config.ArtexGuardConfig{Mode: tc.mode}}}), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			path := tc.path
			if tc.path == "/v1beta/models/*modelAction" {
				path = "/v1beta/models/x:generateContent"
			}
			probe := &artexReadProbe{err: errors.New("body should remain unread")}
			req := httptest.NewRequest(tc.method, path, nil)
			req.Body = probe
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.Zero(t, probe.reads)
		})
	}
}

func TestArtexHTTPGatePreservesChunkedAndPrereadMetadata(t *testing.T) {
	for _, preread := range []bool{false, true} {
		body := artexHTTPFixture(t, "responses", false)
		router := gin.New()
		router.POST("/v1/responses", ArtexGuard(nil), func(c *gin.Context) {
			actual, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, body, actual)
			require.Equal(t, int64(-1), c.Request.ContentLength)
			_, hasLength := c.Request.Header["Content-Length"]
			require.False(t, hasLength)
			require.Equal(t, []string{"chunked"}, c.Request.TransferEncoding)
			require.Equal(t, "identity", c.GetHeader("Content-Encoding"))
			require.Nil(t, c.Request.GetBody)
			c.Status(http.StatusNoContent)
		})
		req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
		req.ContentLength = -1
		req.Header.Del("Content-Length")
		req.Header.Set("Content-Encoding", "identity")
		req.TransferEncoding = []string{"chunked"}
		req.GetBody = nil
		if preread {
			req.Body = httputil.NewPrereadBody(body)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}
}

func TestArtexHTTPGatePreservesLimitAndReadErrors(t *testing.T) {
	for _, readErr := range []error{errors.New("read failure"), context.Canceled} {
		router := gin.New()
		router.POST("/v1/messages", ArtexGuard(nil), func(c *gin.Context) { t.Fatal("read failure must not reach forward") })
		req := httptest.NewRequest("POST", "/v1/messages", nil)
		req.Body = &artexReadProbe{err: readErr}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
	body := artexHTTPFixture(t, "anthropic", true)
	router := gin.New()
	router.POST("/v1/messages", RequestBodyLimit(int64(len(body)-1)), ArtexGuard(nil), func(c *gin.Context) { t.Fatal("oversized body forwarded") })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body)))
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
