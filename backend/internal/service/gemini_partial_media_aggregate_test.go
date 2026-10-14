//go:build unit

package service

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGeminiPartialMediaAggregate(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"image", "empty", "file", "audio", "nameonly", "token", "cache", "conflict"} {
				t.Run(fmt.Sprint(native, stream, mode), func(t *testing.T) {
					part := `{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}`
					extra := ""
					if mode == "empty" {
						part = `{"inlineData":{"mimeType":"image/png","data":""}}`
					}
					if mode == "file" {
						part = `{"fileData":{"mimeType":"image/png","fileUri":"fixture"}}`
					}
					if mode == "audio" {
						part = `{"inlineData":{"mimeType":"audio/wav","data":"YQ=="}}`
					}
					if mode == "nameonly" {
						part = `{"text":"hello"}`
					}
					if mode == "cache" {
						part = `{"text":"hello"}`
						extra = `,"usageMetadata":{"cachedContentTokenCount":7}`
					}
					if mode == "token" {
						part = `{"text":"ok"}`
						extra = `,"usageMetadata":{"promptTokenCount":5}`
					}
					payload := `data: {"response":{"modelVersion":"model-a","candidates":[{"content":{"parts":[` + part + "," + part + "]}}]" + extra + "}}\n\n"
					if mode == "conflict" {
						payload += `data: {"response":{"modelVersion":"model-b","candidates":[{"content":{"parts":[]}}]}}`
					} else {
						payload += payload
					}
					svc := newGeminiSignalService("text/event-stream", "")
					svc.tokenProvider = &GeminiTokenProvider{}
					stub := svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
					stub.response.Body = &geminiPartialReader{data: []byte(payload), terminal: geminiPartialReadErr}
					account := geminiSignalTestAccount()
					account.Type = AccountTypeOAuth
					account.Credentials = map[string]any{"access_token": "fixture", "project_id": "fixture-project"}
					c, rec := newGeminiNativeTestContext(t)
					var r *ForwardResult
					var err error
					if native {
						action := "generateContent"
						if stream {
							action = "streamGenerateContent"
						}
						r, err = svc.ForwardNative(context.Background(), c, account, "gemini-image-model", action, stream, geminiSignalTestRequest())
					} else {
						r, err = svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gemini-image-model","stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream)))
					}
					require.ErrorIs(t, err, geminiPartialReadErr)
					if !stream {
						require.Equal(t, 502, rec.Code)
						require.NotContains(t, rec.Body.String(), "candidates")
					}
					if mode == "empty" || mode == "file" || mode == "audio" || mode == "nameonly" {
						require.Nil(t, r)
					} else {
						require.NotNil(t, r)
						if mode == "token" {
							require.Zero(t, r.ImageCount)
							require.Equal(t, 5, r.Usage.InputTokens)
						} else if mode == "cache" {
							require.Zero(t, r.ImageCount)
							require.Equal(t, 7, r.Usage.CacheReadInputTokens)
						} else {
							require.Equal(t, 2, r.ImageCount)
							require.False(t, hasMeasuredGeminiUsage(&r.Usage))
						}
						if mode == "conflict" {
							require.True(t, r.UpstreamResponseModelConflict)
						}
					}
					require.Equal(t, 1, stub.calls)
				})
			}
		}
	}
}
