package routes

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
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

// The actual private registration retains every original route/auth/permission/
// limit/subpath gate. Only terminal business forwarding is a fixture that sends
// actual HTTP to a local mock upstream; this does not simulate DB billing.
func TestArtexRegisteredAliasesMockUpstreamCounters(t *testing.T) {
	aliases := []struct{ path, pattern, protocol string }{
		{"/v1/messages", "/v1/messages", "anthropic"}, {"/antigravity/v1/messages", "/antigravity/v1/messages", "anthropic"},
		{"/v1/chat/completions", "/v1/chat/completions", "chat"}, {"/chat/completions", "/chat/completions", "chat"},
		{"/v1/responses", "/v1/responses", "responses"}, {"/v1/responses/compact", "/v1/responses/*subpath", "responses"},
		{"/responses", "/responses", "responses"}, {"/responses/compact", "/responses/*subpath", "responses"},
		{"/backend-api/codex/responses", "/backend-api/codex/responses", "responses"}, {"/backend-api/codex/responses/compact", "/backend-api/codex/responses/*subpath", "responses"},
	}
	for _, alias := range aliases {
		for _, mode := range []string{config.ArtexGuardEnforce, config.ArtexGuardMonitor, config.ArtexGuardOff} {
			for _, matched := range []bool{false, true} {
				t.Run(alias.path+"/"+mode+map[bool]string{false: "/normal", true: "/matched"}[matched], func(t *testing.T) {
					body := artexHTTPFixture(t, alias.protocol, matched)
					var upstreamCalls, usageFixture atomic.Int64
					observed := make(chan []byte, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						upstreamCalls.Add(1)
						raw, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						observed <- raw
						w.WriteHeader(http.StatusNoContent)
					}))
					defer upstream.Close()
					terminal := func(c *gin.Context) {
						raw, err := io.ReadAll(c.Request.Body)
						require.NoError(t, err)
						require.Equal(t, body, raw)
						require.Equal(t, "identity", c.GetHeader("Content-Encoding"))
						require.Equal(t, int64(len(body)), c.Request.ContentLength)
						response, err := http.Post(upstream.URL, "application/json", bytes.NewReader(raw))
						require.NoError(t, err)
						defer response.Body.Close()
						usageFixture.Add(1)
						c.Status(response.StatusCode)
					}
					router := gin.New()
					auth := servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
						if c.GetHeader("Authorization") != "Bearer fixture-key" {
							c.AbortWithStatus(http.StatusUnauthorized)
							return
						}
						id := int64(1)
						c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}})
						c.Next()
					})
					cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20, ArtexGuard: config.ArtexGuardConfig{Mode: mode}}}
					registerGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, auth, nil, nil, nil, nil, nil, cfg, map[string]gin.HandlerFunc{alias.pattern: terminal})
					makeReq := func(authorized bool) *http.Request {
						req := httptest.NewRequest("POST", alias.path, bytes.NewReader(body))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Content-Encoding", "identity")
						if authorized {
							req.Header.Set("Authorization", "Bearer fixture-key")
						}
						return req
					}
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, makeReq(false))
					require.Equal(t, http.StatusUnauthorized, recorder.Code)
					require.Zero(t, upstreamCalls.Load())
					recorder = httptest.NewRecorder()
					router.ServeHTTP(recorder, makeReq(true))
					if matched && mode == config.ArtexGuardEnforce {
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.Contains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
						require.Zero(t, upstreamCalls.Load())
						require.Zero(t, usageFixture.Load())
					} else {
						require.Equal(t, http.StatusNoContent, recorder.Code)
						require.Equal(t, int64(1), upstreamCalls.Load())
						require.Equal(t, int64(1), usageFixture.Load())
						require.Equal(t, body, <-observed)
					}
				})
			}
		}
	}
}

// Safe suffixes have always been syntactically forwardable, not a closed known
// endpoint list. Compare off/enforce through the SAME real registration and
// actual local mock upstream, rather than inventing a new 404 for /unknown.
func TestArtexRegisteredSafeUnknownSuffixPreservesOffBehavior(t *testing.T) {
	for _, alias := range []struct{ path, pattern string }{
		{"/v1/responses/unknown", "/v1/responses/*subpath"},
		{"/responses/unknown", "/responses/*subpath"},
		{"/backend-api/codex/responses/unknown", "/backend-api/codex/responses/*subpath"},
	} {
		t.Run(alias.path, func(t *testing.T) {
			var statuses []int
			for _, mode := range []string{config.ArtexGuardOff, config.ArtexGuardEnforce} {
				var calls atomic.Int64
				body := artexHTTPFixture(t, "responses", true)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					actual, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, body, actual)
					w.WriteHeader(http.StatusNoContent)
				}))
				terminal := func(c *gin.Context) {
					response, err := http.Post(upstream.URL, "application/json", c.Request.Body)
					require.NoError(t, err)
					defer response.Body.Close()
					c.Status(response.StatusCode)
				}
				auth := servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
					id := int64(1)
					c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}})
					c.Next()
				})
				cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, TextMaxBodySize: 1 << 20, ArtexGuard: config.ArtexGuardConfig{Mode: mode}}}
				router := gin.New()
				registerGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{}, AsyncImage: handler.NewAsyncImageHandler(nil, nil)}, auth, nil, nil, nil, nil, nil, cfg, map[string]gin.HandlerFunc{alias.pattern: terminal})
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest("POST", alias.path, bytes.NewReader(body)))
				upstream.Close()
				statuses = append(statuses, recorder.Code)
				require.Equal(t, http.StatusNoContent, recorder.Code)
				require.NotContains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
				require.Equal(t, int64(1), calls.Load())
			}
			require.Equal(t, statuses[0], statuses[1], "new target gate must not change safe suffix behavior")
		})
	}
}

func TestArtexRegisteredUnsupportedResponsesSubpathsStay404(t *testing.T) {
	router := newGatewayRoutesTestRouter()
	for _, path := range []string{"/v1/responses/bad!", "/responses/%2e%2e/x", "/backend-api/codex/responses/%3fa=b"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("POST", path, strings.NewReader(string(artexHTTPFixture(t, "responses", true)))))
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.NotContains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
	}
}
