package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// gatewayRequestThinkingPreference 是请求体的 thinking 偏好，必须在冻结账号级上游模型
// 之前写入 ctx。
//
// 为什么必须显式传递：Antigravity 的 /v1/messages 出站模型与 thinking 有关——
//   - claude-sonnet-4-5 在 thinking 开启时会被追加 -thinking 后缀；
//   - 裸 gemini-* 名会按档位（low/medium/high）选上游目录里的后缀变体。
//
// 冻结表只接收标量输入（Model/BodyBytes/...），拿不到请求体；若不把该偏好传进来，
// 冻结值会停留在"未开启思考"档，实际出站却是 thinking 变体，消费侧的等值核对就会
// 失败——用户拿到 200 响应但整笔零扣费，比提前拒绝更糟。
type gatewayRequestThinkingPreference struct {
	enabled bool
	// level 是 low/medium/high；空表示按网关默认（high）解析。
	level string
}

type gatewayRequestThinkingPreferenceCtxKey struct{}

// WithGatewayRequestThinkingPreference 把请求体推导出的 thinking 偏好挂到 ctx 上。
// 冻结流程在 prepareRequestBilling -> accountMappingSnapshot 的 ctx 参数上读取它。
func WithGatewayRequestThinkingPreference(ctx context.Context, enabled bool, level string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gatewayRequestThinkingPreferenceCtxKey{}, gatewayRequestThinkingPreference{
		enabled: enabled,
		level:   strings.ToLower(strings.TrimSpace(level)),
	})
}

// gatewayRequestThinkingPreferenceFromContext 返回已写入的偏好；未写入时按"未开启思考"
// 处理（与不携带 thinking 字段的请求一致）。
func gatewayRequestThinkingPreferenceFromContext(ctx context.Context) (bool, string) {
	if ctx == nil {
		return false, ""
	}
	preference, ok := ctx.Value(gatewayRequestThinkingPreferenceCtxKey{}).(gatewayRequestThinkingPreference)
	if !ok {
		return false, ""
	}
	return preference.enabled, preference.level
}

// ClaudeThinkingPreferenceFromBody 从 Claude Messages 请求体推导 (是否开启思考, 档位)。
//
// 两个口径都必须与 AntigravityGatewayService.Forward 完全同源：
//   - "是否开启"对应 thinking.type ∈ {enabled, adaptive}（出站侧同一判定）；
//   - "档位"阈值与 geminiThinkingLevelFromClaudeThinking 一致。
//
// 任一处不一致都会让冻结模型与实际出站模型不同，消费侧等值核对失败 → 200 但零计费。
func ClaudeThinkingPreferenceFromBody(body []byte) (bool, string) {
	if len(body) == 0 {
		return false, ""
	}
	thinking := gjson.GetBytes(body, "thinking")
	if !thinking.Exists() || thinking.Type == gjson.Null {
		return false, ""
	}
	thinkingType := strings.TrimSpace(thinking.Get("type").String())
	// 判定必须与出站侧完全一致（antigravity_gateway_claude.go 的 thinkingEnabled）：
	// 只认 enabled / adaptive。这里多认一种类型就会让冻结值带上 -thinking 后缀、实际
	// 出站却没有，消费侧等值核对失败——又回到"200 但零计费"。宁可少认，也不能错认。
	enabled := thinkingType == "enabled" || thinkingType == "adaptive"
	return enabled, claudeThinkingLevelFromBudget(thinkingType, thinking.Get("budget_tokens"))
}

// ChatCompletionsThinkingPreferenceFromBody 从 Chat Completions 原始请求体推导 thinking 偏好。
//
// 走的是与出站**完全相同的转换链**（ChatCompletionsToResponses → ResponsesToAnthropicRequest），
// 因此档位口径与 antigravity_gateway_compat.go:215-227 逐字同源；自己另写一套
// reasoning_effort→档位 的映射迟早会与转换链分叉，重新引入 200 但零计费。
func ChatCompletionsThinkingPreferenceFromBody(body []byte) (bool, string) {
	var request apicompat.ChatCompletionsRequest
	if json.Unmarshal(body, &request) != nil {
		return false, ""
	}
	responsesRequest, err := apicompat.ChatCompletionsToResponses(&request)
	if err != nil {
		return false, ""
	}
	return claudeThinkingPreferenceFromResponsesRequest(responsesRequest)
}

// ResponsesThinkingPreferenceFromBody 从 Responses 原始请求体推导 thinking 偏好（同一转换链）。
func ResponsesThinkingPreferenceFromBody(body []byte) (bool, string) {
	var request apicompat.ResponsesRequest
	if json.Unmarshal(body, &request) != nil {
		return false, ""
	}
	return claudeThinkingPreferenceFromResponsesRequest(&request)
}

// claudeThinkingPreferenceFromResponsesRequest 把 Responses 请求转换为 Claude 协议后推导偏好；
// 转换失败时返回"未开启思考"（此时出站也会因转换失败而不发上游，不会产生错价）。
func claudeThinkingPreferenceFromResponsesRequest(request *apicompat.ResponsesRequest) (bool, string) {
	if request == nil {
		return false, ""
	}
	claudeRequest, err := apicompat.ResponsesToAnthropicRequest(request)
	if err != nil || claudeRequest == nil {
		return false, ""
	}
	claudeBody, err := json.Marshal(claudeRequest)
	if err != nil {
		return false, ""
	}
	return ClaudeThinkingPreferenceFromBody(claudeBody)
}

// ClaudeThinkingPreferenceFromConvertedBody 从"已转换为 Claude 协议"的请求体推导 thinking 偏好。
//
// Antigravity 兼容链路（Chat Completions / Responses 经 ForwardAsChatCompletions /
// ForwardAsResponses）在出站时用的是转换后的 claudeBody 里的 thinking 字段来选模型
// （antigravity_gateway_compat.go:215-227），因此冻结侧必须用**同一份** claudeBody 推导，
// 而不是原始请求体：原始体里是 reasoning_effort / reasoning.effort，只有转换后才会变成
// thinking.type/budget_tokens，两者档位口径不同。
func ClaudeThinkingPreferenceFromConvertedBody(claudeBody []byte) (bool, string) {
	return ClaudeThinkingPreferenceFromBody(claudeBody)
}

// GeminiThinkingPreferenceFromBody 从 Gemini 原生请求体推导 thinking 偏好。
//
// Gemini 原生出站路径（antigravity_gateway_gemini.go:91 的 ForwardGemini）只按档位挑
// -low/-medium/-high 变体，**不会**再套 applyThinkingModelSuffix 的 -thinking 后缀，因此
// enabled 恒为 false：多套一次后缀就会让冻结值与实际出站不一致，又变成 200 但零计费。
// 档位直接复用 geminiThinkingLevelFromBody，保证与出站同源。
func GeminiThinkingPreferenceFromBody(body []byte) (bool, string) {
	return false, geminiThinkingLevelFromBody(body)
}

// claudeThinkingLevelFromBudget 复刻 geminiThinkingLevelFromClaudeThinking 的档位推导：
// disabled → low；budget <= 0（动态/未指定）→ high；<=1024 → low；<=8192 → medium；
// 其余 high。
func claudeThinkingLevelFromBudget(thinkingType string, budget gjson.Result) string {
	if strings.EqualFold(thinkingType, "disabled") {
		return "low"
	}
	if !budget.Exists() || budget.Type != gjson.Number {
		return "high"
	}
	value := budget.Float()
	switch {
	case value <= 0:
		return "high"
	case value <= geminiThinkingBudgetLowMax:
		return "low"
	case value <= geminiThinkingBudgetMediumMax:
		return "medium"
	default:
		return "high"
	}
}
