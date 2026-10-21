package service

import (
	"time"
)

// UserErrorRequest 是面向终端用户的"错误请求"精简脱敏视图（白名单）。
// 严禁包含 account / api_key_prefix / upstream_endpoint / user_email 等
// 敏感或内部字段。注：message（网关标准化错误描述）与 key_name
// （用户自有 API Key 名称，KeysView 中本就可见）经产品决策对该用户开放；
// client_ip / user_agent / group_name / request_type / stream 均为该用户
// 自己请求的属性，经产品决策（2026-07-03）开放，
// 与用量明细已向用户展示自身 ip_address/user_agent/分组/类型 的口径对齐；
// error_body 仅在详情接口（GetUserErrorRequestDetail）按归属校验后返回。
type UserErrorRequest struct {
	ID              int64     `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	Model           string    `json:"model"`
	InboundEndpoint string    `json:"inbound_endpoint"`
	StatusCode      int       `json:"status_code"`
	Category        string    `json:"category"`
	// Code 是稳定的机器可读错误码（前端按 code 判定，不解析文案）。
	// 取值见 UserErrorCode；缺价被拒时为 pricing_unavailable。
	Code        string `json:"code"`
	Platform    string `json:"platform"`
	Message     string `json:"message"`
	KeyName     string `json:"key_name"`
	KeyDeleted  bool   `json:"key_deleted"`
	ClientIP    string `json:"client_ip,omitempty"`
	GroupName   string `json:"group_name,omitempty"`
	RequestType *int16 `json:"request_type,omitempty"`
	Stream      bool   `json:"stream"`
	UserAgent   string `json:"user_agent,omitempty"`
}

// UserErrorRequestList 是用户错误请求分页结果。
type UserErrorRequestList struct {
	Items    []*UserErrorRequest `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// MapUserErrorCategory 把后端 error_phase + error_type 映射为用户侧粗分类码。
// 返回的是稳定的分类 code（前端做 i18n），不是展示文案。
func MapUserErrorCategory(phase, errType string) string {
	switch phase {
	case "auth":
		return "auth"
	case "routing":
		return "service_unavailable"
	case "account_auth", "upstream", "network":
		return "upstream"
	case "internal":
		return "internal"
	case "request":
		switch errType {
		case "rate_limit_error":
			return "rate_limit"
		case "billing_error", "subscription_error":
			return "quota"
		case "invalid_request_error":
			return "invalid_request"
		case "cyber_policy":
			return "cyber"
		}
	}
	return "other"
}

// CategoryToFilter 把用户侧分类码反向映射为后端过滤条件（plain ANY）。
// 未知分类返回两个空切片（即不施加分类过滤）。
// 注意："other" 与未知分类都走 default 返回空切片——"other" 无对应的 phase/type 组合，无法精确反查，因此等价于不过滤。
func CategoryToFilter(category string) (phases []string, errorTypes []string) {
	switch category {
	case "auth":
		return []string{"auth"}, nil
	case "service_unavailable":
		return []string{"routing"}, nil
	case "upstream":
		return []string{"account_auth", "upstream", "network"}, nil
	case "internal":
		return []string{"internal"}, nil
	case "rate_limit":
		return nil, []string{"rate_limit_error"}
	case "quota":
		return nil, []string{"billing_error", "subscription_error"}
	case "invalid_request":
		return nil, []string{"invalid_request_error"}
	case "cyber":
		return []string{"request"}, []string{"cyber_policy"}
	default:
		return nil, nil
	}
}

// UserErrorRequestCodeMissingPrice 是缺价（模型没有可用价格）被拒时的稳定错误码。
//
// 网关在转发上游前就拒绝缺价模型（HTTP 503），错误体里的 type 就是 pricing_unavailable；
// 用户侧错误请求列表据此给「缺价」一个明确徽标，而不是让用户去猜后端原文。
const UserErrorRequestCodeMissingPrice = "pricing_unavailable"

// UserErrorCode 把一条错误请求归一成稳定的机器可读错误码。
//
// 前端按 code 判定（不再解析后端文案），因此取值是稳定契约：
//   - pricing_unavailable：缺价，模型没有可用价格（网关在转发上游前拒绝，HTTP 503）
//   - auth_failed：鉴权失败
//   - rate_limit：触发限流
//   - quota_exhausted：额度/订阅不足
//   - invalid_request：请求参数非法
//   - content_policy：内容策略拦截
//   - upstream_error：上游账号或网络错误
//   - service_unavailable：无可用渠道等路由侧不可用
//   - internal_error：内部错误
//   - other：其它
func UserErrorCode(e *OpsErrorLog) string {
	if e == nil {
		return "other"
	}
	if IsMissingPriceRejection(e.StatusCode, e.Message) {
		return UserErrorRequestCodeMissingPrice
	}
	switch e.Phase {
	case "auth":
		return "auth_failed"
	case "routing":
		return "service_unavailable"
	case "account_auth", "upstream", "network":
		return "upstream_error"
	case "internal":
		return "internal_error"
	case "request":
		switch e.Type {
		case "rate_limit_error":
			return "rate_limit"
		case "billing_error", "subscription_error":
			return "quota_exhausted"
		case "invalid_request_error":
			return "invalid_request"
		case "cyber_policy":
			return "content_policy"
		}
	}
	return "other"
}

// ToUserErrorRequest 把内部 OpsErrorLog 裁剪为用户安全视图。
func ToUserErrorRequest(e *OpsErrorLog) *UserErrorRequest {
	if e == nil {
		return nil
	}
	model := e.RequestedModel
	if model == "" {
		model = e.Model
	}
	clientIP := ""
	if e.ClientIP != nil {
		clientIP = *e.ClientIP
	}
	return &UserErrorRequest{
		ID:              e.ID,
		CreatedAt:       e.CreatedAt,
		Model:           model,
		InboundEndpoint: e.InboundEndpoint,
		StatusCode:      e.StatusCode,
		Category:        MapUserErrorCategory(e.Phase, e.Type),
		Code:            UserErrorCode(e),
		Platform:        e.Platform,
		Message:         e.Message,
		KeyName:         e.APIKeyName,
		KeyDeleted:      e.APIKeyDeleted,
		ClientIP:        clientIP,
		GroupName:       e.GroupName,
		RequestType:     e.RequestType,
		Stream:          e.Stream,
		UserAgent:       e.UserAgent,
	}
}

// UserErrorRequestDetail 是错误请求详情的脱敏视图(点击单行查看)。
// 在 UserErrorRequest 基础上额外暴露 error_body(上游错误响应正文)与 upstream_status_code;
// 仍严禁任何内部/敏感字段。
type UserErrorRequestDetail struct {
	UserErrorRequest
	ErrorBody          string `json:"error_body"`
	UpstreamStatusCode *int   `json:"upstream_status_code,omitempty"`
}

// ToUserErrorRequestDetail 把内部 OpsErrorLogDetail 裁剪为用户安全详情视图。
func ToUserErrorRequestDetail(e *OpsErrorLogDetail) *UserErrorRequestDetail {
	if e == nil {
		return nil
	}
	base := ToUserErrorRequest(&e.OpsErrorLog)
	return &UserErrorRequestDetail{
		UserErrorRequest:   *base,
		ErrorBody:          e.ErrorBody,
		UpstreamStatusCode: e.UpstreamStatusCode,
	}
}
