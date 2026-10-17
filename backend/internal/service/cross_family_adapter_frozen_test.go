//go:build unit

package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func crossFrozenGeminiChat(t *testing.T, ctx context.Context, account *Account, native, stream bool) (*ForwardResult, error, *geminiCompatHTTPUpstreamStub) {
	payload := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}`
	ct := "application/json"
	if account.Type == AccountTypeOAuth {
		payload = `{"response":` + payload + "}"
	}
	if stream || account.Type == AccountTypeOAuth {
		payload = "data: " + payload + "\n\n"
		ct = "text/event-stream"
	}
	svc := newGeminiSignalService(ct, payload)
	svc.tokenProvider = &GeminiTokenProvider{tokenCache: &fakeGeminiTokenCache{token: "fixture"}}
	c, _ := newGeminiNativeTestContext(t)
	c.Request = c.Request.WithContext(ctx)
	var result *ForwardResult
	var err error
	body := []byte(fmt.Sprintf(`{"model":"client-model","stream":%t,"max_completion_tokens":64,"messages":[{"role":"user","content":"hi"}]}`, stream))
	result, err = svc.ForwardAsChatCompletions(ctx, c, account, body)
	return result, err, svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
}
func TestCrossFrozenGeminiChatUsesSnapshotModel(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeServiceAccount, AccountTypeOAuth} {
		for _, native := range []bool{false} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/native=%t/stream=%t", kind, native, stream), func(t *testing.T) {
					f, ctx, bundle := geminiFrozenFixture(t, kind)
					expected, err := bundle.UpstreamModelFor(f.group.ID, f.account.ID)
					require.NoError(t, err)
					wantFrozen := "gemini-2.5-flash"
					if kind == AccountTypeOAuth {
						wantFrozen = "client-model"
					}
					require.Equal(t, wantFrozen, expected)
					ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
					require.NoError(t, err)
					f.account.Credentials["model_mapping"] = map[string]any{"client-model": "gemini-2.5-pro"}
					result, err, httpStub := crossFrozenGeminiChat(t, ctx, f.account, native, stream)
					require.NoError(t, err)
					require.Equal(t, 1, httpStub.calls)
					require.Equal(t, expected, result.UpstreamModel)
					if kind == AccountTypeOAuth {
						body, err := io.ReadAll(httpStub.lastReq.Body)
						require.NoError(t, err)
						require.Contains(t, string(body), expected)
						require.NotContains(t, string(body), "gemini-2.5-pro")
					} else {
						require.Contains(t, httpStub.lastReq.URL.Path, expected)
						require.NotContains(t, httpStub.lastReq.URL.Path, "gemini-2.5-pro")
					}
					require.NoError(t, f.svc.RecordUsage(ctx, f.input(result)))
					require.NotNil(t, f.ledger.lastCmd)
					require.Greater(t, f.ledger.lastCmd.BalanceCost, 0.0)
				})
			}
		}
	}
}
func TestCrossFrozenGeminiChatUnlockedCompatibility(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeServiceAccount, AccountTypeOAuth} {
		for _, native := range []bool{false} {
			t.Run(fmt.Sprint(kind, native), func(t *testing.T) {
				f, _, _ := geminiFrozenFixture(t, kind)
				result, err, httpStub := crossFrozenGeminiChat(t, context.Background(), f.account, native, false)
				require.NoError(t, err)
				require.Equal(t, 1, httpStub.calls)
				want := "gemini-2.5-flash"
				if kind == AccountTypeOAuth {
					want = "client-model"
				}
				require.Equal(t, want, result.UpstreamModel)
			})
		}
	}
}
func TestCrossFrozenGeminiChatRejectsUnboundOrMismatchedAccount(t *testing.T) {
	for _, mode := range []string{"unbound", "wrong-account", "missing-mapping"} {
		for _, native := range []bool{false} {
			t.Run(fmt.Sprint(mode, native), func(t *testing.T) {
				f, ctx, bundle := geminiFrozenFixture(t, AccountTypeAPIKey)
				if mode != "unbound" {
					var err error
					ctx, err = WithGatewayRequestBillingAccount(ctx, f.group.ID, f.account.ID)
					require.NoError(t, err)
				}
				if mode == "wrong-account" {
					f.account.ID++
				}
				if mode == "missing-mapping" {
					delete(bundle.candidates[f.group.ID].accountUpstreamModels, f.account.ID)
				}
				result, err, httpStub := crossFrozenGeminiChat(t, ctx, f.account, native, false)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, result)
				require.Equal(t, 0, httpStub.calls)
			})
		}
	}
}
func TestCrossFrozenAnthropicAdapters(t *testing.T) {
	for _, chat := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"frozen", "unbound", "wrong", "unlocked"} {
				t.Run(fmt.Sprint(chat, stream, mode), func(t *testing.T) {
					f := newSharedLockedFixture(t, true, false)
					ctx := f.lockedContext(t, "claude-sonnet-4")
					if mode == "unbound" {
						ctx = withGatewayRequestBilling(context.Background(), GatewayRequestBillingFromContext(ctx))
					}
					if mode == "wrong" {
						f.account.ID++
					}
					if mode == "unlocked" {
						ctx = context.Background()
					}
					f.account.Credentials["model_mapping"] = map[string]any{"client-model": "claude-opus-4"}
					up := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(namespaceToolAnthropicStream()))}}
					svc := &GatewayService{cfg: &config.Config{}, httpUpstream: up}
					body := fmt.Sprintf(`{"model":"client-model","stream":%t,"input":"hi"}`, stream)
					if chat {
						body = fmt.Sprintf(`{"model":"client-model","stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)).WithContext(ctx)
					var result *ForwardResult
					var err error
					if chat {
						result, err = svc.ForwardAsChatCompletions(ctx, c, f.account, []byte(body), nil)
					} else {
						result, err = svc.ForwardAsResponses(ctx, c, f.account, []byte(body), nil)
					}
					if mode == "unbound" || mode == "wrong" {
						require.ErrorIs(t, err, ErrModelPricingUnavailable)
						require.Nil(t, up.lastReq)
						return
					}
					require.NoError(t, err)
					want := "claude-sonnet-4"
					if mode == "unlocked" {
						want = "claude-opus-4"
					}
					require.Equal(t, want, result.UpstreamModel)
					require.Equal(t, want, gjson.GetBytes(up.lastBody, "model").String())
					require.Equal(t, "/v1/messages", up.lastReq.URL.Path)
				})
			}
		}
	}
}
func TestCrossFrozenVertexSupported(t *testing.T) {
	for _, mode := range []string{"canonical", "no-map", "mapped"} {
		kind := AccountTypeServiceAccount
		for _, locked := range []bool{false, true} {
			for _, chat := range []bool{false, true} {
				t.Run(fmt.Sprint(mode, locked, chat), func(t *testing.T) {
					model := "claude-sonnet-4-5"
					f := newSharedLockedFixture(t, true, false)
					f.account.Type = kind
					f.account.Credentials = map[string]any{"access_token": "fixture", "project_id": "project-1", "region": "us-east-1", "aws_region": "us-east-1", "model_mapping": map[string]any{model: model}, "service_account_json": map[string]any{"project_id": "project-1", "client_email": "fixture@example.invalid", "private_key": "fixture-cached"}}
					if mode == "canonical" {
						model = "claude-sonnet-4-5@20250929"
						f.account.Credentials["model_mapping"] = map[string]any{model: model}
					} else if mode == "no-map" {
						delete(f.account.Credentials, "model_mapping")
					} else {
						f.account.Credentials["model_mapping"] = map[string]any{model: "claude-sonnet-4-5@20250929"}
					}
					frozen := resolveGatewayRequestAccountUpstreamModel(context.Background(), f.account, model)
					f.group.ModelPricing[0].Models = append(f.group.ModelPricing[0].Models, frozen)
					ctx := context.Background()
					if locked {
						ctx = f.lockedContext(t, frozen)
					}
					up := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(namespaceToolAnthropicStream()))}}
					svc := &GatewayService{cfg: &config.Config{}, httpUpstream: up, claudeTokenProvider: &ClaudeTokenProvider{tokenCache: &fakeGeminiTokenCache{token: "fixture"}}}
					body := fmt.Sprintf(`{"model":%q,"input":"hi","messages":[{"role":"user","content":"hi"}]}`, model)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)).WithContext(ctx)
					var result *ForwardResult
					var err error
					if chat {
						result, err = svc.ForwardAsChatCompletions(ctx, c, f.account, []byte(body), nil)
					} else {
						result, err = svc.ForwardAsResponses(ctx, c, f.account, []byte(body), nil)
					}

					require.NoError(t, err)
					require.NotNil(t, up.lastReq)
					t.Logf("kind=%s locked=%t frozen=%s result=%s url=%s bodyModel=%s", kind, locked, frozen, result.UpstreamModel, up.lastReq.URL, gjson.GetBytes(up.lastBody, "model").String())
					if locked {
						require.Equal(t, frozen, result.UpstreamModel)
					}
					if kind == AccountTypeServiceAccount {
						want := frozen
						if !locked {
							want = normalizeVertexAnthropicModelID(claude.NormalizeModelID(model))
							require.Equal(t, frozen, want)
						}
						require.Contains(t, up.lastReq.URL.Path, want)
						require.False(t, gjson.GetBytes(up.lastBody, "model").Exists())
					}
					if kind == AccountTypeOAuth {
						require.Equal(t, claude.NormalizeModelID(model), result.UpstreamModel)
					}
					if kind == AccountTypeBedrock {
						require.Equal(t, "api.anthropic.com", up.lastReq.URL.Host, "documents old adapter lacks Bedrock routing, not cloud support")
					}
				})
			}
		}
	}
}
func TestCrossFrozenAdapterIdentityEvidence(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeServiceAccount, AccountTypeBedrock} {
		for _, locked := range []bool{false, true} {
			for _, chat := range []bool{false, true} {
				t.Run(fmt.Sprint(kind, locked, chat), func(t *testing.T) {
					model := "claude-sonnet-4-5"
					f := newSharedLockedFixture(t, true, false)
					f.account.Type = kind
					f.account.Credentials = map[string]any{"access_token": "fixture", "project_id": "project-1", "region": "us-east-1", "aws_region": "us-east-1", "model_mapping": map[string]any{model: model}, "service_account_json": map[string]any{"project_id": "project-1", "client_email": "fixture@example.invalid", "private_key": "fixture-cached"}}
					frozen := resolveGatewayRequestAccountUpstreamModel(context.Background(), f.account, model)
					f.group.ModelPricing[0].Models = append(f.group.ModelPricing[0].Models, frozen)
					ctx := context.Background()
					if locked {
						ctx = f.lockedContext(t, frozen)
					}
					up := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(namespaceToolAnthropicStream()))}}
					svc := &GatewayService{cfg: &config.Config{}, httpUpstream: up, claudeTokenProvider: &ClaudeTokenProvider{tokenCache: &fakeGeminiTokenCache{token: "fixture"}}}
					body := fmt.Sprintf(`{"model":%q,"input":"hi","messages":[{"role":"user","content":"hi"}]}`, model)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)).WithContext(ctx)
					var result *ForwardResult
					var err error
					if chat {
						result, err = svc.ForwardAsChatCompletions(ctx, c, f.account, []byte(body), nil)
					} else {
						result, err = svc.ForwardAsResponses(ctx, c, f.account, []byte(body), nil)
					}
					if locked && (kind == AccountTypeServiceAccount || kind == AccountTypeBedrock) {
						require.ErrorIs(t, err, ErrModelPricingUnavailable)
						require.Nil(t, up.lastReq)
						require.Nil(t, result)
						return
					}
					require.NoError(t, err)
					require.NotNil(t, up.lastReq)
					t.Logf("kind=%s locked=%t frozen=%s result=%s url=%s bodyModel=%s", kind, locked, frozen, result.UpstreamModel, up.lastReq.URL, gjson.GetBytes(up.lastBody, "model").String())
					if locked {
						require.Equal(t, frozen, result.UpstreamModel)
					}
					if kind == AccountTypeServiceAccount {
						want := frozen
						if !locked {
							want = normalizeVertexAnthropicModelID(claude.NormalizeModelID(model))
							require.NotEqual(t, frozen, want)
						}
						require.Contains(t, up.lastReq.URL.Path, want)
						require.False(t, gjson.GetBytes(up.lastBody, "model").Exists())
					}
					if kind == AccountTypeOAuth {
						require.Equal(t, claude.NormalizeModelID(model), result.UpstreamModel)
					}
					if kind == AccountTypeBedrock {
						require.Equal(t, "api.anthropic.com", up.lastReq.URL.Host, "documents old adapter lacks Bedrock routing, not cloud support")
					}
				})
			}
		}
	}
}
