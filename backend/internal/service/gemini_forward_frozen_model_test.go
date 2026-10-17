//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func geminiFrozenFixture(t *testing.T, kind string) (*sharedLockedFixture, context.Context, *RequestBillingContext) {
	f := newSharedLockedFixture(t, true, false)
	f.group.Platform = PlatformGemini
	f.account.Platform = PlatformGemini
	f.account.Type = kind
	f.account.Credentials = map[string]any{"api_key": "fixture", "access_token": "fixture", "project_id": "project-1", "model_mapping": map[string]any{"client-model": "gemini-2.5-flash"}, "service_account_json": map[string]any{"project_id": "project-1", "client_email": "fixture@example.invalid", "private_key": "fixture-not-used-cached-token"}}
	f.group.ModelPricing[0].Models = []string{"client-model", "gemini-2.5-flash", "gemini-2.5-pro"}
	snap := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(f.group.ID, PlatformGemini): {*f.account}}}
	attachInflightSnapshot(f.svc, snap)
	ctx, bundle, err := f.svc.PrepareRequestBilling(context.Background(), f.key, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	return f, ctx, bundle
}
func geminiFrozenForward(t *testing.T, ctx context.Context, account *Account, native, stream bool) (*ForwardResult, error, *geminiCompatHTTPUpstreamStub) {
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
	if native {
		action := "generateContent"
		if stream {
			action = "streamGenerateContent"
		}
		result, err = svc.ForwardNative(ctx, c, account, "client-model", action, stream, geminiSignalTestRequest())
	} else {
		body := []byte(fmt.Sprintf(`{"model":"client-model","stream":%t,"max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`, stream))
		result, err = svc.Forward(ctx, c, account, body)
	}
	return result, err, svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
}
func TestGeminiFrozenForwardUsesSnapshotModel(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeServiceAccount, AccountTypeOAuth} {
		for _, native := range []bool{false, true} {
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
					result, err, httpStub := geminiFrozenForward(t, ctx, f.account, native, stream)
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
func TestGeminiFrozenForwardUnlockedCompatibility(t *testing.T) {
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeServiceAccount, AccountTypeOAuth} {
		for _, native := range []bool{false, true} {
			t.Run(fmt.Sprint(kind, native), func(t *testing.T) {
				f, _, _ := geminiFrozenFixture(t, kind)
				result, err, httpStub := geminiFrozenForward(t, context.Background(), f.account, native, false)
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
func TestGeminiFrozenForwardRejectsUnboundOrMismatchedAccount(t *testing.T) {
	for _, mode := range []string{"unbound", "wrong-account", "missing-mapping"} {
		for _, native := range []bool{false, true} {
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
				result, err, httpStub := geminiFrozenForward(t, ctx, f.account, native, false)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Nil(t, result)
				require.Equal(t, 0, httpStub.calls)
			})
		}
	}
}
