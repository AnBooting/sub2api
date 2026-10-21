package service

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	decl, ok := tool.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("artex fixture tool is not an object: %T", tool))
	}
	if p == ArtexChat {
		fn, ok := decl["function"].(map[string]any)
		if !ok {
			panic("artex fixture tool has no function object")
		}
		return fn
	}
	return decl
}

// artexFixtureObject 是带检查的类型断言：夹具结构不合法时立刻失败，
// 而不是让未检查的断言把 panic 丢到断言之外。
func artexFixtureObject(value any) map[string]any {
	obj, ok := value.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("artex fixture value is not an object: %T", value))
	}
	return obj
}

// artexFixtureList 是带检查的 []any 断言。
func artexFixtureList(value any) []any {
	list, ok := value.([]any)
	if !ok {
		panic(fmt.Sprintf("artex fixture value is not a list: %T", value))
	}
	return list
}

func artexFixtureSchema(tool any, p ArtexProtocol) map[string]any {
	field := "parameters"
	if p == ArtexAnthropic {
		field = "input_schema"
	}
	decl, ok := artexFixtureTool(tool, p)[field].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("artex fixture tool has no %s object", field))
	}
	return decl
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
				for _, tool := range artexFixtureList(request["tools"]) {
					schema := artexFixtureSchema(tool, p)
					schema["description"] = "edited"
					schema["default"] = map[string]any{}
					artexFixtureObject(schema["properties"])["extra"] = map[string]any{"type": "boolean"}
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
					request["tools"] = artexFixtureList(request["tools"])[:2]
				case "case_name":
					artexFixtureTool(artexFixtureList(request["tools"])[0], p)["name"] = "Get_Worker_Trace"
				case "wrong_type":
					artexFixtureObject(artexFixtureSchema(artexFixtureList(request["tools"])[0], p)["properties"])["intent_id"] = map[string]any{"type": "number"}
				case "wrong_item_type":
					artexFixtureObject(artexFixtureSchema(artexFixtureList(request["tools"])[0], p)["properties"])["step_ids"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
				case "root_not_object":
					artexFixtureSchema(artexFixtureList(request["tools"])[0], p)["type"] = "array"
				case "missing_required":
					artexFixtureSchema(artexFixtureList(request["tools"])[0], p)["required"] = []any{}
				case "split_same_name":
					tools := artexFixtureList(request["tools"])
					clonedBytes := artexFixtureMarshal(t, tools[0])
					var other map[string]any
					require.NoError(t, json.Unmarshal(clonedBytes, &other))
					delete(artexFixtureObject(artexFixtureSchema(tools[0], p)["properties"]), "q")
					delete(artexFixtureObject(artexFixtureSchema(other, p)["properties"]), "limit")
					request["tools"] = append(tools, other)
				case "batch_not_root":
					schema := artexFixtureSchema(artexFixtureList(request["tools"])[1], p)
					props := artexFixtureObject(schema["properties"])
					nested := map[string]any{}
					for key, value := range props {
						if key != "facts" {
							nested[key] = value
							delete(props, key)
						}
					}
					artexFixtureObject(props["facts"])["items"] = map[string]any{"type": "object", "properties": nested}
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
	artexFixtureObject(artexFixtureList(request["input"])[0])["type"] = "function_call_output"
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

func TestArtexSchemaKeywordsRequireExactCanonicalCase(t *testing.T) {
	// Mutations originate in independent literal fixtures, not production tables.
	cases := []struct {
		name, level, keyword, replacement string
		expected                          bool
	}{
		{"root_TYPE", "root", "type", "TYPE", false}, {"root_Type", "root", "type", "Type", false}, {"root_tYpE", "root", "type", "tYpE", false},
		{"root_PROPERTIES", "root", "properties", "PROPERTIES", false}, {"root_Properties", "root", "properties", "Properties", false}, {"root_pRoPeRtIeS", "root", "properties", "pRoPeRtIeS", false},
		{"root_REQUIRED", "root", "required", "REQUIRED", false}, {"root_Required", "root", "required", "Required", false}, {"root_rEqUiReD", "root", "required", "rEqUiReD", false},
		{"prop_TYPE", "prop", "type", "TYPE", false}, {"prop_Type", "prop", "type", "Type", false}, {"prop_tYpE", "prop", "type", "tYpE", false},
		{"items_ITEMS", "array", "items", "ITEMS", false}, {"items_Items", "array", "items", "Items", false}, {"items_iTeMs", "array", "items", "iTeMs", false},
		{"item_TYPE", "item", "type", "TYPE", false}, {"item_Type", "item", "type", "Type", false}, {"item_tYpE", "item", "type", "tYpE", false},
		{"wrong_root_type_value", "value", "", "", false}, {"wrong_prop_type_value", "value", "", "", false},
		{"lower_root_wrong_upper_correct", "shadow", "", "", false}, {"lower_prop_wrong_upper_correct", "shadow", "", "", false},
		{"canonical_valid_inert_uppercase_extra", "extra", "", "", true},
	}
	for _, protocol := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		for _, tc := range cases {
			t.Run(string(protocol)+"/"+tc.name, func(t *testing.T) {
				request := artexFixtureRequest(t, protocol, "worker")
				schema := artexFixtureSchema(artexFixtureList(request["tools"])[0], protocol) // trace has original required
				props := artexFixtureObject(schema["properties"])
				intent := artexFixtureObject(props["intent_id"])
				array := artexFixtureObject(props["step_ids"])
				item := artexFixtureObject(array["items"])
				switch tc.level {
				case "root", "prop", "array", "item":
					target := map[string]map[string]any{"root": schema, "prop": intent, "array": array, "item": item}[tc.level]
					target[tc.replacement] = target[tc.keyword]
					delete(target, tc.keyword)
				case "value":
					if tc.name == "wrong_root_type_value" {
						schema["type"] = "Object"
					} else {
						intent["type"] = "Integer"
					}
				case "shadow":
					if tc.name == "lower_root_wrong_upper_correct" {
						schema["type"] = "array"
						schema["TYPE"] = "object"
					} else {
						intent["type"] = "string"
						intent["TYPE"] = "integer"
					}
				case "extra":
					schema["TYPE"] = "array"
					schema["PROPERTIES"] = "inert"
					schema["REQUIRED"] = "inert"
					intent["TYPE"] = "number"
					array["ITEMS"] = "inert"
					item["TYPE"] = "string"
				}
				_, found := DetectArtexCore(protocol, artexFixtureMarshal(t, request))
				require.Equal(t, tc.expected, found)
			})
		}
	}
}

func TestArtexSchemaUppercaseMustNeverOverrideCanonicalLowercase(t *testing.T) {
	// A raw member order proves that a later case-insensitive lookalike cannot
	// fix an invalid canonical value, and an extra cannot poison a valid one.
	for _, tc := range []struct {
		raw      string
		expected bool
	}{
		{`{"type":"array","TYPE":"object","properties":{"goal_id":{"type":"integer"},"evidence_id":{"type":"integer"},"reason":{"type":"string"}},"required":["goal_id","evidence_id"]}`, false},
		{`{"type":"object","TYPE":"array","properties":{"goal_id":{"type":"integer"},"evidence_id":{"type":"integer"},"reason":{"type":"string"}},"required":["goal_id","evidence_id"]}`, true},
	} {
		require.Equal(t, tc.expected, artexSchemaMatches([]byte(tc.raw), artexTools["prove_goal"]))
	}
}

func TestArtexInstructionBlocksStayProtocolSpecificWithFullTools(t *testing.T) {
	for _, protocol := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		for _, family := range []string{"worker", "planner", "main"} {
			for _, right := range []bool{false, true} {
				t.Run(string(protocol)+"/"+family+map[bool]string{true: "/right", false: "/wrong"}[right], func(t *testing.T) {
					request := artexFixtureRequest(t, protocol, family)
					require.Len(t, request["tools"], 3) // keep actual declarations, not a missing-tools negative
					tail := artexFixtureTaskTail
					if family == "worker" {
						tail = artexFixtureWorkerTail
					}
					blockKind := "text"
					if (protocol == ArtexResponses) == right {
						blockKind = "input_text"
					}
					blocks := []any{map[string]any{"type": blockKind, "text": tail}}
					switch protocol {
					case ArtexAnthropic:
						request["system"] = blocks
					case ArtexChat:
						request["messages"] = []any{map[string]any{"role": "developer", "content": blocks}}
					case ArtexResponses:
						delete(request, "instructions")
						request["input"] = []any{map[string]any{"type": "message", "role": "system", "content": blocks}}
					}
					_, found := DetectArtexCore(protocol, artexFixtureMarshal(t, request))
					require.Equal(t, right, found)
				})
			}
		}
	}
}

func TestArtexNonInstructionTailNeverMatchesDespiteFullToolDeclarations(t *testing.T) {
	for _, protocol := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		for _, place := range []string{"user", "assistant", "tool_result", "metadata"} {
			t.Run(string(protocol)+"/"+place, func(t *testing.T) {
				request := artexFixtureRequest(t, protocol, "worker")
				artexFixtureSetInstruction(request, protocol, "normal assistant")
				require.Len(t, request["tools"], 3)
				tail := artexFixtureWorkerTail
				if place == "metadata" {
					request["metadata"] = map[string]any{"reference": tail, "quoted_tools": string(artexFixtureMarshal(t, request["tools"]))}
				} else {
					switch protocol {
					case ArtexAnthropic:
						role := place
						if place == "tool_result" {
							role = "user"
						}
						block := map[string]any{"type": "text", "text": tail}
						if place == "tool_result" {
							block = map[string]any{"type": "tool_result", "tool_use_id": "fixture", "content": tail}
						}
						request["messages"] = []any{map[string]any{"role": role, "content": []any{block}}}
					case ArtexChat:
						message := map[string]any{"role": place, "content": []any{map[string]any{"type": "text", "text": tail}}}
						if place == "tool_result" {
							message = map[string]any{"role": "tool", "tool_call_id": "fixture", "content": tail}
						}
						request["messages"] = []any{map[string]any{"role": "system", "content": "normal assistant"}, message}
					case ArtexResponses:
						kind := "input_text"
						if place == "assistant" {
							kind = "output_text"
						}
						message := map[string]any{"type": "message", "role": place, "content": []any{map[string]any{"type": kind, "text": tail}}}
						if place == "tool_result" {
							message = map[string]any{"type": "function_call_output", "call_id": "fixture", "output": tail}
						}
						request["input"] = []any{message}
					}
				}
				_, found := DetectArtexCore(protocol, artexFixtureMarshal(t, request))
				require.False(t, found)
			})
		}
	}
}

func TestArtexQuotedToolJSONStringIsNeverAnActualDeclaration(t *testing.T) {
	for _, protocol := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
		for _, place := range []string{"string_array", "metadata", "schema_string"} {
			t.Run(string(protocol)+"/"+place, func(t *testing.T) {
				request := artexFixtureRequest(t, protocol, "worker") // effective full tail remains
				tools := artexFixtureList(request["tools"])
				saved := string(artexFixtureMarshal(t, tools))
				switch place {
				case "string_array":
					request["tools"] = []any{saved}
				case "metadata":
					delete(request, "tools")
					request["metadata"] = map[string]any{"tool_catalog": saved}
				case "schema_string":
					tool := artexFixtureTool(tools[0], protocol)
					field := "parameters"
					if protocol == ArtexAnthropic {
						field = "input_schema"
					}
					tool[field] = string(artexFixtureMarshal(t, tool[field])) // one schema is quoted source, not an object
				}
				_, found := DetectArtexCore(protocol, artexFixtureMarshal(t, request))
				require.False(t, found)
			})
		}
	}
}

func TestArtexWholeTailBoundedScanSemanticCases(t *testing.T) {
	// Source-derived fixed text, not a benchmark or a broader resource policy.
	const prefix = "**中间产物输出规约**：脚本、payload、抓到的响应体、临时数据等一切中间产物，**一律写到本次意图的专属工作目录 "
	const suffix = "**（已自动建好，直接用相对路径写在这里即可，无需再手动建目录）——**不要写 /tmp、不要用其它绝对路径**。"
	for _, tc := range []struct {
		name, text string
		expected   bool
	}{
		{"repeatPrefixNoSuffix", strings.Repeat(prefix, 3), false},
		{"emptySlotThenValidTail", prefix + suffix + "\n" + prefix + "valid-relative-dir" + suffix, true},
		{"firstValidDirectory", prefix + "valid-relative-dir" + suffix, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, artexWholeTail(tc.text, prefix, suffix))
			// Keep all three independent actual schemas intact for the protocol fixture.
			for _, protocol := range []ArtexProtocol{ArtexAnthropic, ArtexChat, ArtexResponses} {
				request := artexFixtureRequest(t, protocol, "worker")
				require.Len(t, request["tools"], 3)
				artexFixtureSetInstruction(request, protocol, tc.text)
				_, found := DetectArtexCore(protocol, artexFixtureMarshal(t, request))
				require.Equal(t, tc.expected, found)
			}
		})
	}
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
