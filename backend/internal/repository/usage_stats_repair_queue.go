package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 队列 SQL 常量：测试直接引用以校验占位符与实参 1:1，防止 sqlmock 字符串匹配
// 覆盖不到的真实 PG 参数绑定错误（占位符语义冲突/个数不匹配）。

// 队列 SQL 常量：测试直接引用以校验占位符与实参 1:1，防止 sqlmock 字符串匹配
// 覆盖不到的真实 PG 参数绑定错误（占位符语义冲突/个数不匹配）。
const usageStatsRepairClaimSQL = `WITH due AS (
			SELECT timezone_name, day_start FROM usage_stats_repair_days
			WHERE state = 'dirty' AND next_retry_at <= clock_timestamp()
			  AND (lease_until IS NULL OR lease_until <= clock_timestamp())
			ORDER BY next_retry_at, day_start LIMIT $1 FOR UPDATE SKIP LOCKED
			)
		UPDATE usage_stats_repair_days AS d SET
			lease_until = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
			claim_owner = $2,
			claim_fence = d.claim_fence + 1,
			attempts = d.attempts + 1, updated_at = clock_timestamp()
		FROM due
		WHERE d.timezone_name = due.timezone_name AND d.day_start = due.day_start
		RETURNING d.timezone_name, d.day_start, d.day_end, d.generation, d.attempts, d.reason_code, d.claim_owner, d.claim_fence`

const usageStatsRepairAckSQL = `UPDATE usage_stats_repair_days
		SET state = 'clean', applied_generation = $3, attempts = 0, next_retry_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE timezone_name = $1 AND day_start = $2 AND generation = $3
		  AND claim_owner = $4 AND claim_fence = $5 AND state = 'dirty'`

const usageStatsRepairFailSQL = `UPDATE usage_stats_repair_days
		SET state = CASE WHEN attempts >= $6 THEN 'quarantine' ELSE 'dirty' END,
		    reason_code = CASE WHEN attempts >= $6 THEN $7 ELSE reason_code END,
		    next_retry_at = clock_timestamp() + ($8::bigint * INTERVAL '1 millisecond'),
		    lease_until = NULL, updated_at = clock_timestamp()
		WHERE timezone_name = $1 AND day_start = $2 AND generation = $3
		  AND claim_owner = $4 AND claim_fence = $5 AND state = 'dirty'`

type usageStatsRepairQueueSQL struct {
	db *sql.DB
}

// NewUsageStatsRepairQueue 返回基于 PostgreSQL 的脏日队列。
func NewUsageStatsRepairQueue(db *sql.DB) service.UsageStatsRepairQueue {
	if db == nil {
		return nil
	}
	return &usageStatsRepairQueueSQL{db: db}
}

func (r *usageStatsRepairQueueSQL) ClaimDue(ctx context.Context, owner string, limit int, ttl time.Duration) ([]service.UsageRepairDayClaim, error) {
	if owner == "" || limit <= 0 || limit > 1000 || ttl < time.Second || ttl > 24*time.Hour {
		return nil, service.ErrUsageRepairInvalid
	}
	if r == nil || r.db == nil {
		return nil, service.ErrUsageRepairStoreUnavailable
	}
	rows, err := r.db.QueryContext(ctx, `WITH due AS (
			SELECT timezone_name, day_start FROM usage_stats_repair_days
			WHERE state = 'dirty' AND next_retry_at <= clock_timestamp()
			  AND (lease_until IS NULL OR lease_until <= clock_timestamp())
			ORDER BY next_retry_at, day_start LIMIT $1 FOR UPDATE SKIP LOCKED
			)
		UPDATE usage_stats_repair_days AS d SET
			lease_until = clock_timestamp() + ($3::bigint * INTERVAL '1 millisecond'),
			claim_owner = $2,
			claim_fence = d.claim_fence + 1,
			attempts = d.attempts + 1, updated_at = clock_timestamp()
		FROM due
		WHERE d.timezone_name = due.timezone_name AND d.day_start = due.day_start
		RETURNING d.timezone_name, d.day_start, d.day_end, d.generation, d.attempts, d.reason_code, d.claim_owner, d.claim_fence`,
		limit, owner, ttl.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claims := make([]service.UsageRepairDayClaim, 0)
	for rows.Next() {
		var c service.UsageRepairDayClaim
		if err := rows.Scan(&c.TimezoneName, &c.DayStart, &c.DayEnd, &c.Generation, &c.Attempts, &c.ReasonCode, &c.Owner, &c.Fence); err != nil {
			return nil, err
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return claims, nil
}

func (r *usageStatsRepairQueueSQL) Ack(ctx context.Context, timezoneName string, dayStart time.Time, generation int64, owner string, fence int64) error {
	if timezoneName == "" || dayStart.IsZero() || generation <= 0 || owner == "" || fence <= 0 {
		return service.ErrUsageRepairInvalid
	}
	if r == nil || r.db == nil {
		return service.ErrUsageRepairStoreUnavailable
	}
	result, err := r.db.ExecContext(ctx, `UPDATE usage_stats_repair_days
		SET state = 'clean', applied_generation = $3, attempts = 0, next_retry_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE timezone_name = $1 AND day_start = $2 AND generation = $3
		  AND claim_owner = $4 AND claim_fence = $5 AND state = 'dirty'`,
		timezoneName, dayStart.UTC(), generation, owner, fence)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrUsageRepairLostLease
	}
	return nil
}

func (r *usageStatsRepairQueueSQL) Fail(ctx context.Context, timezoneName string, dayStart time.Time, generation int64, owner string, fence int64, reason string, backoff time.Duration) error {
	if timezoneName == "" || dayStart.IsZero() || generation <= 0 || owner == "" || fence <= 0 {
		return service.ErrUsageRepairInvalid
	}
	if r == nil || r.db == nil {
		return service.ErrUsageRepairStoreUnavailable
	}
	backoffMs := backoff.Milliseconds()
	if backoffMs < 1000 {
		backoffMs = 1000
	}
	result, err := r.db.ExecContext(ctx, `UPDATE usage_stats_repair_days
		SET state = CASE WHEN attempts >= $6 THEN 'quarantine' ELSE 'dirty' END,
		    reason_code = CASE WHEN attempts >= $6 THEN $7 ELSE reason_code END,
		    next_retry_at = clock_timestamp() + ($8::bigint * INTERVAL '1 millisecond'),
		    lease_until = NULL, updated_at = clock_timestamp()
		WHERE timezone_name = $1 AND day_start = $2 AND generation = $3
		  AND claim_owner = $4 AND claim_fence = $5 AND state = 'dirty'`,
		timezoneName, dayStart.UTC(), generation, owner, fence, service.UsageStatsRepairMaxAttempts, reason, backoffMs)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrUsageRepairLostLease
	}
	return nil
}
