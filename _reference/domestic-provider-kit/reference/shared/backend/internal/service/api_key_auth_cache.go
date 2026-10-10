package service

import "time"

// APIKeyAuthSnapshot API Key 认证缓存快照（仅包含认证所需字段）
type APIKeyAuthSnapshot struct {
	Version          int                    `json:"version"`
	APIKeyID         int64                  `json:"api_key_id"`
	UserID           int64                  `json:"user_id"`
	GroupID          *int64                 `json:"group_id,omitempty"`
	AggregateEnabled bool                   `json:"aggregate_enabled"`
	AggregateRoutes  []AggregateRoute       `json:"aggregate_routes,omitempty"`
	Name             string                 `json:"name"`
	Status           string                 `json:"status"`
	IPWhitelist      []string               `json:"ip_whitelist,omitempty"`
	IPBlacklist      []string               `json:"ip_blacklist,omitempty"`
	User             APIKeyAuthUserSnapshot `json:"user"`
	Group            *Group                 `json:"group,omitempty"`

	// Quota fields for API Key independent quota feature
	Quota     float64 `json:"quota"`      // Quota limit in USD (0 = unlimited)
	QuotaUsed float64 `json:"quota_used"` // Used quota amount

	// Expiration field for API Key expiration feature
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // Expiration time (nil = never expires)

	// Rate limit configuration (only limits, not usage - usage read from Redis at check time)
	RateLimit5h float64 `json:"rate_limit_5h"`
	RateLimit1d float64 `json:"rate_limit_1d"`
	RateLimit7d float64 `json:"rate_limit_7d"`
}

// APIKeyAuthUserSnapshot 用户快照
type APIKeyAuthUserSnapshot struct {
	ID            int64   `json:"id"`
	Status        string  `json:"status"`
	Role          string  `json:"role"`
	Balance       float64 `json:"balance"`
	Concurrency   int     `json:"concurrency"`
	AllowedGroups []int64 `json:"allowed_groups,omitempty"`

	// Balance notification fields (required for CheckBalanceAfterDeduction)
	Email                      string             `json:"email"`
	Username                   string             `json:"username"`
	BalanceNotifyEnabled       bool               `json:"balance_notify_enabled"`
	BalanceNotifyThresholdType string             `json:"balance_notify_threshold_type"`
	BalanceNotifyThreshold     *float64           `json:"balance_notify_threshold,omitempty"`
	BalanceNotifyExtraEmails   []NotifyEmailEntry `json:"balance_notify_extra_emails,omitempty"`
	TotalRecharged             float64            `json:"total_recharged"`

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）；用于 billing_cache_service.checkRPM 兜底判断。
	RPMLimit int `json:"rpm_limit"`

	// UserGroupRPMOverride 该 API Key 对应的 (user, group) 专属 RPM 覆盖值。
	// nil = 无 override（回退到 group/user 级）；0 = 不限流；>0 = 专属上限。
	UserGroupRPMOverride *int `json:"user_group_rpm_override,omitempty"`
}

// v1.0.34: 分组快照直接内嵌完整 *Group（全字段自动跟随），替代手写字段镜像。
// 手写镜像先后漏过 media 计费与 domestic_provider（国模计费为 0）字段。
// 字段完整性由 api_key_auth_cache_parity_test.go 反射测试守门。

// APIKeyAuthCacheEntry 缓存条目，支持负缓存
type APIKeyAuthCacheEntry struct {
	NotFound bool                `json:"not_found"`
	Snapshot *APIKeyAuthSnapshot `json:"snapshot,omitempty"`
}
