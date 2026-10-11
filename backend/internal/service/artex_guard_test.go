package service

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Independent, source-derived fixtures: ARTEX e6ec569 worker.go:261,268;
// tools.go:1094,1159,1220,1361,1479,1828. Never run the tool.
const artexFixtureWorkerTail = "\n\n**中间产物输出规约**：脚本、payload、抓到的响应体、临时数据等一切中间产物，**一律写到本次意图的专属工作目录 /any/deployment/SECRET_DIR**（已自动建好，直接用相对路径写在这里即可，无需再手动建目录）——**不要写 /tmp、不要用其它绝对路径**。"
const artexFixtureTaskTail = "\n\n**中间产物输出规约**：脚本、payload、抓到的响应体、临时数据等一切中间产物，**一律写到本任务工作目录 relative-any-dir**（相对路径即写在这里，也可用该绝对路径）——**不要写 /tmp、不要用其它绝对路径**。"

var artexFixtureSchemas = map[string]string{
	"report_finding":   `{"type":"object","properties":{"vulnclass":{"type":"string"},"name":{"type":"string"},"severity":{"type":"string"},"summary":{"type":"string"},"intent_id":{"type":"integer"},"asset_ids":{"type":"array","items":{"type":"integer"}},"evidence":{"type":"string"},"evidence_hint_id":{"type":"integer"}},"required":["summary","severity","vulnclass"]}`,
	"record_fact":      `{"type":"object","properties":{"facts":{"type":"array","items":{"type":"object"}},"summary":{"type":"string"},"intent_id":{"type":"integer"},"detail":{"type":"string"},"evidence":{"type":"string"},"confidence":{"type":"string"},"asset_ids":{"type":"array","items":{"type":"integer"}}}}`,
	"get_worker_trace": `{"type":"object","properties":{"intent_id":{"type":"integer"},"q":{"type":"string"},"step_ids":{"type":"array","items":{"type":"integer"}},"limit":{"type":"integer"}},"required":["intent_id"]}`,
	"add_intent":       `{"type":"object","properties":{"intents":{"type":"array","items":{"type":"object"}},"summary":{"type":"string"},"asset_ids":{"type":"array","items":{"type":"integer"}},"parent_ids":{"type":"array","items":{"type":"integer"}},"priority":{"type":"integer"}}}`,
	"prove_goal":       `{"type":"object","properties":{"goal_id":{"type":"integer"},"evidence_id":{"type":"integer"},"reason":{"type":"string"}},"required":["evidence_id","goal_id"]}`,
	"set_goals":        `{"type":"object","properties":{"goals":{"type":"array","items":{"type":"object"}},"text":{"type":"string"},"vulnclass":{"type":"string"}}}`,
}

func artexFixtureRequest(t *testing.T, p ArtexProtocol, family string) map[string]any {
	t.Helper()
	names := map[string][]string{"worker": {"get_worker_trace", "record_fact", "report_finding"}, "planner": {"get_worker_trace", "prove_goal", "add_intent"}, "main": {"set_goals", "get_worker_trace", "add_intent"}}[family]
	tools := []any{}
	for _, name := range names {
		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(artexFixtureSchemas[name]), &schema))
		tool := map[string]any{"name": name, "parameters": schema, "description": "editable SECRET_SCHEMA"}
		switch p {
		case ArtexAnthropic:
			delete(tool, "parameters")
			tool["input_schema"] = schema
		case ArtexChat:
			tool = map[string]any{"type": "function", "function": tool}
		case ArtexResponses:
			tool["type"] = "function"
		}
		tools = append(tools, tool)
	}
	request := map[string]any{"model": "gpt-5.1", "tools": tools, "stream": true}
	tail := artexFixtureTaskTail
	if family == "worker" {
		tail = artexFixtureWorkerTail
	}
	artexFixtureSetInstruction(request, p, "custom mutable BODY"+tail+"\nconstraints follow; tail is not at end")
	return request
}
func artexFixtureSetInstruction(request map[string]any, p ArtexProtocol, text string) {
	switch p {
	case ArtexAnthropic:
		request["system"] = text
	case ArtexChat:
		request["messages"] = []any{map[string]any{"role": "developer", "content": text}}
	case ArtexResponses:
		request["instructions"] = text
	}
}
func artexFixtureMarshal(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	return body
}
func artexFixtureTool(tool any, p ArtexProtocol) map[string]any {
	decl := tool.(map[string]any)
	if p == ArtexChat {
		return decl["function"].(map[string]any)
	}
	return decl
}
func artexFixtureSchema(tool any, p ArtexProtocol) map[string]any {
	field := "parameters"
	if p == ArtexAnthropic {
		field = "input_schema"
	}
	return artexFixtureTool(tool, p)[field].(map[string]any)
}

func TestArtexCoreNineSourceBoundFamilies(t *testing.T) {
	for _, p := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		for _, family := range []string{"worker", "planner", "main"} {
			t.Run(string(p)+"/"+family, func(t *testing.T) {
				request := artexFixtureRequest(t, p, family)
				match, found := DetectArtexCore(p, artexFixtureMarshal(t, request))
				require.True(t, found)
				require.Equal(t, ArtexMatch{RuleID: family, Version: ArtexGuardVersion}, match)
				tail := artexFixtureTaskTail
				if family == "worker" {
					tail = artexFixtureWorkerTail
				}
				artexFixtureSetInstruction(request, p, "entirely custom BODY\r\n"+strings.ReplaceAll(tail, "\n", "\r\n")+strings.Repeat("after-tail constraints ", 100))
				for _, tool := range request["tools"].([]any) {
					schema := artexFixtureSchema(tool, p)
					schema["description"] = "edited"
					schema["default"] = map[string]any{}
					schema["properties"].(map[string]any)["extra"] = map[string]any{"type": "boolean"}
					schema["required"] = append([]any{"extra"}, func() []any {
						if r, ok := schema["required"].([]any); ok {
							return r
						}
						return nil
					}()...)
				}
				_, found = DetectArtexCore(p, artexFixtureMarshal(t, request))
				require.True(t, found, "CRLF/BODY/descriptions/extra fields/required order/no traffic do not change source fingerprint")
			})
		}
	}
}

func TestArtexCoreNormalAndNearMatchNegatives(t *testing.T) {
	for _, p := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		for _, name := range []string{"chat", "code", "authorized_security", "CTF", "README_brand", "user_quote_full", "tool_result_full", "metadata_full", "normal_system_full_tools", "full_tail_no_tools", "short_title", "truncated_tail", "missing_tool", "case_name", "wrong_type", "wrong_item_type", "root_not_object", "missing_required", "split_same_name", "quoted_json", "batch_not_root"} {
			t.Run(string(p)+"/"+name, func(t *testing.T) {
				request := artexFixtureRequest(t, p, "worker")
				switch name {
				case "chat", "code", "authorized_security", "CTF", "README_brand":
					artexFixtureSetInstruction(request, p, "ARTEX README; authorized security analysis, pentest PoC vulnerability CTF coding Bash Read Skill MCP; writes /tmp")
					delete(request, "tools")
				case "normal_system_full_tools":
					artexFixtureSetInstruction(request, p, "help with normal documentation")
				case "full_tail_no_tools":
					delete(request, "tools")
				case "short_title":
					artexFixtureSetInstruction(request, p, "**中间产物输出规约**")
				case "truncated_tail":
					artexFixtureSetInstruction(request, p, strings.ReplaceAll(artexFixtureWorkerTail, "不要用其它绝对路径", "avoid other paths"))
				case "user_quote_full", "tool_result_full", "metadata_full", "quoted_json":
					saved := artexFixtureMarshal(t, request)
					artexFixtureSetInstruction(request, p, "ordinary assistant")
					delete(request, "tools")
					if name == "metadata_full" {
						request["metadata"] = map[string]any{"system": artexFixtureWorkerTail, "tools": string(saved)}
					} else {
						role := "user"
						if name == "tool_result_full" {
							role = "tool"
						}
						messages := []any{map[string]any{"role": role, "content": string(saved)}}
						request["messages"] = messages
						request["input"] = messages
						if name == "quoted_json" {
							artexFixtureSetInstruction(request, p, "Quoted source sample: "+string(saved))
						}
					}
				case "missing_tool":
					request["tools"] = request["tools"].([]any)[:2]
				case "case_name":
					artexFixtureTool(request["tools"].([]any)[0], p)["name"] = "Get_Worker_Trace"
				case "wrong_type":
					artexFixtureSchema(request["tools"].([]any)[0], p)["properties"].(map[string]any)["intent_id"] = map[string]any{"type": "number"}
				case "wrong_item_type":
					artexFixtureSchema(request["tools"].([]any)[0], p)["properties"].(map[string]any)["step_ids"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
				case "root_not_object":
					artexFixtureSchema(request["tools"].([]any)[0], p)["type"] = "array"
				case "missing_required":
					artexFixtureSchema(request["tools"].([]any)[0], p)["required"] = []any{}
				case "split_same_name":
					tools := request["tools"].([]any)
					clonedBytes := artexFixtureMarshal(t, tools[0])
					var other map[string]any
					require.NoError(t, json.Unmarshal(clonedBytes, &other))
					delete(artexFixtureSchema(tools[0], p)["properties"].(map[string]any), "q")
					delete(artexFixtureSchema(other, p)["properties"].(map[string]any), "limit")
					request["tools"] = append(tools, other)
				case "batch_not_root":
					schema := artexFixtureSchema(request["tools"].([]any)[1], p)
					props := schema["properties"].(map[string]any)
					nested := map[string]any{}
					for key, value := range props {
						if key != "facts" {
							nested[key] = value
							delete(props, key)
						}
					}
					props["facts"].(map[string]any)["items"] = map[string]any{"type": "object", "properties": nested}
				}
				_, found := DetectArtexCore(p, artexFixtureMarshal(t, request))
				require.False(t, found)
			})
		}
	}
}

func TestArtexCoreLegalTextBlocksOnly(t *testing.T) {
	request := artexFixtureRequest(t, ArtexAnthropic, "worker")
	request["system"] = []any{map[string]any{"type": "text", "text": artexFixtureWorkerTail}}
	_, found := DetectArtexCore(ArtexAnthropic, artexFixtureMarshal(t, request))
	require.True(t, found)
	request["system"] = []any{map[string]any{"type": "image", "text": artexFixtureWorkerTail}}
	_, found = DetectArtexCore(ArtexAnthropic, artexFixtureMarshal(t, request))
	require.False(t, found)
	request = artexFixtureRequest(t, ArtexResponses, "planner")
	delete(request, "instructions")
	request["input"] = []any{map[string]any{"type": "message", "role": "system", "content": []any{map[string]any{"type": "input_text", "text": artexFixtureTaskTail}}}}
	_, found = DetectArtexCore(ArtexResponses, artexFixtureMarshal(t, request))
	require.True(t, found)
	request["input"].([]any)[0].(map[string]any)["type"] = "function_call_output"
	_, found = DetectArtexCore(ArtexResponses, artexFixtureMarshal(t, request))
	require.False(t, found)
	for _, raw := range []string{"not-json", "null", "[]", "{}"} {
		_, found = DetectArtexCore(ArtexResponses, []byte(raw))
		require.False(t, found)
	}
}

func TestArtexCoreIgnoresWrongProtocolInstructionFields(t *testing.T) {
	for _, p := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		request := artexFixtureRequest(t, p, "worker")
		artexFixtureSetInstruction(request, p, "ordinary assistant")
		switch p {
		case ArtexAnthropic:
			request["instructions"] = artexFixtureWorkerTail
			request["messages"] = []any{map[string]any{"role": "system", "content": artexFixtureWorkerTail}}
		case ArtexChat:
			request["system"] = artexFixtureWorkerTail
			request["instructions"] = artexFixtureWorkerTail
		case ArtexResponses:
			request["system"] = artexFixtureWorkerTail
			request["messages"] = []any{map[string]any{"role": "system", "content": artexFixtureWorkerTail}}
		}
		_, found := DetectArtexCore(p, artexFixtureMarshal(t, request))
		require.False(t, found)
	}
	body := artexFixtureMarshal(t, artexFixtureRequest(t, ArtexResponses, "worker"))
	escaped := strings.ReplaceAll(string(body), "中", "\\u4e2d")
	_, found := DetectArtexCore(ArtexResponses, []byte(escaped))
	require.True(t, found, "legal JSON Unicode escapes have the same effective text")
}

func TestArtexGuardDecisionModesAndMetadataOnlyLogs(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	body := artexFixtureMarshal(t, artexFixtureRequest(t, ArtexResponses, "worker"))
	require.True(t, EvaluateArtexGuard(config.ArtexGuardEnforce, ArtexResponses, body))
	require.False(t, EvaluateArtexGuard(config.ArtexGuardMonitor, ArtexResponses, body))
	before := logs.Len()
	require.False(t, EvaluateArtexGuard(config.ArtexGuardOff, ArtexResponses, body))
	require.Equal(t, before, logs.Len())
	text := logs.String()
	require.Contains(t, text, "artex_guard_decision")
	require.Contains(t, text, ArtexGuardVersion)
	for _, secret := range []string{"SECRET_DIR", "SECRET_SCHEMA", "/any/deployment", "custom mutable BODY", "report_finding", "API_KEY"} {
		require.NotContains(t, text, secret)
	}
}
