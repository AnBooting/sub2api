// Package yunmao 提供白标充值中心的订单映射持久化。
//
// 设计边界（PRD-03 BE-02）：这里只保存「本站用户 ↔ 上游订单号 ↔ 商品快照 ↔
// 展示状态 ↔ 卡密密文」的最小映射，**不是**第二套支付/财务账本。
// 卡密只以密文入库（由 service.SecretEncryptor 产出），仓储层不接触明文。
package yunmao

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"

	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"
)

// OrderRepository 是基于 PostgreSQL 的订单映射仓储。
type OrderRepository struct {
	db *sql.DB
}

// NewOrderRepository 构造仓储。db 为 nil 时所有方法返回明确错误而不是 panic。
func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

const orderColumns = `
	id, order_no, user_id, idempotency_key, COALESCE(upstream_order_no, ''),
	product_id, product_snapshot, amount_cny::float8, quantity, display_status,
	COALESCE(upstream_status, ''), COALESCE(card_secret, ''), COALESCE(card_hint, ''),
	delivery_attempts, COALESCE(last_error_code, ''), last_queried_at, paid_at, delivered_at,
	created_at, updated_at`

func (r *OrderRepository) ensureDB() error {
	if r == nil || r.db == nil {
		return yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "订单存储未配置", nil)
	}
	return nil
}

// Create 写入一条订单映射；幂等键冲突时返回 ErrIdempotencyConflict（由调用方回读既有映射）。
func (r *OrderRepository) Create(ctx context.Context, record yunmaosvc.CreateOrderRecord) (*yunmaosvc.OrderRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	snapshot := record.ProductSnapshot
	if len(snapshot) == 0 {
		snapshot = json.RawMessage("{}")
	}
	status := record.DisplayStatus
	if !status.Valid() {
		status = yunmaosvc.DisplayPending
	}
	const query = `
INSERT INTO yunmao_orders (
	order_no, user_id, idempotency_key, upstream_order_no, product_id,
	product_snapshot, amount_cny, quantity, display_status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING ` + orderColumns
	row := r.db.QueryRowContext(ctx, query,
		record.OrderNo, record.UserID, record.IdempotencyKey, nullIfEmpty(record.UpstreamOrderNo),
		record.ProductID, []byte(snapshot), record.AmountCNY, record.Quantity, string(status),
	)
	created, err := scanOrder(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, yunmaosvc.ErrIdempotencyConflict
		}
		return nil, err
	}
	return created, nil
}

// FindByOrderNo 按本站订单号查询。
func (r *OrderRepository) FindByOrderNo(ctx context.Context, orderNo string) (*yunmaosvc.OrderRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	const query = `SELECT ` + orderColumns + ` FROM yunmao_orders WHERE order_no = $1`
	return r.findOne(ctx, query, strings.TrimSpace(orderNo))
}

// FindByIdempotencyKey 按用户 + 幂等键查询。
func (r *OrderRepository) FindByIdempotencyKey(ctx context.Context, userID int64, key string) (*yunmaosvc.OrderRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	const query = `SELECT ` + orderColumns + ` FROM yunmao_orders WHERE user_id = $1 AND idempotency_key = $2`
	return r.findOne(ctx, query, userID, strings.TrimSpace(key))
}

// FindByUpstreamOrderNo 按上游订单号查询（回调路径用）。
func (r *OrderRepository) FindByUpstreamOrderNo(ctx context.Context, upstreamOrderNo string) (*yunmaosvc.OrderRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	const query = `SELECT ` + orderColumns + ` FROM yunmao_orders WHERE upstream_order_no = $1`
	return r.findOne(ctx, query, strings.TrimSpace(upstreamOrderNo))
}

// Update 局部更新订单；nil 字段不改。
func (r *OrderRepository) Update(ctx context.Context, orderNo string, patch yunmaosvc.OrderPatch) (*yunmaosvc.OrderRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	sets := make([]string, 0, 12)
	args := make([]any, 0, 14)
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, column+" = $"+itoa(len(args)))
	}
	if patch.UpstreamOrderNo != nil {
		add("upstream_order_no", nullIfEmpty(*patch.UpstreamOrderNo))
	}
	if patch.DisplayStatus != nil {
		status := *patch.DisplayStatus
		if !status.Valid() {
			return nil, yunmaosvc.NewError(yunmaosvc.CodeUpstreamRejected, "展示状态非法", nil)
		}
		add("display_status", string(status))
	}
	if patch.UpstreamStatus != nil {
		add("upstream_status", nullIfEmpty(*patch.UpstreamStatus))
	}
	if patch.CardSecret != nil {
		add("card_secret", nullIfEmpty(*patch.CardSecret))
	}
	if patch.CardHint != nil {
		add("card_hint", nullIfEmpty(*patch.CardHint))
	}
	if patch.DeliveryAttempts != nil {
		add("delivery_attempts", *patch.DeliveryAttempts)
	}
	if patch.LastErrorCode != nil {
		add("last_error_code", nullIfEmpty(*patch.LastErrorCode))
	}
	if patch.LastQueriedAt != nil {
		add("last_queried_at", *patch.LastQueriedAt)
	}
	if patch.PaidAt != nil {
		add("paid_at", *patch.PaidAt)
	}
	if patch.DeliveredAt != nil {
		add("delivered_at", *patch.DeliveredAt)
	}
	if len(sets) == 0 {
		return r.FindByOrderNo(ctx, orderNo)
	}
	add("updated_at", time.Now().UTC())

	args = append(args, strings.TrimSpace(orderNo))
	query := `UPDATE yunmao_orders SET ` + strings.Join(sets, ", ") + ` WHERE order_no = $` + itoa(len(args)) + ` RETURNING ` + orderColumns
	row := r.db.QueryRowContext(ctx, query, args...)
	updated, err := scanOrder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, yunmaosvc.ErrOrderNotFound
		}
		return nil, err
	}
	return updated, nil
}

// ListByUser 列出某用户的订单（倒序）。
func (r *OrderRepository) ListByUser(ctx context.Context, userID int64, limit int) ([]yunmaosvc.OrderRecord, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	const query = `SELECT ` + orderColumns + ` FROM yunmao_orders WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`
	rows, err := r.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]yunmaosvc.OrderRecord, 0, limit)
	for rows.Next() {
		record, scanErr := scanOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadCatalog 读取商品目录快照；从未同步过返回 (nil, nil)。
func (r *OrderRepository) LoadCatalog(ctx context.Context) (*yunmaosvc.CatalogSnapshot, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	const query = `SELECT products, synced_at, sync_failed_at, COALESCE(sync_error_code, '') FROM yunmao_catalog_snapshots WHERE id = 1`
	var (
		raw          []byte
		syncedAt     time.Time
		syncFailedAt sql.NullTime
		syncErrCode  string
	)
	err := r.db.QueryRowContext(ctx, query).Scan(&raw, &syncedAt, &syncFailedAt, &syncErrCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot := &yunmaosvc.CatalogSnapshot{
		SyncedAt:      syncedAt,
		SyncErrorCode: syncErrCode,
	}
	if syncFailedAt.Valid {
		failedAt := syncFailedAt.Time
		snapshot.SyncFailedAt = &failedAt
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &snapshot.Products); err != nil {
			return nil, yunmaosvc.NewError(yunmaosvc.CodeUpstreamResponseInvalid, "商品快照解码失败", err)
		}
	}
	return snapshot, nil
}

// SaveCatalog 保存一次成功的商品同步：覆盖商品列表并清除失败标记。
func (r *OrderRepository) SaveCatalog(ctx context.Context, products []yunmaosvc.Product, syncedAt time.Time) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	if products == nil {
		products = []yunmaosvc.Product{}
	}
	payload, err := json.Marshal(products)
	if err != nil {
		return yunmaosvc.NewError(yunmaosvc.CodeUpstreamRejected, "商品快照序列化失败", err)
	}
	const query = `
INSERT INTO yunmao_catalog_snapshots (id, products, synced_at, sync_failed_at, sync_error_code)
VALUES (1, $1, $2, NULL, NULL)
ON CONFLICT (id) DO UPDATE SET
	products = EXCLUDED.products,
	synced_at = EXCLUDED.synced_at,
	sync_failed_at = NULL,
	sync_error_code = NULL`
	_, err = r.db.ExecContext(ctx, query, payload, syncedAt)
	return err
}

// MarkCatalogSyncFailure 记录同步失败；**必须**保留上一次成功数据（PRD-03 BE-08）。
func (r *OrderRepository) MarkCatalogSyncFailure(ctx context.Context, code yunmaosvc.ErrorCode, failedAt time.Time) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	// 用 INSERT ... ON CONFLICT 保证「首次同步就失败」也能落一条失败标记，
	// 同时 products 列保持默认空数组、绝不覆盖已有商品。
	const query = `
INSERT INTO yunmao_catalog_snapshots (id, products, synced_at, sync_failed_at, sync_error_code)
VALUES (1, '[]'::jsonb, $1, $1, $2)
ON CONFLICT (id) DO UPDATE SET
	sync_failed_at = EXCLUDED.sync_failed_at,
	sync_error_code = EXCLUDED.sync_error_code`
	_, err := r.db.ExecContext(ctx, query, failedAt, string(code))
	return err
}

func (r *OrderRepository) findOne(ctx context.Context, query string, args ...any) (*yunmaosvc.OrderRecord, error) {
	row := r.db.QueryRowContext(ctx, query, args...)
	record, err := scanOrder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, yunmaosvc.ErrOrderNotFound
		}
		return nil, err
	}
	return record, nil
}

// scanner 同时兼容 *sql.Row 与 *sql.Rows。
type scanner interface {
	Scan(dest ...any) error
}

func scanOrder(src scanner) (*yunmaosvc.OrderRecord, error) {
	var (
		record        yunmaosvc.OrderRecord
		rawSnapshot   []byte
		status        string
		lastQueriedAt sql.NullTime
		paidAt        sql.NullTime
		deliveredAt   sql.NullTime
	)
	err := src.Scan(
		&record.ID, &record.OrderNo, &record.UserID, &record.IdempotencyKey, &record.UpstreamOrderNo,
		&record.ProductID, &rawSnapshot, &record.AmountCNY, &record.Quantity, &status,
		&record.UpstreamStatus, &record.CardSecret, &record.CardHint,
		&record.DeliveryAttempts, &record.LastErrorCode, &lastQueriedAt, &paidAt, &deliveredAt,
		&record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	record.ProductSnapshot = json.RawMessage(rawSnapshot)
	record.DisplayStatus = yunmaosvc.DisplayStatus(status)
	if !record.DisplayStatus.Valid() {
		record.DisplayStatus = yunmaosvc.DisplayPending
	}
	if lastQueriedAt.Valid {
		value := lastQueriedAt.Time
		record.LastQueriedAt = &value
	}
	if paidAt.Valid {
		value := paidAt.Time
		record.PaidAt = &value
	}
	if deliveredAt.Valid {
		value := deliveredAt.Time
		record.DeliveredAt = &value
	}
	return &record, nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	pos := len(buf)
	for value > 0 {
		pos--
		buf[pos] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
