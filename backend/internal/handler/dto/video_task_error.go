package dto

import "strings"

// 视频任务失败原因的用户侧错误码。
//
// 背景：worker 失败时把**内部诊断**写进 last_error（承载账号 ID、仓储错误、SQL 片段、
// 内部主机名……）。用户查自己的任务不该看到这些；管理端排障仍需要完整 last_error，
// 所以它保留在库里（仓储照旧存原文），只有用户响应被收敛成「稳定错误码 + 可公开文案」。
// 前端按码做 i18n，不按文案匹配。
const (
	// VideoTaskErrorCodeProcessingDelayed：任务仍在处理中。
	VideoTaskErrorCodeProcessingDelayed = "processing_delayed"
	// VideoTaskErrorCodeAccountNotBound：任务没有绑定到可用的承载账号。
	VideoTaskErrorCodeAccountNotBound = "account_not_bound"
	// VideoTaskErrorCodeAccountLoadFailed：内部读取承载账号失败（用户侧看不到账号 ID 与仓储错误）。
	VideoTaskErrorCodeAccountLoadFailed = "account_load_failed"
	// VideoTaskErrorCodeDependencyUnavailable：账号仓储 / 上游查询器等内部依赖不可用。
	VideoTaskErrorCodeDependencyUnavailable = "dependency_unavailable"
	// VideoTaskErrorCodeUpstreamFailed：上游生成服务调用或状态查询失败。
	VideoTaskErrorCodeUpstreamFailed = "upstream_failed"
	// VideoTaskErrorCodeSettlementFailed：上游已成功，但结算暂未完成。
	VideoTaskErrorCodeSettlementFailed = "settlement_failed"
	// VideoTaskErrorCodeInternal：其余内部错误。
	VideoTaskErrorCodeInternal = "internal_error"
)

// videoTaskPublicErrorMessages 是各错误码对应的**可公开**说明。
//
// 注意：这里给的是按错误码写死的通用文案，**不是**把内部错误改写后发给用户——
// 内部诊断（账号 ID、仓储错误、SQL 片段、内部主机名）一律不出现在用户响应里。
var videoTaskPublicErrorMessages = map[string]string{
	VideoTaskErrorCodeProcessingDelayed:     "The task is still processing. Check its status later or contact support.",
	VideoTaskErrorCodeAccountNotBound:       "No usable upstream account for this task right now. Please retry later or contact support.",
	VideoTaskErrorCodeAccountLoadFailed:     "The task could not load its upstream account. Please retry later or contact support.",
	VideoTaskErrorCodeDependencyUnavailable: "A task dependency is temporarily unavailable. Please retry later or contact support.",
	VideoTaskErrorCodeUpstreamFailed:        "The video task did not complete. Check its result before submitting another task.",
	VideoTaskErrorCodeSettlementFailed:      "The video was generated, but settlement is not complete. Check its status later or contact support.",
	VideoTaskErrorCodeInternal:              "The task failed. Please retry later or contact support.",
}

// VideoTaskPublicErrorCode 把内部 last_error 归一成用户侧稳定错误码。
//
// 归类依据是 worker 写库时的原因（见 internal/service/video_task_worker.go）：
//   - "carrying account is not bound…"                    → account_not_bound
//   - "load carrying account … failed"                    → account_load_failed
//   - "upstream querier or account repository is unavailable" → dependency_unavailable
//   - "settlement deferred: upstream succeeded but billing did not complete" → settlement_failed
//   - 上游调用 / 状态查询失败（文案含 upstream）             → upstream_failed
//   - 其余（含无法归类的）                                  → internal_error
//
// 空错误返回空码（前端据此不显示错误行）。
func VideoTaskPublicErrorCode(lastError string) string {
	trimmed := strings.TrimSpace(lastError)
	if trimmed == "" {
		return ""
	}
	lowered := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lowered, "carrying account is not bound"):
		return VideoTaskErrorCodeAccountNotBound
	case strings.Contains(lowered, "load carrying account"):
		return VideoTaskErrorCodeAccountLoadFailed
	case strings.Contains(lowered, "account repository is unavailable"):
		return VideoTaskErrorCodeDependencyUnavailable
	case lowered == "settlement deferred: upstream succeeded but billing did not complete":
		return VideoTaskErrorCodeSettlementFailed
	case strings.Contains(lowered, "upstream"):
		return VideoTaskErrorCodeUpstreamFailed
	default:
		return VideoTaskErrorCodeInternal
	}
}

// VideoTaskPublicErrorMessage 返回错误码对应的可公开说明。
//
// 未知码回落到 internal_error 的通用说明；空码返回空串。
func VideoTaskPublicErrorMessage(code string) string {
	if code == "" {
		return ""
	}
	if message, ok := videoTaskPublicErrorMessages[code]; ok {
		return message
	}
	return videoTaskPublicErrorMessages[VideoTaskErrorCodeInternal]
}
