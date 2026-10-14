package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// resolveGatewayRequestAccountUpstreamModel 复刻 GatewayService.Forward（/v1/messages
// 普通分支）的账号级模型链，供请求锁价在冻结表里写入"该账号实际会转发出去的模型"。
//
// 冻结表的用途是与 ForwardResult.UpstreamModel 做等值核对，因此这里不能只按
// account.GetMappedModel 的通用口径取值：Anthropic OAuth/SetupToken 会做标准 ID
// 归一化，Vertex ServiceAccount 会做带日期 ID 与 region 形态转换，Bedrock 会解析
// 成 Bedrock model id。任一处口径不一致都会让实际成功的请求在落账时被 fail-closed
// 拒绝（用户拿到响应但计费为零），这比提前拒绝更糟。
//
// Antigravity 还必须带上请求体的 thinking 偏好：出站侧（antigravity_gateway_claude.go）
// 会对 claude-sonnet-4-5 追加 -thinking 后缀，裸 gemini-* 名还会按档位选 low/medium/high
// 变体。偏好由调用方在冻结前写入 ctx（WithGatewayRequestThinkingPreference）。
func resolveGatewayRequestAccountUpstreamModel(ctx context.Context, account *Account, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if account == nil {
		return requestedModel
	}
	// Ordinary HTTP cross-family candidates follow their actual forwarding family.
	if domain.UsesOpenAIGateway(account.Platform) {
		return resolveOpenAIAccountUpstreamModelForRequest(account, requestedModel, false)
	}
	switch {
	// Gemini OAuth Forward/ForwardNative do not apply account model mappings.
	// Freeze that existing protocol behavior rather than changing OAuth routing.
	case account.Platform == PlatformGemini && account.Type == AccountTypeOAuth:
		return requestedModel
	case account.IsAnthropicAPIKeyPassthroughEnabled():
		return mappedOrRequested(account, requestedModel)
	case account.IsBedrock():
		if resolved, ok := ResolveBedrockModelID(account, requestedModel); ok {
			return resolved
		}
		return mappedOrRequested(account, requestedModel)
	case account.Platform == PlatformAntigravity && account.Type == AccountTypeUpstream:
		// Antigravity 上游透传账号（AccountTypeUpstream）走 ForwardUpstream：只换认证头，
		// 不做任何账号映射/thinking 后缀改写，出站模型就是请求模型本身
		// （antigravity_gateway_upstream.go 的 originalModel）。注意这与同平台的 OAuth /
		// APIKey 分支口径**不同**，不能套用 mapAntigravityModel。
		return requestedModel
	case account.Platform == PlatformAntigravity:
		return resolveAntigravityAccountUpstreamModel(ctx, account, requestedModel)
	case account.Type == AccountTypeAPIKey:
		return mappedOrRequested(account, requestedModel)
	case account.Platform == PlatformAnthropic && account.Type == AccountTypeServiceAccount:
		if mapped, matched := account.ResolveMappedModel(requestedModel); matched {
			return strings.TrimSpace(mapped)
		}
		return normalizeVertexAnthropicModelID(claude.NormalizeModelID(requestedModel))
	case account.Platform == PlatformAnthropic && account.Type != AccountTypeAPIKey:
		return claude.NormalizeModelID(requestedModel)
	default:
		return mappedOrRequested(account, requestedModel)
	}
}

// resolveAntigravityAccountUpstreamModel 与 AntigravityGatewayService.Forward 的
// /v1/messages（Claude 协议）出站模型链同源：
//
//	getMappedModelForThinkingLevel(account, model, level) -> applyThinkingModelSuffix(mapped, enabled)
//
// thinking 偏好来自冻结前写入 ctx 的请求体信号；ctx 未携带时按"未开启思考"解析，
// 与不带 thinking 字段的请求一致。
func resolveAntigravityAccountUpstreamModel(ctx context.Context, account *Account, requestedModel string) string {
	enabled, level := gatewayRequestThinkingPreferenceFromContext(ctx)
	mapped := resolveAntigravityMappedModelForThinkingLevel(account, requestedModel, level)
	if mapped == "" {
		mapped = mappedOrRequested(account, requestedModel)
	}
	return applyThinkingModelSuffix(mapped, enabled)
}

// resolveAntigravityMappedModelForThinkingLevel 是 getMappedModelForThinkingLevel 的
// 包级等价实现：先尝试把裸 gemini-* 名解析到上游目录里真实存在的档位变体，未命中时
// 回退常规账号映射。该函数不依赖 AntigravityGatewayService 实例（冻结路径在
// GatewayService 上，拿不到 Antigravity 服务），但两者共用同一组底层解析原语。
func resolveAntigravityMappedModelForThinkingLevel(account *Account, requestedModel string, thinkingLevel string) string {
	if mapped, ok := resolveGeminiThinkingVariantForLevel(account, requestedModel, thinkingLevel); ok {
		return mapped
	}
	return mapAntigravityModel(account, requestedModel)
}

func mappedOrRequested(account *Account, requestedModel string) string {
	if mapped := strings.TrimSpace(account.GetMappedModel(requestedModel)); mapped != "" {
		return mapped
	}
	return requestedModel
}
