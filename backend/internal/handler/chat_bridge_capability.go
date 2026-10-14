package handler

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"reflect"
	"strings"
)

type chatBridgeProtocol string

const (
	chatBridgeRawChat   chatBridgeProtocol = "raw_chat"
	chatBridgeResponses chatBridgeProtocol = "responses"
	chatBridgeAnthropic chatBridgeProtocol = "anthropic_messages"
)

type chatBridgeCapabilityClass string

const (
	chatBridgeCompatible  chatBridgeCapabilityClass = "compatible"
	chatBridgeIllegal     chatBridgeCapabilityClass = "illegal_request"
	chatBridgeUnsupported chatBridgeCapabilityClass = "unsupported_target"
)

// A decision is a routing intent, not an upstream error or a health report.
// Unsupported means C-5 capability mismatch; illegal means fail_client.
type chatBridgeCapabilityDecision struct {
	Class         chatBridgeCapabilityClass
	Field, Reason string
}

func chatBridgeDecision(c chatBridgeCapabilityClass, f, r string) chatBridgeCapabilityDecision {
	return chatBridgeCapabilityDecision{c, f, r}
}
func chatBridgeJSONEqual(a, b []byte) bool {
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	return json.Valid(a) && json.Valid(b) && da.Decode(&x) == nil && db.Decode(&y) == nil && reflect.DeepEqual(x, y)
}

// Accept only one exact text block: no concatenation or whitespace normalization.
func chatBridgeExactText(raw json.RawMessage) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) != 1 {
		return "", false
	}
	switch blocks[0].Type {
	case "text", "input_text", "output_text":
		return blocks[0].Text, true
	}
	return "", false
}
func chatBridgeUnknown(m map[string]json.RawMessage, allowed string) string {
	keys := strings.Fields(allowed)
	for k := range m {
		found := false
		for _, known := range keys {
			if k == known {
				found = true
				break
			}
		}
		if !found {
			return k
		}
	}
	return ""
}

// assessChatBridgeCapability never mutates body and has no Gin/service side effects.
// It is intentionally a conservative conversion gate, not a complete API validator.
func assessChatBridgeCapability(body []byte, frozenModel string, protocol chatBridgeProtocol) chatBridgeCapabilityDecision {
	illegal := func(f, r string) chatBridgeCapabilityDecision { return chatBridgeDecision(chatBridgeIllegal, f, r) }
	unsupported := func(f, r string) chatBridgeCapabilityDecision { return chatBridgeDecision(chatBridgeUnsupported, f, r) }
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return illegal("body", "expected JSON object")
	}
	var req apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return illegal("body", "invalid field type")
	}
	if strings.TrimSpace(req.Model) == "" || len(req.Messages) == 0 {
		return illegal("model/messages", "model and messages are required")
	}
	for _, p := range []*int{req.MaxTokens, req.MaxCompletionTokens} {
		if p != nil && *p <= 0 {
			return illegal("max_tokens", "token limits must be positive")
		}
	}
	for _, m := range req.Messages {
		if m.Role == "" {
			return illegal("messages.role", "role is required")
		}
		var v any
		if len(m.Content) > 0 && json.Unmarshal(m.Content, &v) != nil {
			return illegal("messages.content", "invalid JSON")
		}
		switch v.(type) {
		case nil, string, []any:
		default:
			return illegal("messages.content", "expected text or content parts")
		}
	}
	for _, tool := range req.Tools {
		if tool.Type == "function" && (tool.Function == nil || tool.Function.Name == "") {
			return illegal("tools.function", "function name is required")
		}
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return illegal("temperature", "out of range")
	}
	if req.TopP != nil && (*req.TopP < 0 || *req.TopP > 1) {
		return illegal("top_p", "out of range")
	}
	if raw, ok := fields["tool_choice"]; ok {
		var v any
		_ = json.Unmarshal(raw, &v)
		switch v.(type) {
		case string, map[string]any:
		default:
			return illegal("tool_choice", "expected string or object")
		}
	}
	for _, tool := range req.Tools {
		if tool.Function != nil && len(tool.Function.Parameters) > 0 {
			var schema map[string]json.RawMessage
			if json.Unmarshal(tool.Function.Parameters, &schema) != nil || schema == nil {
				return illegal("tools.function.parameters", "expected schema object")
			}
		}
	}
	if raw, ok := fields["response_format"]; ok {
		var v map[string]json.RawMessage
		if json.Unmarshal(raw, &v) != nil || v == nil {
			return illegal("response_format", "expected object")
		}
	}
	if frozenModel == "" {
		return unsupported("model", "missing frozen upstream model")
	}
	if protocol == chatBridgeRawChat {
		return chatBridgeDecision(chatBridgeCompatible, "", "")
	}
	if protocol != chatBridgeResponses && protocol != chatBridgeAnthropic {
		return unsupported("protocol", "unknown target protocol")
	}
	if f := chatBridgeUnknown(fields, "model messages instructions max_tokens max_completion_tokens temperature top_p stream stream_options tools tool_choice parallel_tool_calls reasoning_effort service_tier response_format"); f != "" {
		return unsupported("extension", "field is not proven preserved by translator")
	}
	var messages []map[string]json.RawMessage
	_ = json.Unmarshal(fields["messages"], &messages)
	for _, m := range messages {
		if f := chatBridgeUnknown(m, "role content"); f != "" {
			return unsupported("messages.extension", "message extension is not proven preserved")
		}
		var text string
		if json.Unmarshal(m["content"], &text) != nil {
			return unsupported("messages.content", "only plain text is proven by this slice")
		}
		if strings.TrimSpace(text) != text || text == "" {
			return unsupported("messages.content", "text normalization is not lossless")
		}
		role := string(m["role"])
		if role != `"user"` && role != `"assistant"` && role != `"system"` && role != `"developer"` {
			return unsupported("messages.role", "role is not proven preserved")
		}
	}
	if raw, ok := fields["stream_options"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return illegal("stream_options", "expected object")
		}
		if f := chatBridgeUnknown(m, "include_usage"); f != "" {
			return unsupported("stream_options.extension", "unknown option")
		}
	}
	var tools []map[string]json.RawMessage
	_ = json.Unmarshal(fields["tools"], &tools)
	for _, tool := range tools {
		if f := chatBridgeUnknown(tool, "type function"); f != "" {
			return unsupported("tools.extension", "unknown tool extension")
		}
		var fn map[string]json.RawMessage
		if json.Unmarshal(tool["function"], &fn) != nil {
			return unsupported("tools", "only function tools supported")
		}
		if f := chatBridgeUnknown(fn, "name description parameters strict"); f != "" {
			return unsupported("tools.function.extension", "unknown function extension")
		}
		if protocol == chatBridgeAnthropic {
			if _, ok := fn["strict"]; ok {
				return unsupported("tools.function.strict", "strict is discarded by Anthropic converter")
			}
		}
	}
	if raw, ok := fields["tool_choice"]; ok {
		var choice any
		_ = json.Unmarshal(raw, &choice)
		switch v := choice.(type) {
		case string:
			if v != "auto" && v != "none" && v != "required" {
				return unsupported("tool_choice", "unknown choice")
			}
		case map[string]any:
			if len(v) != 2 || v["type"] != "function" {
				return unsupported("tool_choice", "unknown choice extension")
			}
			fn, ok := v["function"].(map[string]any)
			if !ok || len(fn) != 1 {
				return unsupported("tool_choice.function", "unknown choice extension")
			}
		default:
			return illegal("tool_choice", "expected string or object")
		}
	}
	responses, err := apicompat.ChatCompletionsToResponses(&req)
	if err != nil {
		return unsupported("conversion", "target conversion cannot preserve this request")
	}
	if responses.Instructions != req.Instructions {
		return unsupported("instructions", "converter changes instructions")
	}
	var convertedMessages []apicompat.ResponsesInputItem
	if json.Unmarshal(responses.Input, &convertedMessages) != nil || len(convertedMessages) != len(req.Messages) {
		return unsupported("messages", "converter changes message sequence")
	}
	for i, m := range req.Messages {
		original, _ := chatBridgeExactText(m.Content)
		actual, ok := chatBridgeExactText(convertedMessages[i].Content)
		if !ok || convertedMessages[i].Role != m.Role || actual != original {
			return unsupported("messages", "converter changes role or text order")
		}
	}
	limit := req.MaxTokens
	if req.MaxCompletionTokens != nil {
		limit = req.MaxCompletionTokens
	}
	if limit != nil && (responses.MaxOutputTokens == nil || *limit != *responses.MaxOutputTokens) {
		return unsupported("max_tokens", "converter changes token limit")
	}
	if !reflect.DeepEqual(req.Temperature, responses.Temperature) || !reflect.DeepEqual(req.TopP, responses.TopP) {
		return unsupported("sampling", "converter drops sampling")
	}
	if protocol == chatBridgeResponses {
		if raw, ok := fields["response_format"]; ok {
			var format map[string]json.RawMessage
			_ = json.Unmarshal(raw, &format)
			if f := chatBridgeUnknown(format, "type json_schema"); f != "" {
				return unsupported("response_format.extension", "unknown format extension")
			}
			var kind string
			_ = json.Unmarshal(format["type"], &kind)
			if kind != "json_schema" && kind != "json_object" && kind != "text" {
				return unsupported("response_format.type", "unknown format")
			}
			if kind == "json_schema" {
				var schema map[string]json.RawMessage
				if json.Unmarshal(format["json_schema"], &schema) != nil {
					return illegal("response_format.json_schema", "expected object")
				}
				if f := chatBridgeUnknown(schema, "name description schema strict"); f != "" {
					return unsupported("response_format.json_schema.extension", "unknown schema wrapper extension")
				}
				if responses.Text == nil {
					return unsupported("response_format", "converter drops format")
				}
				var converted map[string]json.RawMessage
				_ = json.Unmarshal(responses.Text.Format, &converted)
				for k, v := range schema {
					if !chatBridgeJSONEqual(v, converted[k]) {
						return unsupported("response_format.json_schema", "converter changes schema wrapper")
					}
				}
			}
		}
		if !reflect.DeepEqual(req.ParallelToolCalls, responses.ParallelToolCalls) || req.ServiceTier != responses.ServiceTier {
			return unsupported("options", "converter changes options")
		}
		if len(responses.Tools) != len(req.Tools) {
			return unsupported("tools", "converter changes tool set")
		}
		for i, t := range req.Tools {
			if t.Function == nil {
				return unsupported("tools", "non-function tool")
			}
			f := t.Function
			a := responses.Tools[i]
			if a.Name != f.Name || a.Description != f.Description || !reflect.DeepEqual(a.Strict, f.Strict) || !chatBridgeJSONEqual(f.Parameters, a.Parameters) {
				return unsupported("tools.function", "converter changes function definition")
			}
		}
		return chatBridgeDecision(chatBridgeCompatible, "", "")
	}
	for _, f := range []string{"parallel_tool_calls", "service_tier", "response_format"} {
		if _, ok := fields[f]; ok {
			return unsupported(f, "Anthropic converter does not preserve field")
		}
	}
	if req.ReasoningEffort != "" && req.ReasoningEffort != "low" && req.ReasoningEffort != "medium" && req.ReasoningEffort != "high" {
		return unsupported("reasoning_effort", "effort equivalence is not established for target")
	}
	expectedSystem := req.Instructions
	expectedMessages := req.Messages
	for i, m := range req.Messages {
		if m.Role == "system" || m.Role == "developer" {
			if i != 0 || expectedSystem != "" {
				return unsupported("instructions", "multiple or non-leading instruction sources are not lossless")
			}
			expectedSystem, _ = chatBridgeExactText(m.Content)
			expectedMessages = req.Messages[1:]
		}
	}
	responses.Model = frozenModel
	anthropic, err := apicompat.ResponsesToAnthropicRequest(responses)
	if err != nil {
		return unsupported("conversion", "target conversion cannot preserve this request")
	}
	actualSystem := ""
	if len(anthropic.System) > 0 {
		var ok bool
		actualSystem, ok = chatBridgeExactText(anthropic.System)
		if !ok {
			return unsupported("instructions", "final system structure changed")
		}
	}
	if actualSystem != expectedSystem {
		return unsupported("instructions", "final converter changes instruction text")
	}
	if len(anthropic.Messages) != len(expectedMessages) {
		return unsupported("messages", "final converter changes message sequence")
	}
	for i, m := range expectedMessages {
		original, _ := chatBridgeExactText(m.Content)
		actual, ok := chatBridgeExactText(anthropic.Messages[i].Content)
		if !ok || anthropic.Messages[i].Role != m.Role || original != actual {
			return unsupported("messages", "final converter changes role or text order")
		}
	}
	if !reflect.DeepEqual(req.Temperature, anthropic.Temperature) || !reflect.DeepEqual(req.TopP, anthropic.TopP) {
		return unsupported("sampling", "final converter changes sampling")
	}
	if len(req.ToolChoice) > 0 {
		var choice any
		_ = json.Unmarshal(req.ToolChoice, &choice)
		expected := map[string]string{}
		switch v := choice.(type) {
		case string:
			typ := v
			if typ == "required" {
				typ = "any"
			}
			expected["type"] = typ
		case map[string]any:
			fn, _ := v["function"].(map[string]any)
			name, ok := fn["name"].(string)
			if !ok || name == "" {
				return illegal("tool_choice.function.name", "name must be nonempty string")
			}
			expected["type"] = "tool"
			expected["name"] = name
		}
		encoded, _ := json.Marshal(expected)
		if !chatBridgeJSONEqual(encoded, anthropic.ToolChoice) {
			return unsupported("tool_choice", "final converter changes tool choice")
		}
	}
	if limit != nil && anthropic.MaxTokens != *limit {
		return unsupported("max_tokens", "Anthropic converter changes token limit")
	}
	if req.ReasoningEffort != "" && (anthropic.OutputConfig == nil || anthropic.OutputConfig.Effort != req.ReasoningEffort) {
		return unsupported("reasoning_effort", "converter changes reasoning effort")
	}
	if len(anthropic.Tools) != len(req.Tools) {
		return unsupported("tools", "converter changes tool set")
	}
	for i, tool := range req.Tools {
		if tool.Function == nil {
			return unsupported("tools", "non-function tool")
		}
		f := tool.Function
		a := anthropic.Tools[i]
		if a.Name != f.Name || a.Description != f.Description || !chatBridgeJSONEqual(f.Parameters, a.InputSchema) {
			return unsupported("tools.function.parameters", "converter changes function schema")
		}
	}
	return chatBridgeDecision(chatBridgeCompatible, "", "")
}
