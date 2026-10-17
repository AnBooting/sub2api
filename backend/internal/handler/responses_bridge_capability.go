package handler

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"reflect"
	"strings"
)

// Route facts are supplied independently of JSON; a compact URL need not carry
// a compaction item. Supported flags describe the actual selected endpoint.
type responsesBridgeRouteFlags struct {
	LegacyCompact, NativeV2                                    bool
	SupportsResponses, SupportsLegacyCompact, SupportsNativeV2 bool
	HTTPContinuationSupported                                  bool
}

// originalBody is the untouched canonical ingress, never a derived attempt body.
// This probe does not normalize it or return a replacement payload.
func assessResponsesBridgeCapability(originalBody []byte, frozenModel string, protocol chatBridgeProtocol, route responsesBridgeRouteFlags) chatBridgeCapabilityDecision {
	bad := func(field, reason string) chatBridgeCapabilityDecision {
		return chatBridgeDecision(chatBridgeIllegal, field, reason)
	}
	no := func(field, reason string) chatBridgeCapabilityDecision {
		return chatBridgeDecision(chatBridgeUnsupported, field, reason)
	}
	ok := chatBridgeDecision(chatBridgeCompatible, "", "")
	var fields map[string]json.RawMessage
	var req apicompat.ResponsesRequest
	if json.Unmarshal(originalBody, &fields) != nil || fields == nil {
		return bad("body", "expected JSON object")
	}
	if json.Unmarshal(originalBody, &req) != nil {
		return bad("body", "invalid field type")
	}
	if strings.TrimSpace(req.Model) == "" {
		return bad("model", "model is required")
	}
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens <= 0 {
		return bad("max_output_tokens", "must be positive")
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return bad("temperature", "out of range")
	}
	if req.TopP != nil && (*req.TopP < 0 || *req.TopP > 1) {
		return bad("top_p", "out of range")
	}
	previous := strings.TrimSpace(req.PreviousResponseID)
	if previous != "" && service.ClassifyOpenAIPreviousResponseIDKind(previous) == service.OpenAIPreviousResponseIDKindMessageID {
		return bad("previous_response_id", "message IDs cannot be used for continuation")
	}
	var items []apicompat.ResponsesInputItem
	var rawItems []map[string]json.RawMessage
	var text string
	input := bytes.TrimSpace(req.Input)
	if len(input) == 0 || bytes.Equal(input, []byte("null")) {
		return bad("input", "input is required")
	}
	if input[0] == '"' {
		if json.Unmarshal(input, &text) != nil {
			return bad("input", "invalid text")
		}
		items = []apicompat.ResponsesInputItem{{Role: "user", Content: append(json.RawMessage(nil), input...)}}
	} else {
		if input[0] != '[' || json.Unmarshal(input, &items) != nil || json.Unmarshal(input, &rawItems) != nil {
			return bad("input", "expected string or input item array")
		}
		for _, item := range items {
			if item.Type == "message" || item.Type == "" {
				if item.Role == "" {
					return bad("input.role", "role is required")
				}
				v := bytes.TrimSpace(item.Content)
				if len(v) == 0 || bytes.Equal(v, []byte("null")) || (v[0] != '"' && v[0] != '[') {
					return bad("input.content", "expected text or content part array")
				}
			}
		}
	}
	for _, tool := range req.Tools {
		if tool.Type == "function" {
			if strings.TrimSpace(tool.Name) == "" {
				return bad("tools.name", "function name required")
			}
			if len(tool.Parameters) > 0 {
				var schema map[string]json.RawMessage
				if json.Unmarshal(tool.Parameters, &schema) != nil || schema == nil {
					return bad("tools.parameters", "expected schema object")
				}
			}
		}
	}
	if raw, exists := fields["tool_choice"]; exists {
		var value any
		_ = json.Unmarshal(raw, &value)
		switch value.(type) {
		case string, map[string]any:
		default:
			return bad("tool_choice", "expected string or object")
		}
	}
	if strings.TrimSpace(frozenModel) == "" {
		return no("model", "missing frozen model")
	}
	if protocol == chatBridgeResponses {
		if !route.SupportsResponses {
			return no("protocol", "endpoint lacks Responses capability")
		}
		if route.LegacyCompact && !route.SupportsLegacyCompact {
			return no("route", "legacy compact unsupported")
		}
		if (route.NativeV2 || service.HasCompactionTriggerInInput(originalBody)) && !route.SupportsNativeV2 {
			return no("route", "native compaction unsupported")
		}
		if previous != "" && !route.HTTPContinuationSupported {
			return no("previous_response_id", "HTTP continuation unsupported")
		}
		return ok
	}
	if protocol != chatBridgeAnthropic {
		return no("protocol", "Responses conversion to target is not established")
	}
	if route.LegacyCompact || route.NativeV2 || service.HasCompactionTriggerInInput(originalBody) {
		return no("route", "compaction cannot be translated")
	}
	if previous != "" {
		return no("previous_response_id", "continuation cannot be translated")
	}
	if chatBridgeUnknown(fields, "model input instructions max_output_tokens temperature top_p stream tools tool_choice reasoning") != "" {
		return no("extension", "field not preserved by target")
	}
	if raw, exists := fields["reasoning"]; exists {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(raw, &m)
		if chatBridgeUnknown(m, "effort") != "" {
			return no("reasoning", "reasoning extension not preserved")
		}
		if req.Reasoning != nil && req.Reasoning.Effort != "low" && req.Reasoning.Effort != "medium" && req.Reasoning.Effort != "high" {
			return no("reasoning.effort", "effort equivalence not established")
		}
	}
	for _, item := range rawItems {
		if chatBridgeUnknown(item, "type role content") != "" {
			return no("input.extension", "input extension not preserved")
		}
	}
	expectedSystem := req.Instructions
	expectedMessages := make([]apicompat.ResponsesInputItem, 0, len(items))
	for i, item := range items {
		if item.Type != "" && item.Type != "message" {
			return no("input.type", "non-message input unsupported")
		}
		exact, valid := responsesBridgeText(item.Content)
		if !valid {
			return no("input.content", "only exact single text part supported")
		}
		if item.Role == "system" || item.Role == "developer" {
			if i != 0 || expectedSystem != "" {
				return no("instructions", "multiple or non-leading instruction sources")
			}
			expectedSystem = exact
			continue
		}
		if item.Role != "user" && item.Role != "assistant" {
			return no("input.role", "role unsupported")
		}
		expectedMessages = append(expectedMessages, item)
	}
	var rawTools []map[string]json.RawMessage
	_ = json.Unmarshal(fields["tools"], &rawTools)
	for _, tool := range rawTools {
		if chatBridgeUnknown(tool, "type name description parameters") != "" {
			return no("tools.extension", "tool extension or strict not preserved")
		}
	}
	expectedChoice := map[string]string{}
	if raw, exists := fields["tool_choice"]; exists {
		var v any
		_ = json.Unmarshal(raw, &v)
		switch choice := v.(type) {
		case string:
			if choice != "auto" && choice != "none" && choice != "required" {
				return no("tool_choice", "choice unsupported")
			}
			if choice == "required" {
				choice = "any"
			}
			expectedChoice["type"] = choice
		case map[string]any:
			if len(choice) != 2 || choice["type"] != "function" {
				return no("tool_choice", "choice extension unsupported")
			}
			name, valid := choice["name"].(string)
			if !valid || name == "" {
				return bad("tool_choice.name", "nonempty string required")
			}
			expectedChoice["type"] = "tool"
			expectedChoice["name"] = name
		}
	}
	// Conversion uses an independent decoded value and the frozen target model.
	req.Model = frozenModel
	target, err := apicompat.ResponsesToAnthropicRequest(&req)
	if err != nil {
		return no("conversion", "target cannot preserve request")
	}
	actualSystem := ""
	if len(target.System) > 0 {
		var valid bool
		actualSystem, valid = chatBridgeExactText(target.System)
		if !valid {
			return no("instructions", "system structure changed")
		}
	}
	if actualSystem != expectedSystem {
		return no("instructions", "system text changed")
	}
	if len(target.Messages) != len(expectedMessages) {
		return no("input", "message count changed")
	}
	for i, item := range expectedMessages {
		want, _ := responsesBridgeText(item.Content)
		got, valid := chatBridgeExactText(target.Messages[i].Content)
		if !valid || item.Role != target.Messages[i].Role || want != got {
			return no("input", "message role text or order changed")
		}
	}
	if req.MaxOutputTokens != nil && target.MaxTokens != *req.MaxOutputTokens {
		return no("max_output_tokens", "token limit changed")
	}
	if !reflect.DeepEqual(req.Temperature, target.Temperature) || !reflect.DeepEqual(req.TopP, target.TopP) {
		return no("sampling", "sampling changed")
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" && (target.OutputConfig == nil || target.OutputConfig.Effort != req.Reasoning.Effort) {
		return no("reasoning.effort", "effort changed")
	}
	if len(expectedChoice) > 0 {
		encoded, _ := json.Marshal(expectedChoice)
		if !chatBridgeJSONEqual(encoded, target.ToolChoice) {
			return no("tool_choice", "choice semantics changed")
		}
	}
	if len(target.Tools) != len(req.Tools) {
		return no("tools", "tool set changed")
	}
	for i, tool := range req.Tools {
		actual := target.Tools[i]
		if tool.Type != "function" || tool.Name != actual.Name || tool.Description != actual.Description || !chatBridgeJSONEqual(tool.Parameters, actual.InputSchema) {
			return no("tools", "function definition changed")
		}
	}
	return ok
}

// Check raw part keys before using the shared exact-text extractor.
func responsesBridgeText(raw json.RawMessage) (string, bool) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || len(parts) != 1 || chatBridgeUnknown(parts[0], "type text") != "" {
		return "", false
	}
	return chatBridgeExactText(raw)
}
