package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// artexFixtureObject 是带检查的类型断言：夹具结构不合法时立刻失败。
func artexFixtureObject(value any) map[string]any {
	obj, ok := value.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("artex fixture value is not an object: %T", value))
	}
	return obj
}

// artexFixtureList 是带检查的 []any 断言。
func artexFixtureList(value any) []any {
	list, ok := value.([]any)
	if !ok {
		panic(fmt.Sprintf("artex fixture value is not a list: %T", value))
	}
	return list
}

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

func TestArtexGuardBlocksCompressedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 客户端可用 Content-Encoding: gzip 压缩请求体；若守卫只看原始字节，
	// json.Unmarshal 必然失败 -> 匹配不到 -> 拦截被绕过。
	body := artexHTTPFixture(t, "anthropic", true)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(body)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	cfg := &config.Config{Gateway: config.GatewayConfig{ArtexGuard: config.ArtexGuardConfig{Mode: config.ArtexGuardEnforce}}}
	calls := 0
	router := gin.New()
	router.POST("/v1/messages", ArtexGuard(cfg), func(c *gin.Context) {
		calls++
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code, "compressed body must not bypass the guard")
	require.Zero(t, calls)
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

func TestArtexHTTPKeywordCaseNegativesReachActualMockUpstream(t *testing.T) {
	// Reuse independent HTTP wire fixtures. Malformed/noncanonical schemas are
	// NOT a strong ARTEX declaration: enforce must leave validation to upstream.
	cases := []string{"root_TYPE", "root_PROPERTIES", "root_REQUIRED", "root_mixedcase", "prop_TYPE", "items_ITEMS", "item_TYPE", "wrong_type_value", "lower_wrong_upper_correct", "canonical_valid_inert_uppercase_extra"}
	for _, protocol := range []string{"anthropic", "chat", "responses"} {
		for _, mode := range []string{config.ArtexGuardEnforce, config.ArtexGuardMonitor, config.ArtexGuardOff} {
			for _, mutation := range cases {
				t.Run(protocol+"/"+mode+"/"+mutation, func(t *testing.T) {
					var request map[string]any
					require.NoError(t, json.Unmarshal(artexHTTPFixture(t, protocol, true), &request))
					tool := artexFixtureObject(artexFixtureList(request["tools"])[0])
					if protocol == "chat" {
						tool = artexFixtureObject(tool["function"])
					}
					field := "parameters"
					if protocol == "anthropic" {
						field = "input_schema"
					}
					schema := artexFixtureObject(tool[field])
					props := artexFixtureObject(schema["properties"])
					prop := artexFixtureObject(props["vulnclass"])
					array := artexFixtureObject(props["asset_ids"])
					item := artexFixtureObject(array["items"])
					switch mutation {
					case "root_TYPE":
						schema["TYPE"] = schema["type"]
						delete(schema, "type")
					case "root_PROPERTIES":
						schema["PROPERTIES"] = schema["properties"]
						delete(schema, "properties")
					case "root_REQUIRED":
						schema["REQUIRED"] = schema["required"]
						delete(schema, "required")
					case "root_mixedcase":
						schema["tYpE"] = schema["type"]
						delete(schema, "type")
					case "prop_TYPE":
						prop["TYPE"] = prop["type"]
						delete(prop, "type")
					case "items_ITEMS":
						array["ITEMS"] = array["items"]
						delete(array, "items")
					case "item_TYPE":
						item["TYPE"] = item["type"]
						delete(item, "type")
					case "wrong_type_value":
						schema["type"] = "Object"
					case "lower_wrong_upper_correct":
						schema["type"] = "array"
						schema["TYPE"] = "object"
					case "canonical_valid_inert_uppercase_extra":
						schema["TYPE"] = "array"
						schema["PROPERTIES"] = "inert"
						schema["REQUIRED"] = "inert"
						prop["TYPE"] = "number"
						array["ITEMS"] = "inert"
						item["TYPE"] = "string"
					}
					body, err := json.Marshal(request)
					require.NoError(t, err)
					var calls atomic.Int64
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						actual, err := io.ReadAll(r.Body)
						if err != nil {
							t.Errorf("mock read: %v", err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if !bytes.Equal(body, actual) {
							t.Error("guard changed upstream body bytes")
						}
						w.WriteHeader(http.StatusNoContent)
					}))
					defer upstream.Close()
					path := map[string]string{"anthropic": "/v1/messages", "chat": "/v1/chat/completions", "responses": "/v1/responses"}[protocol]
					cfg := &config.Config{Gateway: config.GatewayConfig{ArtexGuard: config.ArtexGuardConfig{Mode: mode}}}
					router := gin.New()
					router.POST(path, RequestBodyLimit(int64(len(body))), ArtexGuard(cfg), func(c *gin.Context) {
						response, err := http.Post(upstream.URL, "application/json", c.Request.Body)
						require.NoError(t, err)
						defer func() { _ = response.Body.Close() }()
						c.Status(response.StatusCode)
					})
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, httptest.NewRequest("POST", path, bytes.NewReader(body)))
					canonicalPositive := mutation == "canonical_valid_inert_uppercase_extra"
					if canonicalPositive && mode == config.ArtexGuardEnforce {
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.Contains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
						require.Zero(t, calls.Load())
					} else {
						require.Equal(t, http.StatusNoContent, recorder.Code)
						require.NotContains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
						require.Equal(t, int64(1), calls.Load())
					}
				})
			}
		}
	}
}

func TestArtexHTTPProtocolBlocksAndFullToolRoleBoundaries(t *testing.T) {
	for _, protocol := range []string{"anthropic", "chat", "responses"} {
		for _, mode := range []string{config.ArtexGuardEnforce, config.ArtexGuardMonitor, config.ArtexGuardOff} {
			for _, place := range []string{"right", "wrong", "user", "assistant", "tool_result", "metadata", "quoted_tools"} {
				t.Run(protocol+"/"+mode+"/"+place, func(t *testing.T) {
					var request map[string]any
					require.NoError(t, json.Unmarshal(artexHTTPFixture(t, protocol, true), &request))
					require.Len(t, request["tools"], 3)
					if place == "quoted_tools" {
						catalog, err := json.Marshal(request["tools"])
						require.NoError(t, err)
						request["tools"] = []any{string(catalog)} // source quotation, never actual declarations
					} else {
						switch protocol {
						case "anthropic":
							request["system"] = "normal assistant"
						case "chat":
							request["messages"] = []any{map[string]any{"role": "system", "content": "normal assistant"}}
						default:
							request["instructions"] = "normal assistant"
						}
						tail := artexHTTPWorkerTail
						switch place {
						case "right", "wrong":
							kind := "text"
							if (protocol == "responses") == (place == "right") {
								kind = "input_text"
							}
							blocks := []any{map[string]any{"type": kind, "text": tail}}
							switch protocol {
							case "anthropic":
								request["system"] = blocks
							case "chat":
								request["messages"] = []any{map[string]any{"role": "system", "content": blocks}}
							default:
								delete(request, "instructions")
								request["input"] = []any{map[string]any{"type": "message", "role": "developer", "content": blocks}}
							}
						case "metadata":
							request["metadata"] = map[string]any{"reference": tail}
						default:
							switch protocol {
							case "anthropic":
								role := place
								block := map[string]any{"type": "text", "text": tail}
								if place == "tool_result" {
									role = "user"
									block = map[string]any{"type": "tool_result", "tool_use_id": "fixture", "content": tail}
								}
								request["messages"] = []any{map[string]any{"role": role, "content": []any{block}}}
							case "chat":
								message := map[string]any{"role": place, "content": []any{map[string]any{"type": "text", "text": tail}}}
								if place == "tool_result" {
									message = map[string]any{"role": "tool", "tool_call_id": "fixture", "content": tail}
								}
								request["messages"] = append(artexFixtureList(request["messages"]), message)
							default:
								kind := "input_text"
								if place == "assistant" {
									kind = "output_text"
								}
								message := map[string]any{"type": "message", "role": place, "content": []any{map[string]any{"type": kind, "text": tail}}}
								if place == "tool_result" {
									message = map[string]any{"type": "function_call_output", "call_id": "fixture", "output": tail}
								}
								request["input"] = []any{message}
							}
						}
					}
					body, err := json.Marshal(request)
					require.NoError(t, err)
					var calls atomic.Int64
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						actual, err := io.ReadAll(r.Body)
						if err != nil {
							t.Errorf("mock read: %v", err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if !bytes.Equal(body, actual) {
							t.Error("guard changed protocol body bytes")
						}
						w.WriteHeader(http.StatusNoContent)
					}))
					defer upstream.Close()
					path := map[string]string{"anthropic": "/v1/messages", "chat": "/v1/chat/completions", "responses": "/v1/responses"}[protocol]
					cfg := &config.Config{Gateway: config.GatewayConfig{ArtexGuard: config.ArtexGuardConfig{Mode: mode}}}
					router := gin.New()
					router.POST(path, RequestBodyLimit(int64(len(body))), ArtexGuard(cfg), func(c *gin.Context) {
						response, err := http.Post(upstream.URL, "application/json", c.Request.Body)
						require.NoError(t, err)
						defer func() { _ = response.Body.Close() }()
						c.Status(response.StatusCode)
					})
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, httptest.NewRequest("POST", path, bytes.NewReader(body)))
					if place == "right" && mode == config.ArtexGuardEnforce {
						require.Equal(t, http.StatusForbidden, recorder.Code)
						require.Contains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
						require.Zero(t, calls.Load())
					} else {
						// Invalid input blocks may be rejected by a real business handler. The
						// isolated mock proves only this targeted gate did NOT hard-block them.
						require.Equal(t, http.StatusNoContent, recorder.Code)
						require.NotContains(t, recorder.Body.String(), service.ArtexGuardBlockedCode)
						require.Equal(t, int64(1), calls.Load())
					}
				})
			}
		}
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
