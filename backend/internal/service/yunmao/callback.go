package yunmao

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// PendingCallbackVerifier 是回调验签的**预留骨架**。
//
// 高危红线（用户本轮明确要求，PRD-03 §2.4 / AC-B1）：
// 上游签名算法尚未确定，因此本站**无法证明**任何回调可信。
// 这个实现刻意在 Verify 里无条件返回 CodeCallbackSignatureUnavailable，
// 绝不「先放行后补验签」，也绝不用「前端跳回成功页 / URL 参数 / 请求体里的 status=paid」当依据。
//
// 拿到接口文档后的填充步骤（全部集中在本文件）：
//  1. 用真实算法名替换 signatureAlgorithm（当前为空串 = 未确定）；
//  2. 在 Verify 里按文档实现「取签名头 → 拼接待签串 → 恒定时间比较 → 校验时间戳/nonce 重放窗口」；
//  3. 在 Decode 里按文档实现报文到 CallbackEvent 的映射。
//
// 在此之前，业务侧只会走「服务端主动查单」这条可信路径（Service.SettleOrder）。
type PendingCallbackVerifier struct{}

// SignatureAlgorithm 为空串表示签名算法未确定。TODO(云猫文档)。
func (PendingCallbackVerifier) SignatureAlgorithm() string { return "" }

// Verify 永远 fail closed。
//
// TODO(云猫文档)：按真实签名算法实现；实现前必须保持「拒绝」语义。
func (PendingCallbackVerifier) Verify(context.Context, http.Header, []byte) error {
	return NewError(
		CodeCallbackSignatureUnavailable,
		"回调验签算法未确定，已拒绝该回调（等待上游接口文档）",
		nil,
	)
}

// Decode 在验签通过前不可用；即使被直接调用也只返回未实现错误，绝不猜测字段。
//
// TODO(云猫文档)：按真实报文结构实现。
func (PendingCallbackVerifier) Decode(context.Context, []byte) (*CallbackEvent, error) {
	return nil, NewError(
		CodeCallbackSignatureUnavailable,
		"回调报文解码未实现（等待上游接口文档）",
		nil,
	)
}

// CallbackDecision 是回调处理后的判定结果。
//
// Accepted 为 true **只**代表「回调可信且已被采纳」；任何验签失败或未确定都必须是 false。
type CallbackDecision struct {
	Accepted bool
	// Code 是拒绝原因（Accepted=false 时必填）。
	Code ErrorCode
	// Event 是验签通过后解码出的事件。
	Event *CallbackEvent
}

// EvaluateCallback 是「只认验签通过」的判定骨架。
//
// 语义（这是回调路径唯一允许的判定顺序）：
//  1. 未配置上游 → 拒绝（fail closed）；
//  2. 验签器缺失或算法未确定 → 拒绝，并且**不尝试解码**；
//  3. 验签失败 → 拒绝；
//  4. 验签通过 → 解码，解码失败同样拒绝；
//  5. 解码成功但事件缺少订单号 → 拒绝。
//
// 返回的 Accepted=false 一定伴随具体错误码，调用方必须原样记入订单的 last_error_code，
// 并保持订单状态不变（PRD-03 接口文档：验签失败 401 且不改状态）。
func EvaluateCallback(cfg Config, verifier CallbackVerifier, ctx context.Context, headers http.Header, body []byte) CallbackDecision {
	if err := ValidateConfig(cfg); err != nil {
		return CallbackDecision{Accepted: false, Code: CodeNotConfigured}
	}
	if verifier == nil {
		return CallbackDecision{Accepted: false, Code: CodeCallbackSignatureUnavailable}
	}
	if strings.TrimSpace(verifier.SignatureAlgorithm()) == "" {
		return CallbackDecision{Accepted: false, Code: CodeCallbackSignatureUnavailable}
	}
	if err := verifier.Verify(ctx, headers, body); err != nil {
		code := CodeOf(err)
		if code == "" {
			code = CodeCallbackSignatureInvalid
		}
		return CallbackDecision{Accepted: false, Code: code}
	}
	event, err := verifier.Decode(ctx, body)
	if err != nil {
		code := CodeOf(err)
		if code == "" {
			code = CodeCallbackPayloadInvalid
		}
		return CallbackDecision{Accepted: false, Code: code}
	}
	if event == nil || strings.TrimSpace(event.UpstreamOrderNo) == "" {
		return CallbackDecision{Accepted: false, Code: CodeCallbackPayloadInvalid}
	}
	return CallbackDecision{Accepted: true, Event: event}
}

// HMACCallbackVerifier 是给「签名算法已确定为 HMAC-SHA256 共享密钥」情形准备的**可替换实现**。
//
// 它不参与默认装配（默认是 PendingCallbackVerifier）：只有上游文档明确写清
// 「签名头名 + 待签串拼接规则 + 时间戳头 + 重放窗口」之后，才允许把它接上，
// 并同步实现 Decode。保留它的价值是：验签判定逻辑（恒定时间比较、时间戳窗口、
// 重放保护）可以被单测钉死，而不是等到文档到位才从零写。
type HMACCallbackVerifier struct {
	// Secret 是共享密钥，只从环境变量读取。
	Secret []byte
	// SignatureHeader 是签名所在请求头名（由文档确定）。
	SignatureHeader string
	// TimestampHeader 是时间戳所在请求头名（由文档确定）。
	TimestampHeader string
	// Payload 由文档确定的待签串构造规则提供；返回空串视为无法构造，必须拒绝。
	Payload func(headers http.Header, body []byte) string
	// MaxSkew 是允许的时间戳偏差（重放窗口）。
	MaxSkew time.Duration
	// DecodeFunc 由文档确定。
	DecodeFunc func(body []byte) (*CallbackEvent, error)
	// Now 可注入时钟，便于测试重放窗口。
	Now func() time.Time
}

// SignatureAlgorithm 返回算法标识。
func (v *HMACCallbackVerifier) SignatureAlgorithm() string { return "hmac-sha256" }

// Verify 校验 HMAC 签名与时间戳窗口。
//
// 任何一步缺失（头不存在、待签串为空、密钥为空、时间戳越界、签名不匹配）都返回错误，
// 不存在「跳过校验」的分支。
func (v *HMACCallbackVerifier) Verify(_ context.Context, headers http.Header, body []byte) error {
	if v == nil || len(v.Secret) == 0 {
		return NewError(CodeCallbackSignatureUnavailable, "回调验签密钥未配置", nil)
	}
	if v.Payload == nil || strings.TrimSpace(v.SignatureHeader) == "" {
		return NewError(CodeCallbackSignatureUnavailable, "回调验签规则未确定（等待上游接口文档）", nil)
	}
	signature := strings.TrimSpace(headers.Get(v.SignatureHeader))
	if signature == "" {
		return NewError(CodeCallbackSignatureInvalid, "回调缺少签名", nil)
	}
	payload := v.Payload(headers, body)
	if strings.TrimSpace(payload) == "" {
		return NewError(CodeCallbackSignatureUnavailable, "回调待签串无法构造", nil)
	}
	if v.TimestampHeader != "" && v.MaxSkew > 0 {
		rawTimestamp := strings.TrimSpace(headers.Get(v.TimestampHeader))
		if rawTimestamp == "" {
			return NewError(CodeCallbackSignatureInvalid, "回调缺少时间戳", nil)
		}
		now := time.Now()
		if v.Now != nil {
			now = v.Now()
		}
		if err := withinSkew(rawTimestamp, now, v.MaxSkew); err != nil {
			return err
		}
	}
	if !constantTimeEqualHex(signature, SignHMACSHA256Hex(v.Secret, payload)) {
		return NewError(CodeCallbackSignatureInvalid, "回调签名不匹配", nil)
	}
	return nil
}

// Decode 使用文档确定的解码函数；未提供即拒绝。
func (v *HMACCallbackVerifier) Decode(_ context.Context, body []byte) (*CallbackEvent, error) {
	if v == nil || v.DecodeFunc == nil {
		return nil, NewError(CodeCallbackSignatureUnavailable, "回调报文解码未实现（等待上游接口文档）", nil)
	}
	return v.DecodeFunc(body)
}

func withinSkew(rawTimestamp string, now time.Time, maxSkew time.Duration) error {
	parsed, err := parseTimestamp(rawTimestamp)
	if err != nil {
		return NewError(CodeCallbackSignatureInvalid, "回调时间戳非法", nil)
	}
	diff := now.Sub(parsed)
	if diff < 0 {
		diff = -diff
	}
	if diff > maxSkew {
		return NewError(CodeCallbackSignatureInvalid, "回调时间戳超出重放窗口", nil)
	}
	return nil
}

// parseTimestamp 同时接受 Unix 秒与 RFC3339（文档确定后再收敛到一种）。
func parseTimestamp(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if seconds, err := parseInt64(raw); err == nil {
		return time.Unix(seconds, 0), nil
	}
	return time.Time{}, errInvalidTimestamp
}
