package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// videoTaskRepository 是 video_tasks 表的 raw SQL 仓储。
//
// 为什么用 raw SQL 而不是 ent：本表是本批协议要求的最小生命周期表
// （PRD-06 §2.4），字段与状态机都很小，raw SQL 足以表达且不需要改动
// ent schema（避免牵动 go generate 与既有生成物）。
type videoTaskRepository struct {
	db *sql.DB
}

// NewVideoTaskRepository 创建视频任务仓储。
//
// db 为 nil（测试或降级部署）时返回一个所有方法都 fail closed 的实现，
// 而不是在构造期 panic：调用方据此返回 VIDEO_TASK_UNAVAILABLE。
func NewVideoTaskRepository(db *sql.DB) service.VideoTaskRepository {
	return &videoTaskRepository{db: db}
}

const videoTaskColumns = `
	id, task_no, user_id, api_key_id, group_id, platform, model, billing_model,
	billing_mode, upstream_task_id, account_id, billing_snapshot, status, attempts,
	COALESCE(last_error, ''), last_checked_at, next_check_at, settled_at,
	created_at, updated_at`

func (r *videoTaskRepository) ensureDB() error {
	if r == nil || r.db == nil {
		return service.ErrVideoTaskStoreNotConfigured
	}
	return nil
}

// Create 插入任务；同一 (user_id, api_key_id, upstream_task_id) 已存在时回读既有行。
//
// 幂等语义与 ImageTaskService 的 Create 一致：创建路径重放不会产生第二行，
// 也不会把已经推进过的任务重置回 processing。
func (r *videoTaskRepository) Create(ctx context.Context, task *service.VideoTask) (*service.VideoTask, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if task == nil {
		return nil, service.ErrVideoTaskUnavailable
	}
	snapshot := task.BillingSnapshot
	if len(snapshot) == 0 {
		snapshot = json.RawMessage("{}")
	}
	const query = `
INSERT INTO video_tasks (
	task_no, user_id, api_key_id, group_id, platform, model, billing_model,
	billing_mode, upstream_task_id, account_id, billing_snapshot, status, next_check_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
ON CONFLICT (user_id, api_key_id, upstream_task_id) DO NOTHING
RETURNING ` + videoTaskColumns

	row := r.db.QueryRowContext(ctx, query,
		task.TaskNo, task.UserID, task.APIKeyID, task.GroupID,
		videoTaskNullString(task.Platform), videoTaskNullString(task.Model), videoTaskNullString(task.BillingModel),
		videoTaskNullString(task.BillingMode), task.UpstreamTaskID, nullInt64Ptr(task.AccountID),
		[]byte(snapshot), service.VideoTaskStatusProcessing,
	)
	created, err := scanVideoTask(row)
	if err == nil {
		return created, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	// DO NOTHING 命中：回读既有行，保持其推进进度。
	return r.findByOwnerAndUpstream(ctx, task.UserID, task.APIKeyID, task.UpstreamTaskID)
}

func (r *videoTaskRepository) findByOwnerAndUpstream(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string) (*service.VideoTask, error) {
	const query = `SELECT ` + videoTaskColumns + `
	FROM video_tasks WHERE user_id = $1 AND api_key_id = $2 AND upstream_task_id = $3`
	return scanVideoTask(r.db.QueryRowContext(ctx, query, userID, apiKeyID, upstreamTaskID))
}

// GetByTaskNo 按本站任务号查询；未命中返回 (nil, nil) 由上层映射为 404。
func (r *videoTaskRepository) GetByTaskNo(ctx context.Context, taskNo string) (*service.VideoTask, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	const query = `SELECT ` + videoTaskColumns + ` FROM video_tasks WHERE task_no = $1`
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, query, strings.TrimSpace(taskNo)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return task, err
}

// ListDue 返回未终态且已到检查时刻的任务，按到期顺序取一批。
//
// next_check_at 为 NULL 视为立即到期（新建任务首次查询不需要等待退避）。
func (r *videoTaskRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]*service.VideoTask, error) {
	if err := r.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	const query = `SELECT ` + videoTaskColumns + `
	FROM video_tasks
	WHERE status = 'processing' AND (next_check_at IS NULL OR next_check_at <= $1)
	ORDER BY COALESCE(next_check_at, created_at) ASC
	LIMIT $2`
	rows, err := r.db.QueryContext(ctx, query, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]*service.VideoTask, 0, limit)
	for rows.Next() {
		task, scanErr := scanVideoTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if task != nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, rows.Err()
}

// MarkChecked 记录一次尝试并安排下次检查；保持 processing 不变。
func (r *videoTaskRepository) MarkChecked(ctx context.Context, id int64, attempts int, nextCheckAt time.Time, lastError string) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	if attempts < 0 {
		attempts = 0
	}
	const query = `
UPDATE video_tasks
SET attempts = $2, last_error = $3, last_checked_at = NOW(), next_check_at = $4, updated_at = NOW()
WHERE id = $1 AND status = 'processing'`
	_, err := r.db.ExecContext(ctx, query, id, attempts, videoTaskNullString(lastError), nextCheckAt.UTC())
	return err
}

// MarkCompleted 仅当仍处于 processing 时写入 completed。
//
// 返回 false 表示另一路（客户端轮询或并发的 worker）已经推进过该任务——
// 这正是「只结算一次」的表级判据，与 Redis SetNX 占位互为双保险。
func (r *videoTaskRepository) MarkCompleted(ctx context.Context, id int64, settledAt time.Time) (bool, error) {
	return r.markTerminal(ctx, id, service.VideoTaskStatusCompleted, settledAt, "")
}

// MarkFailed 仅当仍处于 processing 时写入 failed。
func (r *videoTaskRepository) MarkFailed(ctx context.Context, id int64, settledAt time.Time, reason string) (bool, error) {
	return r.markTerminal(ctx, id, service.VideoTaskStatusFailed, settledAt, reason)
}

func (r *videoTaskRepository) markTerminal(ctx context.Context, id int64, status string, settledAt time.Time, reason string) (bool, error) {
	if err := r.ensureDB(); err != nil {
		return false, err
	}
	const query = `
UPDATE video_tasks
SET status = $2, settled_at = $3, last_checked_at = NOW(),
    last_error = CASE WHEN $4 = '' THEN last_error ELSE $4 END,
    next_check_at = NULL, updated_at = NOW()
WHERE id = $1 AND status = 'processing'`
	result, err := r.db.ExecContext(ctx, query, id, status, settledAt.UTC(), strings.TrimSpace(reason))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// AttachBilling 补写创建时计费事实；仅在任务仍 processing 时生效。
//
// 用 COALESCE/NULLIF 而不是整列覆盖：后到的补写不会把先前已写入的模型/快照
// 清空（创建路径分两步调用，任一步可能先到）。
func (r *videoTaskRepository) AttachBilling(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string, patch service.VideoTaskBillingPatch) error {
	if err := r.ensureDB(); err != nil {
		return err
	}
	snapshot := patch.BillingSnapshot
	if len(snapshot) == 0 {
		snapshot = json.RawMessage("{}")
	}
	const query = `
UPDATE video_tasks
SET model = COALESCE(NULLIF($4, ''), model),
    billing_model = COALESCE(NULLIF($5, ''), billing_model),
    billing_mode = COALESCE(NULLIF($6, ''), billing_mode),
    billing_snapshot = CASE WHEN $7::jsonb = '{}'::jsonb THEN billing_snapshot ELSE $7::jsonb END,
    updated_at = NOW()
WHERE user_id = $1 AND api_key_id = $2 AND upstream_task_id = $3 AND status = 'processing'`
	_, err := r.db.ExecContext(ctx, query,
		userID, apiKeyID, strings.TrimSpace(upstreamTaskID),
		strings.TrimSpace(patch.Model), strings.TrimSpace(patch.BillingModel),
		strings.TrimSpace(patch.BillingMode), []byte(snapshot),
	)
	return err
}

// videoTaskScanner 抽象 *sql.Row 与 *sql.Rows 的公共读取能力。
type videoTaskScanner interface {
	Scan(dest ...any) error
}

func scanVideoTask(scanner videoTaskScanner) (*service.VideoTask, error) {
	var (
		task        service.VideoTask
		accountID   sql.NullInt64
		snapshot    []byte
		lastChecked sql.NullTime
		nextCheckAt sql.NullTime
		settledAt   sql.NullTime
	)
	if err := scanner.Scan(
		&task.ID, &task.TaskNo, &task.UserID, &task.APIKeyID, &task.GroupID,
		&task.Platform, &task.Model, &task.BillingModel, &task.BillingMode,
		&task.UpstreamTaskID, &accountID, &snapshot, &task.Status, &task.Attempts,
		&task.LastError, &lastChecked, &nextCheckAt, &settledAt,
		&task.CreatedAt, &task.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if accountID.Valid && accountID.Int64 > 0 {
		value := accountID.Int64
		task.AccountID = &value
	}
	if len(snapshot) > 0 {
		task.BillingSnapshot = json.RawMessage(snapshot)
	} else {
		task.BillingSnapshot = json.RawMessage("{}")
	}
	if lastChecked.Valid {
		value := lastChecked.Time
		task.LastCheckedAt = &value
	}
	if nextCheckAt.Valid {
		value := nextCheckAt.Time
		task.NextCheckAt = &value
	}
	if settledAt.Valid {
		value := settledAt.Time
		task.SettledAt = &value
	}
	return &task, nil
}

// videoTaskNullString 把空串写成 NULL：列语义上「未设置」与「空串」不同。
// 刻意用本文件私有名，避免与同包其它仓储的 helper 撞名。
func videoTaskNullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
