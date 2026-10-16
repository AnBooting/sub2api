package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestArtexWSConfirmedContextNeverCommitsLocalOrRejectedUpdates(t *testing.T) {
	request := artexFixtureRequest(t, ArtexResponses, "worker")
	tools := request["tools"]
	instructions := request["instructions"]
	guard := newArtexWSSessionGuard(config.ArtexGuardEnforce)
	frame := func(kind string, value map[string]any) []byte {
		if kind == "session.update" || kind == "session.updated" {
			return artexFixtureMarshal(t, map[string]any{"type": kind, "session": value})
		}
		value["type"] = kind
		return artexFixtureMarshal(t, value)
	}
	require.NoError(t, guard.CheckBeforeSend(frame("session.update", map[string]any{"instructions": instructions})))
	guard.ObserveUpstream(artexFixtureMarshal(t, map[string]any{"type": "error", "error": map[string]any{"message": "rejected update", "session": map[string]any{"instructions": instructions}}}))
	require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{"tools": tools})), "rejected/pending update is not effective context")
	// The provider really confirms instructions but no tool declarations.
	guard.ObserveUpstream(frame("session.updated", map[string]any{"instructions": instructions, "tools": []any{}}))
	require.Error(t, guard.CheckBeforeSend(frame("response.create", map[string]any{"tools": tools})), "actual accepted context plus per-frame tools matches")
	require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{"instructions": "normal assistant", "tools": tools})))
	require.Error(t, guard.CheckBeforeSend(frame("response.create", map[string]any{"tools": tools})), "normal one-turn override does not overwrite confirmed context")
	// Remote echo omitting an old field must never retain stale proof.
	guard.ObserveUpstream(frame("session.updated", map[string]any{"tools": tools}))
	require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{})), "missing instructions is unknown, not the old tail")
	guard.ObserveUpstream(frame("session.updated", map[string]any{"instructions": instructions}))
	require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{})), "missing tools is unknown, not the old family")
	guard.ObserveUpstream(frame("session.updated", map[string]any{"instructions": instructions, "tools": tools}))
	require.Error(t, guard.CheckBeforeSend(frame("response.create", map[string]any{})))
	for _, clear := range []map[string]any{{"instructions": nil}, {"instructions": ""}, {"tools": nil}, {"tools": []any{}}} {
		guard.ObserveUpstream(frame("session.updated", map[string]any{"instructions": instructions, "tools": tools}))
		require.NoError(t, guard.CheckBeforeSend(frame("response.create", clear)), "explicit null/empty overrides do not inherit old values")
		guard.ObserveUpstream(frame("session.updated", clear))
		require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{})), "confirmed clears remove old matching context")
	}
	// A response itself never persists. Unknown split history remains unknown.
	guard = newArtexWSSessionGuard(config.ArtexGuardEnforce)
	require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{"instructions": instructions})))
	require.NoError(t, guard.CheckBeforeSend(frame("response.create", map[string]any{"tools": tools})))
	require.NoError(t, guard.CheckBeforeSend(frame("conversation.item.create", map[string]any{"instructions": instructions, "tools": tools})))
}

func TestArtexWSModeAndPolicyError(t *testing.T) {
	body := artexFixtureMarshal(t, artexFixtureRequest(t, ArtexResponses, "worker"))
	for _, mode := range []string{config.ArtexGuardOff, config.ArtexGuardMonitor, config.ArtexGuardEnforce} {
		guard := newArtexWSSessionGuard(mode)
		err := guard.CheckBeforeSend(body)
		cfg := &config.Config{Gateway: config.GatewayConfig{ArtexGuard: config.ArtexGuardConfig{Mode: mode}}}
		directErr := CheckArtexWSRequest(cfg, body)
		if mode == config.ArtexGuardEnforce {
			var closeErr *OpenAIWSClientCloseError
			require.ErrorAs(t, err, &closeErr)
			require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
			require.Contains(t, err.Error(), ArtexGuardBlockedCode)
			require.Error(t, directErr)
		} else {
			require.NoError(t, err)
			require.NoError(t, directErr)
		}
	}
}

// Both native pool WriteJSON and passthrough WriteFrame are captured by this
// mock upstream. It emits a completed event only AFTER an actual request write.
type artexGuardRecordingWSConn struct {
	*stagedPassthroughConn
	count atomic.Int64
}

func (c *artexGuardRecordingWSConn) WriteJSON(ctx context.Context, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, body)
}
func (c *artexGuardRecordingWSConn) WriteFrame(ctx context.Context, kind coderws.MessageType, body []byte) error {
	if err := c.stagedPassthroughConn.WriteFrame(ctx, kind, body); err != nil {
		return err
	}
	c.count.Add(1)
	var request map[string]any
	if json.Unmarshal(body, &request) == nil && request["type"] == "response.create" {
		c.Send(`{"type":"response.completed","response":{"id":"resp_artex_normal","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	}
	return nil
}

// Mirror the real handler's typed-close contract. coderws.Close is a protocol
// handshake: the test client must read/ack the close frame BEFORE waiting for
// service/handler exit. Not consuming it makes the unchanged engine appear hung.
func startArtexGuardRecordingServer(t *testing.T, ctx context.Context, svc *OpenAIGatewayService, account *Account, hooks *OpenAIWSIngressHooks) (*httptest.Server, <-chan error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()
		kind, first, err := ReadOpenAIWSClientMessage(ctx, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first model frame")
		if err != nil {
			serverErr <- err
			return
		}
		if kind != coderws.MessageText {
			serverErr <- errors.New("fixture first model frame is not text")
			return
		}
		ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ginCtx.Request = r.Clone(ctx)
		fixtureHooks := hooks
		if fixtureHooks == nil {
			fixtureHooks = &OpenAIWSIngressHooks{}
		}
		copyHooks := *fixtureHooks
		copyHooks.ClientLifecycleContext = ctx
		err = svc.ProxyResponsesWebSocketFromClient(ctx, ginCtx, conn, account, "sk-test", first, &copyHooks)
		var closeErr *OpenAIWSClientCloseError
		if errors.As(err, &closeErr) {
			_ = conn.Close(closeErr.StatusCode(), closeErr.Reason())
		}
		serverErr <- err
	}))
	return server, serverErr
}
func requireArtexWSClientClose(t *testing.T, conn *coderws.Conn, status coderws.StatusCode, reason string) {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	for {
		_, _, err := conn.Read(ctx)
		if err == nil {
			continue
		}
		var closeErr coderws.CloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, status, closeErr.Code)
		if reason != "" {
			require.Equal(t, reason, closeErr.Reason)
		}
		return
	}
}

func TestArtexWSPassthroughObservesOnlyActualRemoteAck(t *testing.T) {
	for _, mode := range []string{config.ArtexGuardEnforce, config.ArtexGuardMonitor, config.ArtexGuardOff} {
		for _, outcome := range []string{"accepted", "rejected", "pending", "cleared"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.ArtexGuard.Mode = mode
				upstream := newStagedPassthroughConn()
				svc := newPassthroughLifecycleService(cfg, upstream)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				server, serverErr := startArtexGuardRecordingServer(t, ctx, svc, passthroughLifecycleAccount(), nil)
				defer server.Close()
				conn := dialPassthroughLifecycleClient(t, server)
				defer conn.CloseNow()
				requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_ack_seed","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
				_, err := readPassthroughLifecycleFrame(t, conn, 3*time.Second)
				require.NoError(t, err)
				request := artexFixtureRequest(t, ArtexResponses, "worker")
				write := func(value any) {
					writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
					defer stop()
					require.NoError(t, conn.Write(writeCtx, coderws.MessageText, artexFixtureMarshal(t, value)))
				}
				write(map[string]any{"type": "session.update", "session": map[string]any{"instructions": request["instructions"]}})
				requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
				if outcome == "rejected" {
					upstream.Send(`{"type":"error","error":{"code":"invalid_session_update","message":"update rejected"}}`)
					_, err = readPassthroughLifecycleFrame(t, conn, 3*time.Second)
					require.NoError(t, err)
				}
				if outcome == "accepted" || outcome == "cleared" {
					upstream.Send(string(artexFixtureMarshal(t, map[string]any{"type": "session.updated", "session": map[string]any{"instructions": request["instructions"], "tools": []any{}}})))
					_, err = readPassthroughLifecycleFrame(t, conn, 3*time.Second)
					require.NoError(t, err)
				}
				if outcome == "cleared" {
					write(map[string]any{"type": "session.update", "session": map[string]any{"instructions": nil, "tools": []any{}}})
					requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
					upstream.Send(string(artexFixtureMarshal(t, map[string]any{"type": "session.updated", "session": map[string]any{"instructions": nil, "tools": []any{}}})))
					_, err = readPassthroughLifecycleFrame(t, conn, 3*time.Second)
					require.NoError(t, err)
				}
				// A tools-only model frame must compose ONLY the real accepted remote tail.
				write(map[string]any{"type": "response.create", "model": "gpt-5.1", "tools": request["tools"], "input": []any{map[string]any{"role": "user", "content": "normal prompt"}}})
				if outcome == "accepted" && mode == config.ArtexGuardEnforce {
					requireArtexWSClientClose(t, conn, coderws.StatusPolicyViolation, ArtexGuardBlockedCode)
					select {
					case err := <-serverErr:
						var closeErr *OpenAIWSClientCloseError
						require.ErrorAs(t, err, &closeErr)
						require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
					case <-time.After(3 * time.Second):
						t.Fatal("confirmed composition not rejected")
					}
					select {
					case <-upstream.writes:
						t.Fatal("confirmed strong model frame reached upstream")
					default:
					}
				} else {
					requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
					cancel()
					requireArtexWSClientClose(t, conn, coderws.StatusGoingAway, "")
					select {
					case <-serverErr:
					case <-time.After(3 * time.Second):
						t.Fatal("proxy did not settle")
					}
				}
			})
		}
	}
}

func TestArtexWSActualModesRejectBeforeFirstAndLaterWrite(t *testing.T) {
	for _, ingress := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		for _, guardMode := range []string{config.ArtexGuardEnforce, config.ArtexGuardMonitor, config.ArtexGuardOff} {
			for _, later := range []bool{false, true} {
				t.Run(ingress+"/"+guardMode+map[bool]string{false: "/first", true: "/later"}[later], func(t *testing.T) {
					cfg := passthroughLifecycleConfig()
					cfg.Gateway.ArtexGuard.Mode = guardMode
					cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
					cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
					cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
					account := passthroughLifecycleAccount()
					account.Extra["openai_apikey_responses_websockets_v2_mode"] = ingress
					upstream := &artexGuardRecordingWSConn{stagedPassthroughConn: newStagedPassthroughConn()}
					bridge := &httpUpstreamRecorder{}
					for i := 0; i < 2; i++ {
						bridge.responses = append(bridge.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_bridge\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\ndata: [DONE]\n\n"))})
					}
					svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: bridge, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPassthroughDialer: &stagedPassthroughDialer{conn: upstream}}
					pool := newOpenAIWSConnPool(cfg)
					pool.setClientDialerForTest(&stagedPassthroughDialer{conn: upstream})
					svc.openaiWSPool = pool
					defer pool.Close()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					terminal := make(chan struct{}, 2)
					hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
						if err == nil && result != nil {
							terminal <- struct{}{}
						}
					}}
					server, serverErr := startArtexGuardRecordingServer(t, ctx, svc, account, hooks)
					defer server.Close()
					conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
					require.NoError(t, err)
					defer conn.CloseNow()
					send := func(body []byte) {
						writeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
						defer stop()
						require.NoError(t, conn.Write(writeCtx, coderws.MessageText, body))
					}
					awaitCompletion := func() {
						readCtx, stop := context.WithTimeout(ctx, 3*time.Second)
						defer stop()
						for {
							_, raw, err := conn.Read(readCtx)
							require.NoError(t, err)
							if strings.Contains(string(raw), "response.completed") || strings.Contains(string(raw), "response.done") {
								break
							}
						}
						select {
						case <-terminal:
						case <-time.After(3 * time.Second):
							t.Fatal("successful turn completion not recorded")
						}
					}
					matched := artexFixtureRequest(t, ArtexResponses, "worker")
					matched["type"] = "response.create"
					if later {
						send([]byte(`{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"normal"}]}`))
						awaitCompletion()
					}
					send(artexFixtureMarshal(t, matched))
					expected := int64(1)
					if later {
						expected++
					}
					if guardMode == config.ArtexGuardEnforce {
						requireArtexWSClientClose(t, conn, coderws.StatusPolicyViolation, ArtexGuardBlockedCode)
						select {
						case err := <-serverErr:
							var closeErr *OpenAIWSClientCloseError
							require.ErrorAs(t, err, &closeErr)
							require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
						case <-time.After(3 * time.Second):
							t.Fatal("guard did not terminate before upstream write")
						}
						expected--
					} else {
						awaitCompletion()
						cancel()
						requireArtexWSClientClose(t, conn, coderws.StatusGoingAway, "")
						select {
						case <-serverErr:
						case <-time.After(3 * time.Second):
							t.Fatal("normal cancellation did not settle proxy")
						}
					}
					if ingress == OpenAIWSIngressModeHTTPBridge {
						require.Equal(t, int(expected), len(bridge.requests))
						require.Zero(t, upstream.count.Load())
					} else {
						require.Equal(t, expected, upstream.count.Load())
						require.Empty(t, bridge.requests)
					}
				})
			}
		}
	}
}
