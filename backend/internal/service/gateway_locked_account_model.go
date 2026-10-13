package service

import (
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
// 已知边界：Antigravity 账号在 /v1/messages 上还会按请求体的 thinking 预算挑选
// gemini thinking 变体，冻结入口拿不到该请求体信号，只能按无偏好档位解析。若实际
// 出站模型与冻结值不同，消费侧仍会 fail-closed 拒绝，不会错价。
func resolveGatewayRequestAccountUpstreamModel(account *Account, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	if account == nil {
		return requestedModel
	}
	switch {
	case account.IsAnthropicAPIKeyPassthroughEnabled():
		return mappedOrRequested(account, requestedModel)
	case account.IsBedrock():
		if resolved, ok := ResolveBedrockModelID(account, requestedModel); ok {
			return resolved
		}
		return mappedOrRequested(account, requestedModel)
	case account.Platform == PlatformAntigravity:
		if mapped := mapAntigravityModel(account, requestedModel); mapped != "" {
			return applyThinkingModelSuffix(mapped, false)
		}
		return applyThinkingModelSuffix(mappedOrRequested(account, requestedModel), false)
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

func mappedOrRequested(account *Account, requestedModel string) string {
	if mapped := strings.TrimSpace(account.GetMappedModel(requestedModel)); mapped != "" {
		return mapped
	}
	return requestedModel
}
