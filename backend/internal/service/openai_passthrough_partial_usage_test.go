//go:build unit

package service

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIPassthroughMeasuredPartial(t *testing.T) {
	for _, usage := range []string{`{"input_tokens":10,"output_tokens":4}`, `{"input_tokens_details":{"cached_tokens":7}}`, `{"input_tokens":0,"output_tokens":0}`} {
		for _, tail := range []string{"", "\n\n"} {
			for _, terminalErr := range []error{geminiPartialReadErr, context.Canceled} {
				t.Run(fmt.Sprint(usage, tail, terminalErr), func(t *testing.T) {
					payload := `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"visible"}

event: response.in_progress
data: {"type":"response.in_progress","response":{"id":"resp_partial","model":"gpt-5.4","usage":` + usage + "}}" + tail
					up := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &geminiPartialReader{data: []byte(payload), terminal: terminalErr}}}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: up}
					a := &Account{ID: 123, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "base_url": "https://fixture.invalid"}, Extra: map[string]any{"openai_passthrough": true}}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					body := []byte(`{"model":"gpt-5.4","stream":true,"input":"hi"}`)
					c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body)))
					r, err := svc.Forward(context.Background(), c, a, body)
					require.ErrorIs(t, err, terminalErr)
					require.Len(t, up.requests, 1)
					require.Contains(t, rec.Body.String(), "visible")
					if strings.Contains(usage, ":0") {
						require.Nil(t, r)
					} else {
						require.NotNil(t, r)
						require.Equal(t, "gpt-5.4", r.Model) // Unmapped passthrough leaves UpstreamModel empty by its existing contract.
						require.Equal(t, "resp_partial", r.ResponseID)
						if strings.Contains(usage, ":10") {
							require.Equal(t, 10, r.Usage.InputTokens)
							require.Equal(t, 4, r.Usage.OutputTokens)
						} else {
							require.Equal(t, 7, r.Usage.CacheReadInputTokens)
						}
					}
				})
			}
		}
	}
}
func TestOpenAIPassthroughPartialDimensions(t *testing.T) {
	for _, u := range []OpenAIUsage{{InputTokens: 1}, {OutputTokens: 1}, {ImageInputTokens: 1}, {ImageCacheReadTokens: 1}, {CacheReadInputTokens: 1}, {CacheCreationInputTokens: 1}, {ImageOutputTokens: 1}} {
		r := &openaiStreamingResultPassthrough{usage: &u}
		require.True(t, hasMeasuredPassthroughPartial(r, geminiPartialReadErr))
		require.False(t, hasMeasuredPassthroughPartial(r, &UpstreamFailoverError{StatusCode: 503}))
	}
	require.False(t, hasMeasuredPassthroughPartial(&openaiStreamingResultPassthrough{usage: &OpenAIUsage{}, imageCount: 1}, geminiPartialReadErr))
	require.False(t, hasMeasuredPassthroughPartial(nil, geminiPartialReadErr))
}
