package service

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// 缺价告警通道：让管理员看得见「哪些模型在因缺价被拒」。
//
// 红线不变：缺价不得放行（未定价模型在转发上游前被拒，HTTP 503 + pricing_unavailable）。
// 本文件只做**观察**：不改变拒绝行为、不放行缺价、不按零价计费。
//
// 数据来源是既有的 ops_error_logs（审计/错误日志机制）：缺价拒绝本来就会落一条错误记录，
// 这里按「模型 + 分组」聚合出管理员可查的清单，**不新增表、不新增迁移**。

// missingPriceRejectionMarkers 是缺价拒绝的文案特征。
//
// 与网关 billingErrorDetails 的错误文案一致（缺价一律 503，code=pricing_unavailable）：
//   - "no pricing available for model: X: pricing not found"
//   - "Model pricing is unavailable. Please retry later."
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

// MissingPriceRejection 是按「模型 + 分组」聚合出的一条缺价告警。
type MissingPriceRejection struct {
	// Model 是被拒的模型名（请求模型优先，与错误列表展示同义）。
	Model string `json:"model"`
	// GroupID / GroupName 是被拒时所在的分组（可能为空）。
	GroupID   *int64 `json:"group_id,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	// Count 是窗口内的被拒次数。
	Count int64 `json:"count"`
	// FirstSeenAt / LastSeenAt 是窗口内首次与最近一次被拒时间。
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	// LastMessage 是最近一次被拒的文案（脱敏后的错误描述）。
	LastMessage string `json:"last_message,omitempty"`
}

// MissingPriceRejectionFilter 是缺价聚合的查询条件。
type MissingPriceRejectionFilter struct {
	// StartTime / EndTime 是统计窗口；StartTime 为零时默认最近 24 小时。
	StartTime time.Time
	EndTime   time.Time
	// Limit 是返回条数上限（默认 50，最大 200）。
	Limit int
}

// DefaultMissingPriceRejectionWindow 是缺价告警的默认统计窗口。
const DefaultMissingPriceRejectionWindow = 24 * time.Hour

// MissingPriceRejections 返回按「模型 + 分组」聚合的缺价拒绝清单。
//
// 只读观察：不会改写错误记录，也不影响网关的缺价拒绝行为。
func (s *OpsService) MissingPriceRejections(ctx context.Context, filter *MissingPriceRejectionFilter) ([]*MissingPriceRejection, error) {
	if s == nil || s.opsRepo == nil {
		return []*MissingPriceRejection{}, nil
	}
	normalized := normalizeMissingPriceRejectionFilter(filter, time.Now())
	return s.opsRepo.AggregateMissingPriceRejections(ctx, normalized)
}

// normalizeMissingPriceRejectionFilter 补齐缺省窗口与条数上限。
func normalizeMissingPriceRejectionFilter(filter *MissingPriceRejectionFilter, now time.Time) *MissingPriceRejectionFilter {
	out := &MissingPriceRejectionFilter{}
	if filter != nil {
		*out = *filter
	}
	if out.EndTime.IsZero() {
		out.EndTime = now
	}
	if out.StartTime.IsZero() {
		out.StartTime = out.EndTime.Add(-DefaultMissingPriceRejectionWindow)
	}
	if out.Limit <= 0 {
		out.Limit = 50
	}
	if out.Limit > 200 {
		out.Limit = 200
	}
	return out
}
