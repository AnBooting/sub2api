package domain

// VideoBillingMode 是视频计费的计量模式（PRD-06 / K-4）。
//
// 之所以需要它：不同上游厂商的官方口径并不一致——
//   - Ark Seedance：按 token 计费（token 单价 × usage.completion_tokens）；
//   - xAI Grok Imagine：按秒计费（时长 × 每秒单价）；
//   - MiniMax Hailuo：按次计费（查询响应不返回时长）。
//
// 因此「一个模型一种模式」必须是一等公民，而不是把所有视频都当成按秒。
type VideoBillingMode string

const (
	// VideoBillingModePerRequest 按次计费（单价 × 次数）。
	VideoBillingModePerRequest VideoBillingMode = "per_request"
	// VideoBillingModePerSecond 按秒计费（每秒单价 × 时长秒数 × 次数）。
	VideoBillingModePerSecond VideoBillingMode = "per_second"
	// VideoBillingModePerToken 按 token 计费（每百万 token 单价 × usage token 用量）。
	VideoBillingModePerToken VideoBillingMode = "per_token"
)

// IsValid 报告模式是否为受支持值；空串表示「未配置」，调用方必须 fail closed。
func (m VideoBillingMode) IsValid() bool {
	switch m {
	case VideoBillingModePerRequest, VideoBillingModePerSecond, VideoBillingModePerToken:
		return true
	}
	return false
}

// VideoTokenPrice 是按 token 计费时的单价分档（元/百万 token）。
//
// 官方按「输出分辨率 × 输入是否含视频」分档，所以两个维度都是可选的：
// 调用方取到哪一档用哪一档，取不到就不计价（fail closed），不猜价。
type VideoTokenPrice struct {
	WithInputVideo    *float64 `json:"with_input_video,omitempty"`
	WithoutInputVideo *float64 `json:"without_input_video,omitempty"`
}

// VideoPricePlan 是单个模型族的视频计费计划。
//
// 单价全部由管理员填写：本结构不携带任何内置默认价，未配置即拒绝放行（缺价不得放行）。
type VideoPricePlan struct {
	// Mode 为计量模式；空串表示未配置。
	Mode VideoBillingMode `json:"mode,omitempty"`
	// PerRequestPrice 按次单价，仅 Mode=per_request 时使用。
	PerRequestPrice *float64 `json:"per_request_price,omitempty"`
	// PerSecondPrices 每秒单价，按分辨率分档，仅 Mode=per_second 时使用。
	PerSecondPrices map[string]float64 `json:"per_second_prices,omitempty"`
	// PerTokenPrices 每百万 token 单价，按分辨率分档，仅 Mode=per_token 时使用。
	PerTokenPrices map[string]VideoTokenPrice `json:"per_token_prices,omitempty"`
}
