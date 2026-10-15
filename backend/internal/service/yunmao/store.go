package yunmao

import (
	"context"
	"encoding/json"
	"time"
)

// OrderRecord 是本站侧的最小订单映射（对应迁移 248 的 yunmao_orders）。
//
// 它**不是**第二套订单/支付账本：只保留找回订单、展示卡密、恢复查询与追踪异常
// 所需的最小字段（PRD-03 §2.2 / BE-02）。
type OrderRecord struct {
	ID              int64
	OrderNo         string
	UserID          int64
	IdempotencyKey  string
	UpstreamOrderNo string
	ProductID       string
	ProductSnapshot json.RawMessage
	AmountCNY       float64
	Quantity        int
	DisplayStatus   DisplayStatus
	UpstreamStatus  string
	// CardSecret 是卡密**密文**（SecretEncryptor 输出）。仓储层与日志绝不接触明文。
	CardSecret string
	// CardHint 是脱敏提示，可安全展示。
	CardHint         string
	DeliveryAttempts int
	LastErrorCode    string
	LastQueriedAt    *time.Time
	PaidAt           *time.Time
	DeliveredAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CreateOrderRecord 是新建订单的入参。
type CreateOrderRecord struct {
	OrderNo         string
	UserID          int64
	IdempotencyKey  string
	UpstreamOrderNo string
	ProductID       string
	ProductSnapshot json.RawMessage
	AmountCNY       float64
	Quantity        int
	DisplayStatus   DisplayStatus
}

// OrderPatch 是订单更新的可选字段集合（nil = 不改）。
//
// 用可选字段而不是整行覆盖，是为了让「查单只更新状态」「取卡密只更新卡密」
// 这类并发场景不会互相把对方刚写入的字段抹掉。
type OrderPatch struct {
	UpstreamOrderNo  *string
	DisplayStatus    *DisplayStatus
	UpstreamStatus   *string
	CardSecret       *string
	CardHint         *string
	DeliveryAttempts *int
	LastErrorCode    *string
	LastQueriedAt    *time.Time
	PaidAt           *time.Time
	DeliveredAt      *time.Time
}

// CatalogSnapshot 是商品目录快照（对应 yunmao_catalog_snapshots 单行）。
type CatalogSnapshot struct {
	Products      []Product
	SyncedAt      time.Time
	SyncFailedAt  *time.Time
	SyncErrorCode string
}

// OrderStore 是订单映射的持久化契约。
type OrderStore interface {
	// Create 写入一条新订单。幂等键冲突时必须返回 ErrIdempotencyConflict。
	Create(ctx context.Context, record CreateOrderRecord) (*OrderRecord, error)
	// FindByOrderNo 按本站订单号查询。不存在返回 ErrOrderNotFound。
	FindByOrderNo(ctx context.Context, orderNo string) (*OrderRecord, error)
	// FindByIdempotencyKey 按幂等键查询。不存在返回 ErrOrderNotFound。
	FindByIdempotencyKey(ctx context.Context, userID int64, key string) (*OrderRecord, error)
	// FindByUpstreamOrderNo 按上游订单号查询（回调路径用）。不存在返回 ErrOrderNotFound。
	FindByUpstreamOrderNo(ctx context.Context, upstreamOrderNo string) (*OrderRecord, error)
	// Update 局部更新订单。
	Update(ctx context.Context, orderNo string, patch OrderPatch) (*OrderRecord, error)
	// ListByUser 按用户列出订单（倒序）。
	ListByUser(ctx context.Context, userID int64, limit int) ([]OrderRecord, error)
	// LoadCatalog 读取商品目录快照；从未同步过时返回 (nil, nil)。
	LoadCatalog(ctx context.Context) (*CatalogSnapshot, error)
	// SaveCatalog 保存一次**成功**的商品同步结果。
	SaveCatalog(ctx context.Context, products []Product, syncedAt time.Time) error
	// MarkCatalogSyncFailure 记录一次同步失败；必须保留上一次成功数据（PRD-03 BE-08）。
	MarkCatalogSyncFailure(ctx context.Context, code ErrorCode, failedAt time.Time) error
}

// ErrOrderNotFound 表示订单不存在或不属于当前用户（两者同一错误，避免探测存在性）。
var ErrOrderNotFound = NewError(CodeOrderNotFound, "订单不存在", nil)

// ErrIdempotencyConflict 表示同一幂等键被不同内容复用。
var ErrIdempotencyConflict = NewError(CodeIdempotencyConflict, "重复的请求标识", nil)
