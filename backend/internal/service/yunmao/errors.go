package yunmao

import (
	"errors"
	"fmt"
)

// ErrorCode 是白标充值适配层对外暴露的稳定错误码集合。
//
// 这些码会出现在本站 API 的 error.reason 里，前端据此决定展示「未配置」「服务不可用」
// 还是普通失败。**不允许**用它们携带任何上游品牌、域名或凭据信息（PRD-03 FE-04）。
type ErrorCode string

const (
	// CodeNotConfigured 上游凭据或接口地址未配置。fail closed：不发起任何请求、不降级到
	// 其他支付渠道、不返回假数据（PRD-03 §2.8、共享契约 C-8：yunmao.* 凭据仅服务端）。
	CodeNotConfigured ErrorCode = "shop_not_configured"
	// CodeUpstreamUnavailable 上游网络/超时/5xx 等暂时不可用。
	CodeUpstreamUnavailable ErrorCode = "shop_upstream_unavailable"
	// CodeUpstreamRejected 上游以业务错误拒绝了本次调用。
	CodeUpstreamRejected ErrorCode = "shop_upstream_rejected"
	// CodeUpstreamResponseInvalid 上游返回了无法按契约解码的响应体。
	CodeUpstreamResponseInvalid ErrorCode = "shop_upstream_response_invalid"
	// CodeProductNotFound 商品不存在或已下架。
	CodeProductNotFound ErrorCode = "shop_product_not_found"
	// CodeProductSyncUnavailable 商品同步失败：保留最近成功数据但暂停购买（PRD-03 BE-08）。
	CodeProductSyncUnavailable ErrorCode = "product_sync_unavailable"
	// CodeOrderNotFound 订单不存在，或不属于当前用户（两者同一码，避免探测存在性）。
	CodeOrderNotFound ErrorCode = "shop_order_not_found"
	// CodeIdempotencyConflict 同一幂等键被不同内容复用。
	CodeIdempotencyConflict ErrorCode = "shop_idempotency_conflict"
	// CodeDeliveryPending 上游已支付但卡密尚未就绪：展示「已支付 / 交付异常」（PRD-03 §2.5）。
	CodeDeliveryPending ErrorCode = "order_delivery_pending"
	// CodeOrderNotPaid 上游明确表示尚未支付。
	CodeOrderNotPaid ErrorCode = "order_not_paid"
	// CodeCallbackSignatureUnavailable 回调验签算法尚未确定（等待上游接口文档），
	// 因此**无法**证明回调可信。必须 fail closed，绝不能默认放行（PRD-03 §2.4、AC-B1）。
	CodeCallbackSignatureUnavailable ErrorCode = "shop_callback_signature_unavailable"
	// CodeCallbackSignatureInvalid 回调验签不通过（签名错误、时间戳越界、重放）。
	CodeCallbackSignatureInvalid ErrorCode = "shop_callback_signature_invalid"
	// CodeCallbackPayloadInvalid 回调请求体不符合最小必要契约。
	CodeCallbackPayloadInvalid ErrorCode = "shop_callback_payload_invalid"
	// CodeDeliveryUnavailable 卡密加密槽位不可用（未注入加密器）。
	CodeDeliveryUnavailable ErrorCode = "shop_delivery_unavailable"
)

// ErrNotConfigured 是「未配置」哨兵错误，供上层用 errors.Is 判定。
var ErrNotConfigured = errors.New("yunmao: adapter is not configured")

// APIError 是适配层统一的错误类型。
//
// 设计约束：
//   - Reason 是稳定错误码，直接进入本站 API 的 error.reason；
//   - Message 只描述本站语义，禁止拼入上游响应原文、URL、凭据或卡密；
//   - Cause 只用于服务端日志与 errors.Is/As 判定，绝不序列化给前端。
type APIError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return fmt.Sprintf("yunmao[%s]: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("yunmao[%s]: %s: %v", e.Code, e.Message, e.Cause)
}

// Unwrap 让 errors.Is/As 能穿透到 Cause。
func (e *APIError) Unwrap() error { return e.Cause }

// Is 让同一错误码之间可以用 errors.Is 比较（即使 Message 不同）。
func (e *APIError) Is(target error) bool {
	var other *APIError
	if !errors.As(target, &other) {
		return false
	}
	return other.Code == e.Code
}

// NewError 构造一个适配层错误。
func NewError(code ErrorCode, message string, cause error) *APIError {
	return &APIError{Code: code, Message: message, Cause: cause}
}

// notConfiguredError 返回统一的「未配置」错误（同时可被 errors.Is(err, ErrNotConfigured) 命中）。
func notConfiguredError(detail string) *APIError {
	msg := "白标充值服务未配置"
	if detail != "" {
		msg = msg + "：" + detail
	}
	return NewError(CodeNotConfigured, msg, ErrNotConfigured)
}

// CodeOf 提取错误的稳定错误码；非适配层错误返回空串。
func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

// IsCode 判断错误是否属于指定错误码。
func IsCode(err error, code ErrorCode) bool {
	return CodeOf(err) == code
}
