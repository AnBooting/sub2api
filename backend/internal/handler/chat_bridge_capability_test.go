package handler

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChatBridgeRawInvalidAndSchemaExtensions(t *testing.T) {
	for _, extra := range []string{`,"tool_choice":42`, `,"response_format":[]`, `,"tools":[{"type":"function","function":{"name":"f","parameters":[]}}]`} {
		body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}]` + extra + "}")
		require.Equal(t, chatBridgeIllegal, assessChatBridgeCapability(body, "x", chatBridgeRawChat).Class)
	}
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{"type":"object"},"secret_extension":"payload-secret"}}}`)
	d := assessChatBridgeCapability(body, "x", chatBridgeResponses)
	require.Equal(t, chatBridgeUnsupported, d.Class)
	require.NotContains(t, d.Field+d.Reason, "payload-secret")
}
func TestChatBridgeExactKeysAndNumbers(t *testing.T) {
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi"}],"model messages":true}`)
	require.Equal(t, chatBridgeUnsupported, assessChatBridgeCapability(body, "x", chatBridgeAnthropic).Class)
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "x", chatBridgeRawChat).Class)
	require.False(t, chatBridgeJSONEqual([]byte(`{"maximum":9007199254740992}`), []byte(`{"maximum":9007199254740993}`)))
	require.True(t, chatBridgeJSONEqual([]byte(`{"maximum":9007199254740993}`), []byte(`{ "maximum":9007199254740993 }`)))
}
func TestChatBridgeFinalAnthropicChoice(t *testing.T) {
	for _, choice := range []string{`"auto"`, `"none"`, `"required"`, `{"type":"function","function":{"name":"lookup"}}`} {
		body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"temperature":0.7,"top_p":0.8,"tool_choice":` + choice + "}")
		require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "claude-sonnet-4", chatBridgeAnthropic).Class)
	}
	for _, extra := range []string{`,"tool_choice":"required"`, `,"tool_choice":{"type":"function","function":{"name":"lookup"}}`, `,"temperature":0.7`} {
		body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}]` + extra + "}")
		require.Equal(t, chatBridgeUnsupported, assessChatBridgeCapability(body, "claude-sonnet-5-5", chatBridgeAnthropic).Class)
	}
	body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"function","function":{"name":" lookup "}}}`)
	require.Equal(t, chatBridgeUnsupported, assessChatBridgeCapability(body, "claude-sonnet-4", chatBridgeAnthropic).Class)
}
func TestChatBridgeInstructionSequence(t *testing.T) {
	for _, tc := range []struct {
		name, instructions, messages string
		want                         chatBridgeCapabilityClass
	}{
		{"instructions trim merge", `,"instructions":"A "`, `[{"role":"system","content":"B"},{"role":"user","content":"hi"}]`, chatBridgeUnsupported},
		{"instructions trim", `,"instructions":"A "`, `[{"role":"user","content":"hi"}]`, chatBridgeUnsupported},
		{"multiple system", "", `[{"role":"system","content":"A"},{"role":"developer","content":"B"},{"role":"user","content":"hi"}]`, chatBridgeUnsupported},
		{"middle system", "", `[{"role":"user","content":"hi"},{"role":"system","content":"B"}]`, chatBridgeUnsupported},
		{"middle developer", "", `[{"role":"user","content":"hi"},{"role":"developer","content":"B"}]`, chatBridgeUnsupported},
		{"single leading", "", `[{"role":"system","content":"A"},{"role":"user","content":"hi"},{"role":"assistant","content":"reply"},{"role":"user","content":"next"}]`, chatBridgeCompatible},
		{"single instructions", `,"instructions":"A"`, `[{"role":"user","content":"hi"}]`, chatBridgeCompatible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"client-model","messages":` + tc.messages + tc.instructions + "}")
			before := bytes.Clone(body)
			require.Equal(t, tc.want, assessChatBridgeCapability(body, "claude-sonnet-4", chatBridgeAnthropic).Class)
			require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "x", chatBridgeRawChat).Class)
			require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "x", chatBridgeResponses).Class)
			require.Equal(t, before, body)
		})
	}
}
func TestChatBridgeCapabilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		p           chatBridgeProtocol
		want        chatBridgeCapabilityClass
	}{
		{"basic", "", chatBridgeAnthropic, chatBridgeCompatible}, {"raw unknown", `,"future":{"secret":"value"}`, chatBridgeRawChat, chatBridgeCompatible}, {"translated unknown", `,"future":true`, chatBridgeAnthropic, chatBridgeUnsupported},
		{"token small", `,"max_tokens":64`, chatBridgeAnthropic, chatBridgeUnsupported}, {"raw small", `,"max_tokens":64`, chatBridgeRawChat, chatBridgeCompatible}, {"limit", `,"max_tokens":256`, chatBridgeAnthropic, chatBridgeCompatible}, {"priority", `,"max_tokens":64,"max_completion_tokens":256`, chatBridgeAnthropic, chatBridgeCompatible}, {"negative even raw", `,"max_tokens":-1`, chatBridgeRawChat, chatBridgeIllegal}, {"wrongtype", `,"stream":"true"`, chatBridgeRawChat, chatBridgeIllegal},
		{"parallel", `,"parallel_tool_calls":false`, chatBridgeAnthropic, chatBridgeUnsupported}, {"parallel responses", `,"parallel_tool_calls":false`, chatBridgeResponses, chatBridgeCompatible}, {"tier", `,"service_tier":"priority"`, chatBridgeAnthropic, chatBridgeUnsupported}, {"schema", `,"response_format":{"type":"json_schema","json_schema":{"name":"x","strict":true,"schema":{"type":"object"}}}`, chatBridgeAnthropic, chatBridgeUnsupported},
		{"high", `,"reasoning_effort":"high"`, chatBridgeAnthropic, chatBridgeCompatible}, {"xhigh", `,"reasoning_effort":"xhigh"`, chatBridgeAnthropic, chatBridgeUnsupported}, {"sampling", `,"temperature":0.7,"top_p":0.8`, chatBridgeAnthropic, chatBridgeCompatible}, {"invalid sampling", `,"temperature":3`, chatBridgeRawChat, chatBridgeIllegal},
		{"tools", `,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}}`, chatBridgeAnthropic, chatBridgeCompatible},
		{"strict", `,"tools":[{"type":"function","function":{"name":"lookup","strict":false,"parameters":{"type":"object"}}}]`, chatBridgeAnthropic, chatBridgeUnsupported},
		{"missing function", `,"tools":[{"type":"function"}]`, chatBridgeRawChat, chatBridgeIllegal}, {"unknown protocol", "", chatBridgeProtocol("future"), chatBridgeUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hello"}]` + tc.extra + "}")
			before := bytes.Clone(body)
			got := assessChatBridgeCapability(body, "claude-sonnet-4", tc.p)
			require.Equal(t, tc.want, got.Class, got)
			require.Equal(t, before, body)
		})
	}
}
func TestChatBridgeCapabilityNestedAndParser(t *testing.T) {
	for _, body := range []string{`[]`, `{"model":"x","messages":[{"role":"user","content":1}]}`, `{"model":"x","messages":[]} trailing`} {
		require.Equal(t, chatBridgeIllegal, assessChatBridgeCapability([]byte(body), "x", chatBridgeRawChat).Class)
	}
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"hi","extension":1}]}`)
	require.Equal(t, chatBridgeUnsupported, assessChatBridgeCapability(body, "x", chatBridgeAnthropic).Class)
	require.Equal(t, chatBridgeCompatible, assessChatBridgeCapability(body, "x", chatBridgeRawChat).Class)
}
func TestChatBridgeRealConversionEvidence(t *testing.T) {
	var req apicompat.ChatCompletionsRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"client-model","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"parallel_tool_calls":false,"service_tier":"priority","reasoning_effort":"xhigh"}`), &req))
	r, err := apicompat.ChatCompletionsToResponses(&req)
	require.NoError(t, err)
	require.Equal(t, 128, *r.MaxOutputTokens)
	require.False(t, *r.ParallelToolCalls)
	require.Equal(t, "priority", r.ServiceTier)
	r.Model = "claude-sonnet-4"
	a, err := apicompat.ResponsesToAnthropicRequest(r)
	require.NoError(t, err)
	require.Equal(t, "max", a.OutputConfig.Effort)
	encoded, err := json.Marshal(a)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "parallel_tool_calls")
	require.NotContains(t, string(encoded), "service_tier")
}
