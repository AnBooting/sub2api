package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	LiveControllerPending  = "pending"
	LiveControllerObserver = "observer"
	LiveControllerProxy    = "proxy"
	LiveControllerClosed   = "closed"
)

var (
	ErrLiveUnavailable       = errors.New("live is unavailable")
	ErrLiveConcurrencyFull   = errors.New("live concurrency is full")
	ErrLiveCallNotFound      = errors.New("live call not found")
	ErrLiveIdentityMismatch  = errors.New("live call identity mismatch")
	ErrLiveSettlementInvalid = errors.New("invalid live settlement state")
	ErrLiveControllerChanged = errors.New("live controller changed")
)

type LiveAttestationUnavailableError struct {
	Reason string
}

func (e *LiveAttestationUnavailableError) Error() string {
	if e == nil || e.Reason == "" {
		return "Live attestation is unavailable"
	}
	return "Live attestation is unavailable: " + e.Reason
}

// LiveCallRequest 是两个下游创建协议归一后的请求。Session 不做结构改写。
type LiveCallRequest struct {
	SDP     string          `json:"sdp"`
	Session json.RawMessage `json:"session"`
}

type LiveCallIdentity struct {
	APIKeyID        int64
	UserID          int64
	GroupID         *int64
	SubscriptionID  *int64
	UserAgent       string
	IPAddress       string
	InboundEndpoint string
	// RealtimePricePerMin 是开播时冻结的分组实时语音单价（元/分钟）。
	// Live 按时长计费，价格必须在会话创建时锁定：结算发生在会话关闭时，
	// 期间管理员可能改价，用结算价会与开播时的报价不一致。
	// nil 表示该分组未配置 Live 单价，结算时必须拒绝而不是按零价放行。
	RealtimePricePerMin *float64
	// BillingPolicy 是开播时的计费策略快照（密钥额度/窗口限额/平台）。
	// 结算可能由 observer 从 Redis 回读的记录触发，或需要对失败结算重试，
	// 都必须使用与开播一致的策略，不能回源读取可能已被修改的实时配置。
	BillingPolicy *LiveBillingPolicy
}

// LiveBillingPolicy 冻结一次 Live 会话的计费策略快照。
type LiveBillingPolicy struct {
	APIKeyQuota   float64 `json:"api_key_quota,omitempty"`
	RateLimit5h   float64 `json:"rate_limit_5h,omitempty"`
	RateLimit1d   float64 `json:"rate_limit_1d,omitempty"`
	RateLimit7d   float64 `json:"rate_limit_7d,omitempty"`
	GroupPlatform string  `json:"group_platform,omitempty"`
}

// LiveSettlement 持久化一次 Live 会话的结算事实：金额在第一次尝试前冻结，
// 重试只复用冻结金额，不随再次调用时间增长；status=settled 后不再重复扣费。
type LiveSettlement struct {
	Status         string    `json:"status"`
	TotalCost      float64   `json:"total_cost"`
	ActualCost     float64   `json:"actual_cost"`
	BillingMode    string    `json:"billing_mode,omitempty"`
	SettlementMode string    `json:"settlement_mode,omitempty"`
	DurationMs     int       `json:"duration_ms"`
	Attempts       int       `json:"attempts"`
	FrozenAt       time.Time `json:"frozen_at"`
	// UsageLogWritten 标记用量日志已落库，避免结算成功后重复补写（崩溃窗口补偿）。
	UsageLogWritten bool `json:"usage_log_written,omitempty"`
}

const (
	// LiveSettlementPending 表示金额已冻结但财务结算尚未成功，可安全重试。
	LiveSettlementPending = "pending"
	// LiveSettlementSettled 表示该会话已完成结算（含零费用/简易模式跳过）。
	LiveSettlementSettled = "settled"
)

type LiveCallRecord struct {
	CallID          string
	CallHash        string
	AccountID       int64
	APIKeyID        int64
	UserID          int64
	GroupID         int64
	SubscriptionID  int64
	LeaseID         string
	Model           string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	Controller      string
	ControllerOwner string
	UserAgent       string
	IPAddress       string
	InboundEndpoint string
	// RealtimePricePerMin 是开播时冻结的实时语音单价（元/分钟）；
	// 结算以它为准，nil 表示未配置，结算时必须拒绝。
	RealtimePricePerMin *float64
	// AttestationCiphertext 仅用于让同一会话的 Sideband 复用创建时的证明。
	AttestationCiphertext string
	// BillingPolicy 是开播时冻结的计费策略快照，随记录持久化。
	BillingPolicy *LiveBillingPolicy
	// Settlement 记录结算状态；nil 表示尚未开始结算。
	Settlement                 *LiveSettlement
	settlementIdentityRejected bool
}

type LiveCallCreated struct {
	SDP      []byte
	CallID   string
	Location string
	Account  *Account
}

// LiveCallStore 由 GatewayCache 的 Redis 实现可选提供，避免扩大旧缓存接口。
type LiveCallStore interface {
	SaveLiveCall(ctx context.Context, record *LiveCallRecord, ttl time.Duration) error
	GetLiveCall(ctx context.Context, callHash string) (*LiveCallRecord, error)
	ClaimLiveController(ctx context.Context, callHash, controller, owner string) (bool, error)
	ReleaseLiveController(ctx context.Context, callHash, owner string) (bool, error)
	GetLiveController(ctx context.Context, callHash string) (string, error)
	MarkLiveCallClosed(ctx context.Context, callHash string, ttl time.Duration) (bool, error)
	BeginLiveSettlement(ctx context.Context, callHash string, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error)
	BeginLiveSettlementVerified(ctx context.Context, record *LiveCallRecord, candidate *LiveSettlement, ttl time.Duration) (*LiveSettlement, string, bool, error)
	CompareAndSwapLiveSettlement(ctx context.Context, callHash, expected string, next *LiveSettlement, ttl time.Duration) (bool, error)
	ClaimLiveSettlementAudit(ctx context.Context, callHash, owner string, ttl time.Duration) (bool, error)
	FinishLiveSettlementAudit(ctx context.Context, callHash, owner string, written bool) (bool, error)
}

type LiveConcurrencyCache interface {
	AcquireLiveLease(
		ctx context.Context,
		accountID int64,
		accountMax int,
		userID int64,
		userMax int,
		apiKeyID int64,
		leaseID string,
		replacingRegularSlots bool,
	) (bool, error)
	RefreshLiveLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) (bool, error)
	ReleaseLiveLease(ctx context.Context, accountID, userID, apiKeyID int64, leaseID string) error
}
