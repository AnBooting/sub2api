package service

import (
	"net/http"
	"strings"
)

// 缺价判定：全站唯一的「这次失败是不是缺价」口径。
//
// 红线不变：缺价不得放行（未定价模型在转发上游前被拒，HTTP 503 + code=pricing_unavailable）。
// 这里只做**识别**，不改变拒绝行为、不放行缺价、不按零价计费。
//
// 与网关 billingErrorDetails 的错误文案一致（缺价一律 503）：
//   - "no pricing available for model: X: pricing not found"
//   - "Model pricing is unavailable. Please retry later."

// MissingPriceRejectionMarkers 是缺价拒绝的文案特征。
var MissingPriceRejectionMarkers = []string{
	"pricing not found",
	"no pricing available",
	"pricing is unavailable",
	"pricing_unavailable",
}

// MissingPriceRejectionStatusCode 是缺价拒绝的固定状态码（网关 billingErrorDetails → 503）。
const MissingPriceRejectionStatusCode = http.StatusServiceUnavailable

// IsMissingPriceRejection 判断一条错误记录是不是「缺价被拒」。
//
// 口径：HTTP 503（网关缺价拒绝的固定状态码）+ 文案命中缺价特征。
// 只认 503 + 文案，避免把普通 5xx 误报成缺价。
func IsMissingPriceRejection(statusCode int, message string) bool {
	if statusCode != MissingPriceRejectionStatusCode {
		return false
	}
	lowered := strings.ToLower(strings.TrimSpace(message))
	if lowered == "" {
		return false
	}
	for _, marker := range MissingPriceRejectionMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}
