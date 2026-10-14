package service

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const ArtexGuardVersion = "artex-e6ec569-core-v1"
const ArtexGuardBlockedCode = "artex_client_blocked"

type ArtexProtocol string

const (
	ArtexAnthropic ArtexProtocol = "anthropic"
	ArtexChat      ArtexProtocol = "chat_completions"
	ArtexResponses ArtexProtocol = "responses"
)

type ArtexMatch struct {
	RuleID  string
	Version string
}

type artexSchemaField struct{ name, kind, itemKind string }
type artexToolSignature struct {
	fields   []artexSchemaField
	required []string
}

// Source-bound to ARTEX e6ec569 agent/tools.go, not a generic security classifier.
// Batch item objects in this source have only type:object. The single-item fields
// are ROOT properties; never combine fields from separate declarations.
var artexTools = map[string]artexToolSignature{
	"report_finding":   {[]artexSchemaField{{"vulnclass", "string", ""}, {"severity", "string", ""}, {"summary", "string", ""}, {"evidence", "string", ""}, {"intent_id", "integer", ""}, {"asset_ids", "array", "integer"}}, []string{"vulnclass", "severity", "summary"}},
	"record_fact":      {[]artexSchemaField{{"facts", "array", "object"}, {"summary", "string", ""}, {"detail", "string", ""}, {"evidence", "string", ""}, {"confidence", "string", ""}, {"intent_id", "integer", ""}, {"asset_ids", "array", "integer"}}, nil},
	"get_worker_trace": {[]artexSchemaField{{"intent_id", "integer", ""}, {"q", "string", ""}, {"step_ids", "array", "integer"}, {"limit", "integer", ""}}, []string{"intent_id"}},
	"add_intent":       {[]artexSchemaField{{"intents", "array", "object"}, {"summary", "string", ""}, {"asset_ids", "array", "integer"}, {"parent_ids", "array", "integer"}, {"priority", "integer", ""}}, nil},
	"prove_goal":       {[]artexSchemaField{{"goal_id", "integer", ""}, {"evidence_id", "integer", ""}, {"reason", "string", ""}}, []string{"goal_id", "evidence_id"}},
	"set_goals":        {[]artexSchemaField{{"goals", "array", "object"}, {"text", "string", ""}, {"vulnclass", "string", ""}}, nil},
}

const artexArtifactPrefix = "**中间产物输出规约**：脚本、payload、抓到的响应体、临时数据等一切中间产物，**一律写到"
const artexWorkerPrefix = artexArtifactPrefix + "本次意图的专属工作目录 "
const artexTaskPrefix = artexArtifactPrefix + "本任务工作目录 "
const artexWorkerSuffix = "**（已自动建好，直接用相对路径写在这里即可，无需再手动建目录）——**不要写 /tmp、不要用其它绝对路径**。"
const artexTaskSuffix = "**（相对路径即写在这里，也可用该绝对路径）——**不要写 /tmp、不要用其它绝对路径**。"

// DetectArtexCore examines only legal effective instruction text and actual tool
// declarations. No recursive scan, brand/header/intent signal or fuzzy fallback.
func DetectArtexCore(protocol ArtexProtocol, body []byte) (ArtexMatch, bool) {
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil || request == nil {
		return ArtexMatch{}, false
	}
	instruction := artexInstructions(protocol, request)
	worker := artexWholeTail(instruction, artexWorkerPrefix, artexWorkerSuffix)
	task := artexWholeTail(instruction, artexTaskPrefix, artexTaskSuffix)
	if !worker && !task {
		return ArtexMatch{}, false
	}
	var declarations []map[string]json.RawMessage
	if json.Unmarshal(request["tools"], &declarations) != nil {
		return ArtexMatch{}, false
	}
	matched := map[string]bool{}
	for _, decl := range declarations {
		tool := decl
		schemaField := "input_schema"
		if protocol != ArtexAnthropic {
			if artexString(decl["type"]) != "function" {
				continue
			}
			schemaField = "parameters"
			if protocol == ArtexChat {
				tool = nil
				if json.Unmarshal(decl["function"], &tool) != nil || tool == nil {
					continue
				}
			}
		}
		name := artexString(tool["name"])
		signature, known := artexTools[name]
		if known && artexSchemaMatches(tool[schemaField], signature) {
			matched[name] = true
		}
	}
	rule := ""
	if worker && matched["report_finding"] && matched["record_fact"] && matched["get_worker_trace"] {
		rule = "worker"
	}
	if task && matched["add_intent"] && matched["prove_goal"] && matched["get_worker_trace"] {
		rule = "planner"
	}
	if rule == "" && task && matched["add_intent"] && matched["set_goals"] && matched["get_worker_trace"] {
		rule = "main"
	}
	if rule == "" {
		return ArtexMatch{}, false
	}
	return ArtexMatch{RuleID: rule, Version: ArtexGuardVersion}, true
}

func artexInstructions(protocol ArtexProtocol, request map[string]json.RawMessage) string {
	var texts []string
	switch protocol {
	case ArtexAnthropic:
		texts = append(texts, artexText(request["system"], false))
	case ArtexChat, ArtexResponses:
		field := "messages"
		if protocol == ArtexResponses {
			texts = append(texts, artexString(request["instructions"]))
			field = "input"
		}
		var messages []map[string]json.RawMessage
		if json.Unmarshal(request[field], &messages) == nil {
			for _, message := range messages {
				role := artexString(message["role"])
				if role != "system" && role != "developer" {
					continue
				}
				if protocol == ArtexResponses {
					kind := artexString(message["type"])
					if kind != "" && kind != "message" {
						continue
					}
				}
				texts = append(texts, artexText(message["content"], protocol == ArtexResponses))
			}
		}
	default:
		return ""
	}
	return strings.ReplaceAll(strings.Join(texts, "\n"), "\r\n", "\n")
}

func artexString(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}
func artexText(raw json.RawMessage, responses bool) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var texts []string
	blockKind := "text"
	if responses {
		blockKind = "input_text"
	}
	for _, block := range blocks {
		kind := artexString(block["type"])
		if kind == blockKind {
			texts = append(texts, artexString(block["text"]))
		}
	}
	return strings.Join(texts, "\n")
}
func artexWholeTail(text, prefix, suffix string) bool {
	for {
		start := strings.Index(text, prefix)
		if start < 0 {
			return false
		}
		text = text[start+len(prefix):]
		end := strings.Index(text, suffix)
		if end < 0 {
			// No later prefix can have a suffix if the entire remainder has none.
			// Avoid repeatedly scanning the same suffix-less remaining text.
			return false
		}
		if end > 0 {
			return true
		} // only the directory slot is variable
	}
}
func artexSchemaMatches(raw json.RawMessage, signature artexToolSignature) bool {
	// JSON Schema keywords are case-sensitive. A tagged Go struct would
	// silently accept TYPE/Properties/Items via encoding/json case folding.
	// Exact map indexes also prevent an uppercase extra from overriding the
	// real lowercase keyword, while still allowing inert extension members.
	var schema map[string]json.RawMessage
	if json.Unmarshal(raw, &schema) != nil || artexString(schema["type"]) != "object" {
		return false
	}
	var properties map[string]json.RawMessage
	if json.Unmarshal(schema["properties"], &properties) != nil {
		return false
	}
	var requiredSet []string
	if value, provided := schema["required"]; provided && json.Unmarshal(value, &requiredSet) != nil {
		return false
	}
	for _, field := range signature.fields {
		var prop map[string]json.RawMessage
		if json.Unmarshal(properties[field.name], &prop) != nil || artexString(prop["type"]) != field.kind {
			return false
		}
		if field.itemKind != "" {
			var items map[string]json.RawMessage
			if json.Unmarshal(prop["items"], &items) != nil || artexString(items["type"]) != field.itemKind {
				return false
			}
		}
	}
	for _, required := range signature.required {
		found := false
		for _, actual := range requiredSet {
			if actual == required {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// EvaluateArtexGuard is the sole decision/logging function for HTTP and WS.
// It emits only allowlisted metadata for a strong source-bound match.
func EvaluateArtexGuard(mode string, protocol ArtexProtocol, body []byte) bool {
	if mode == config.ArtexGuardOff {
		return false
	}
	match, found := DetectArtexCore(protocol, body)
	if !found {
		return false
	}
	decision := "block"
	if mode == config.ArtexGuardMonitor {
		decision = "monitor"
	}
	slog.Info("artex_guard_decision", "rule_id", match.RuleID, "rule_version", match.Version, "protocol", string(protocol), "mode", mode, "decision", decision)
	return mode != config.ArtexGuardMonitor
}
