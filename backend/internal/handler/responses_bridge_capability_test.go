package handler

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResponsesBridgeCapability(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       chatBridgeCapabilityClass
	}{
		{"text", `{"model":"alias","input":"hi","max_output_tokens":64}`, chatBridgeCompatible},
		{"ordered", `{"model":"alias","input":[{"role":"system","content":"A"},{"role":"user","content":[{"type":"input_text","text":"hi"}]},{"role":"assistant","content":"reply"},{"role":"user","content":"next"}]}`, chatBridgeCompatible},
		{"instructions", `{"model":"alias","input":"hi","instructions":"A","temperature":0.7,"top_p":0.8,"reasoning":{"effort":"high"}}`, chatBridgeCompatible},
		{"merge", `{"model":"alias","instructions":"A ","input":[{"role":"system","content":"B"},{"role":"user","content":"hi"}]}`, chatBridgeUnsupported},
		{"trim", `{"model":"alias","instructions":"A ","input":"hi"}`, chatBridgeUnsupported},
		{"middle", `{"model":"alias","input":[{"role":"user","content":"hi"},{"role":"developer","content":"A"}]}`, chatBridgeUnsupported},
		{"strict", `{"model":"alias","input":"hi","tools":[{"type":"function","name":"f","strict":false,"parameters":{"type":"object"}}]}`, chatBridgeUnsupported},
		{"schema numbers", `{"model":"alias","input":"hi","tools":[{"type":"function","name":"f","parameters":{"type":"object","properties":{"n":{"type":"integer","maximum":9007199254740993}}}}],"tool_choice":{"type":"function","name":"f"}}`, chatBridgeCompatible},
		{"parallel", `{"model":"alias","input":"hi","parallel_tool_calls":false}`, chatBridgeUnsupported},
		{"schema", `{"model":"alias","input":"hi","text":{"format":{"type":"json_schema"}}}`, chatBridgeUnsupported},
		{"tier", `{"model":"alias","input":"hi","service_tier":"priority"}`, chatBridgeUnsupported},
		{"unknown space", `{"model":"alias","input":"hi","model input":true}`, chatBridgeUnsupported},
		{"nested", `{"model":"alias","input":[{"role":"user","content":[{"type":"input_text","text":"hi","secret":"payload"}]}]}`, chatBridgeUnsupported},
		{"encrypted", `{"model":"alias","input":[{"type":"reasoning","encrypted_content":"secret"}]}`, chatBridgeUnsupported},
		{"builtin", `{"model":"alias","input":"hi","tools":[{"type":"web_search"}]}`, chatBridgeUnsupported},
		{"xhigh", `{"model":"alias","input":"hi","reasoning":{"effort":"xhigh"}}`, chatBridgeUnsupported},
		{"negative", `{"model":"alias","input":"hi","max_output_tokens":-1}`, chatBridgeIllegal},
		{"bad input", `{"model":"alias","input":42}`, chatBridgeIllegal},
		{"tail", `{"model":"alias","input":"hi"}garbage`, chatBridgeIllegal},
		{"bad previous", `{"model":"alias","input":"hi","previous_response_id":"msg_123"}`, chatBridgeIllegal},
		{"previous", `{"model":"alias","input":"hi","previous_response_id":"resp_123"}`, chatBridgeUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := []byte(tc.body)
			before := bytes.Clone(b)
			d := assessResponsesBridgeCapability(b, "claude-sonnet-4", chatBridgeAnthropic, responsesBridgeRouteFlags{})
			require.Equal(t, tc.want, d.Class, d)
			require.Equal(t, before, b)
			require.NotContains(t, d.Reason, "secret")
		})
	}
}
func TestResponsesBridgeFinalTargetChanges(t *testing.T) {
	for _, extra := range []string{`,"temperature":0.7`, `,"tool_choice":"required"`, `,"tool_choice":{"type":"function","name":"f"}`} {
		body := []byte(`{"model":"alias","input":"hi"` + extra + "}")
		require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability(body, "claude-sonnet-5-5", chatBridgeAnthropic, responsesBridgeRouteFlags{}).Class)
	}
	for _, body := range []string{`{"model":"alias","input":"hi","stream":"yes"}`, `{"model":"alias","input":[{"type":"message","role":"user","content":3}]}`, `{"model":"alias","input":"hi","tools":[{"type":"function","name":"f","parameters":[]}]}`} {
		require.Equal(t, chatBridgeIllegal, assessResponsesBridgeCapability([]byte(body), "x", chatBridgeResponses, responsesBridgeRouteFlags{SupportsResponses: true}).Class)
	}
	body := []byte(`{"model":"alias","input":[{"type":"compaction_trigger"}]}`)
	require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability(body, "x", chatBridgeAnthropic, responsesBridgeRouteFlags{}).Class)
	require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability(body, "x", chatBridgeResponses, responsesBridgeRouteFlags{SupportsResponses: true}).Class)
}
func TestResponsesBridgeRouteFlags(t *testing.T) {
	body := []byte(`{"model":"alias","input":"hi","unknown":true}`)
	for _, flags := range []responsesBridgeRouteFlags{{}, {SupportsResponses: true, LegacyCompact: true}, {SupportsResponses: true, NativeV2: true}} {
		require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability(body, "x", chatBridgeResponses, flags).Class)
	}
	all := responsesBridgeRouteFlags{SupportsResponses: true, SupportsLegacyCompact: true, SupportsNativeV2: true, HTTPContinuationSupported: true}
	require.Equal(t, chatBridgeCompatible, assessResponsesBridgeCapability(body, "x", chatBridgeResponses, all).Class)
	for _, flags := range []responsesBridgeRouteFlags{{LegacyCompact: true}, {NativeV2: true}} {
		require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability([]byte(`{"model":"alias","input":"hi"}`), "x", chatBridgeAnthropic, flags).Class)
	}
	previous := []byte(`{"model":"alias","input":"hi","previous_response_id":"resp_123"}`)
	require.Equal(t, chatBridgeCompatible, assessResponsesBridgeCapability(previous, "x", chatBridgeResponses, all).Class)
	all.HTTPContinuationSupported = false
	require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability(previous, "x", chatBridgeResponses, all).Class)
	require.Equal(t, chatBridgeUnsupported, assessResponsesBridgeCapability(body, "x", chatBridgeRawChat, all).Class)
}
func TestResponsesBridgeConversionEvidence(t *testing.T) {
	for _, choice := range []string{`"auto"`, `"none"`, `"required"`, `{"type":"function","name":"f"}`} {
		body := []byte(`{"model":"alias","input":"hi","max_output_tokens":64,"tool_choice":` + choice + "}")
		require.Equal(t, chatBridgeCompatible, assessResponsesBridgeCapability(body, "claude-sonnet-4", chatBridgeAnthropic, responsesBridgeRouteFlags{}).Class)
	}
	var req apicompat.ResponsesRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"claude-sonnet-4","instructions":"A ","input":[{"role":"system","content":"B"},{"role":"user","content":"hi"}],"max_output_tokens":64}`), &req))
	a, err := apicompat.ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	text, valid := chatBridgeExactText(a.System)
	require.True(t, valid)
	require.Equal(t, "A\n\nB", text)
	require.Equal(t, 64, a.MaxTokens)
}
