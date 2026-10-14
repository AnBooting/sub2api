//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

var geminiPartialReadErr = errors.New("fixture upstream disconnected")

type geminiPartialReader struct {
	data     []byte
	terminal error
}

func (r *geminiPartialReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.terminal
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.terminal
	}
	return n, nil
}
func (r *geminiPartialReader) Close() error { return nil }
func TestGeminiPartialUsageProducer(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, usage := range []string{`"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}`, `"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0}`, `"usageMetadata":{"cachedContentTokenCount":7}`, `"unused":0`} {
			for _, newline := range []bool{false, true} {
				t.Run(fmt.Sprint(native, usage, newline), func(t *testing.T) {
					payload := `data: {"candidates":[{"content":{"parts":[{"text":"ok"}]}}],` + usage + "}"
					if newline {
						payload += "\n\n"
					}
					svc := newGeminiSignalService("text/event-stream", "")
					stub := svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
					stub.response.Body = &geminiPartialReader{data: []byte(payload), terminal: geminiPartialReadErr}
					c, rec := newGeminiNativeTestContext(t)
					var result *ForwardResult
					var err error
					if native {
						result, err = svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(), "gemini-2.5-flash", "streamGenerateContent", true, geminiSignalTestRequest())
					} else {
						result, err = svc.Forward(context.Background(), c, geminiSignalTestAccount(), []byte(`{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
					}
					require.ErrorIs(t, err, geminiPartialReadErr)
					require.Equal(t, 1, stub.calls)
					measured := strings.Contains(usage, ":10") || strings.Contains(usage, ":7")
					if measured {
						require.NotNil(t, result)
						require.Equal(t, "gemini-2.5-flash", result.UpstreamModel)
						require.True(t, hasMeasuredGeminiUsage(&result.Usage))
					} else {
						require.Nil(t, result)
					}
					if !native {
						require.NotContains(t, rec.Body.String(), "event: message_stop")
					}
				})
			}
		}
	}
}
func TestGeminiPartialUsageAllDimensions(t *testing.T) {
	cases := []ClaudeUsage{{InputTokens: 1}, {OutputTokens: 1}, {CacheCreationInputTokens: 1}, {CacheReadInputTokens: 1}, {CacheCreation5mTokens: 1}, {CacheCreation1hTokens: 1}, {ImageOutputTokens: 1}}
	for _, u := range cases {
		require.True(t, hasMeasuredGeminiUsage(&u))
	}
	require.False(t, hasMeasuredGeminiUsage(nil))
	require.False(t, hasMeasuredGeminiUsage(&ClaudeUsage{}))
}
func TestGeminiPartialUsageCanceledWithoutMeasurement(t *testing.T) {
	for _, native := range []bool{false, true} {
		svc := newGeminiSignalService("text/event-stream", "")
		stub := svc.httpUpstream.(*geminiCompatHTTPUpstreamStub)
		stub.response.Body = &geminiPartialReader{terminal: context.Canceled}
		c, _ := newGeminiNativeTestContext(t)
		var result *ForwardResult
		var err error
		if native {
			result, err = svc.ForwardNative(context.Background(), c, geminiSignalTestAccount(), "gemini-2.5-flash", "streamGenerateContent", true, geminiSignalTestRequest())
		} else {
			result, err = svc.Forward(context.Background(), c, geminiSignalTestAccount(), []byte(`{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
		}
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, result)
	}
}
