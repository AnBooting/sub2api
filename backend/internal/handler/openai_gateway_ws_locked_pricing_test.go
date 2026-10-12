package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// C1 锁价消费切片（WS /responses）：最小契约测试。
//
// 现有缺陷：WS 握手只有 WithOpenAIRequestPricingContext（仅装利润门 + pricingAt），
// ctx 中没有冻结 bundle。锁价消费分支因此永不激活，WS 走 legacy 计费——缺价时按
// 零成本落账（usage.go 的 legacy 分支），管理员看不到任何拒绝。本切片要求与 HTTP
// Responses 文本入口同口径：首帧在选号/上游连接之前冻结价格，缺价即拒绝。

// TestOpenAIResponsesWebSocket_LockedPricingRejectsMissingPriceBeforeUpstream 锁定：
// 首帧模型在冻结价表中无客户价时，连接在上游请求之前被 WS policy violation 关闭，
// 不产生任何上游帧，也不写任何用量记录。WS 没有 HTTP 状态码，契约即"不建上游连接、
// 不进 turn、不落账"，与 HTTP 的 503 pricing_unavailable 语义等价。
func TestOpenAIResponsesWebSocket_LockedPricingRejectsMissingPriceBeforeUpstream(t *testing.T) {
	runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		// 未知模型：既无渠道/分组定价，也不在可确定性识别的内置价表内。
		firstPayload:            `{"type":"response.create","model":"ws-locked-unpriced-model","stream":false}`,
		firstFrameCloseExpected: true,
		closeReason:             "request pricing unavailable",
		closeStatus:             coderws.StatusPolicyViolation,
	})
}

// TestOpenAIResponsesWebSocket_LockedPricingImageIntentNotTextFrozen 记录与 HTTP
// 路径一致的产品口径：显式生图意图回合不进入文本锁价（lockedText =
// !imageIntent && platform == openai），因此不会因"缺 token 价"被文本锁价拒绝。
// 连接中途出现的生图回合由 turn 级检测回退 legacy 计费（见检查点 3）。
//
// 这里只用"分组不允许生图"的可观测差异证明握手未按文本锁价提前放行/拒绝：
// 若文本锁价错误地把生图回合当文本处理，未知生图模型会在选号前以
// "request pricing unavailable" 关闭；正确行为是走到生图能力校验并返回分组生图
// 未开启的 policy violation。
func TestOpenAIResponsesWebSocket_LockedPricingImageIntentNotTextFrozen(t *testing.T) {
	runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:            `{"type":"response.create","model":"ws-locked-image-model","stream":false,"tools":[{"type":"image_generation"}]}`,
		group:                   wsAllowlistGroup(false),
		firstFrameCloseExpected: true,
		closeReason:             service.ImageGenerationPermissionMessage(),
		closeStatus:             coderws.StatusPolicyViolation,
	})
}

// TestOpenAIResponsesWebSocket_LockedPricingUsesIngressModelForPassthrough 钉住
// Lead 裁决的组合：WS passthrough ingress + 账号未标 openai_passthrough +
// 非空 model_mapping。
//
// 该组合下 ingress 适配器只替换认证（通道映射结果直接出站、不套账号映射），
// 因此冻结表的账号上游模型必须按同一 ingress 口径解析，否则冻结值=账号映射结果、
// 实际值=通道映射结果，用量核对 fail-closed 整笔拒绝（served-but-unbilled）。
// 断言：出站模型等于通道映射结果；turn 正常落账一次且费用按该模型计价。
func TestOpenAIResponsesWebSocket_LockedPricingUsesIngressModelForPassthrough(t *testing.T) {
	const ingressModel = "ws-ingress-target"
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: `{"type":"response.create","model":"ws-ingress-source","stream":false}`,
		ingressMode:  service.OpenAIWSIngressModePassthrough,
		channelMapping: map[string]string{
			"ws-ingress-source": ingressModel,
		},
		// 账号标记保持缺省（false），且账号映射非空——即风险组合本身。
		accountModelMapping: map[string]any{
			"ws-ingress-source": "ws-account-mapped",
			ingressModel:        "ws-account-mapped",
		},
		// 测试 fixture 定价（非产品价）：只给实际上游模型定价，账号映射结果故意
		// 不给价——若冻结误用账号映射口径，接入会因缺价直接拒绝，用例即可失败。
		channelModelPricing: []service.ChannelModelPricing{
			wsFixtureTokenPricing([]string{ingressModel}, 1e-6, 2e-6),
		},
	})

	require.Len(t, got.upstreamPayloads, 1)
	require.Equal(t, ingressModel, gjson.GetBytes(got.upstreamPayloads[0], "model").String(),
		"passthrough ingress 只替换认证，出站模型必须是通道映射结果")
	require.Len(t, got.logs, 1, "该组合必须只落账一次（不得 served-but-unbilled）")
	require.NotNil(t, got.logs[0].UpstreamModel)
	require.Equal(t, ingressModel, *got.logs[0].UpstreamModel,
		"落账上游模型必须等于实际上游出站模型")
	require.Greater(t, got.logs[0].TotalCost, 0.0, "该 turn 必须按实际模型正常计价")
}

// TestOpenAIResponsesWebSocket_LockedPricingImageTurnFallsBackToLegacy 锁定检查点 3
// 的中途图生回合口径：首 turn 文本按锁价正常落账，第二 turn 显式声明
// image_generation 工具时该 turn 不进入文本锁价——不重冻、不绑账号，并剥离连接级
// bundle 回退 legacy 按次计费。
//
// 反例（本切片修复前）：第二 turn 继承建连时的文本冻结 bundle，按 token 口径计价/
// 拒绝，与"显式生图回合不锁价"的 HTTP 口径不一致。
func TestOpenAIResponsesWebSocket_LockedPricingImageTurnFallsBackToLegacy(t *testing.T) {
	group := wsAllowlistGroup(false)
	group.AllowImageGeneration = true
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: `{"type":"response.create","model":"gpt-5.4","stream":false}`,
		// 第二 turn：显式生图意图 + 无任何客户价的模型。若该 turn 仍走文本锁价，
		// 会在上游前以 "request pricing unavailable" 关闭连接；回退 legacy 才放行。
		secondPayload: `{"type":"response.create","model":"ws-locked-image-model","stream":false,"tools":[{"type":"image_generation"}]}`,
		group:         group,
	})

	// 关键回归信号：无客户价的生图 turn 仍到达上游。若该 turn 沿用连接级文本
	// 锁价（未剥离 bundle），它会在上游前以 "request pricing unavailable" 关闭。
	require.Len(t, got.upstreamPayloads, 2, "生图 turn 必须放行到上游（不得被文本锁价拒绝）")
	require.Len(t, got.logs, 2, "两个 turn 都必须落账")
	require.Greater(t, got.logs[0].TotalCost, 0.0, "文本 turn 必须按锁价正常计价")
	require.NotNil(t, got.logs[1].BillingMode)
	require.Equal(t, string(service.BillingModeToken), *got.logs[1].BillingMode,
		"生图 turn 走 legacy 分支（不是锁价 token 消费者）")
}

// TestOpenAIResponsesWebSocket_LockedPricingRejectsUnpricedModelOnLaterTurn 锁定
// recon §1.4-2：第二个 turn 换成缺价模型时，该 turn 必须在上游写入前被拒（关闭
// 连接），而不是沿用首 turn 的冻结价或零价落账。
func TestOpenAIResponsesWebSocket_LockedPricingRejectsUnpricedModelOnLaterTurn(t *testing.T) {
	runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:            `{"type":"response.create","model":"gpt-5.6-sol","stream":false}`,
		secondPayload:           `{"type":"response.create","model":"ws-locked-second-unpriced","stream":false}`,
		secondTurnCloseExpected: true,
		closeReason:             "request pricing unavailable",
		closeStatus:             coderws.StatusPolicyViolation,
	})
}
