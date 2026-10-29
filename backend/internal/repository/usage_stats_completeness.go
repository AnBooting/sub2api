package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Watermark, quarantine and statistics must describe the same committed state.
func (r *usageLogRepository) readUsageStatsSnapshot(ctx context.Context, read func(*usageLogRepository) error) error {
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return read(r) // A caller-owned transaction keeps its own isolation contract.
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := read(&usageLogRepository{sql: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// Existing published buckets are preserved on incomplete days, but must not be
// presented as current totals. A later safe range remains independently readable.
func (r *usageLogRepository) requireUsageStatsAvailable(ctx context.Context, start, end time.Time) error {
	var available bool
	if err := scanSingleRow(ctx, r.sql, `SELECT
		EXISTS (SELECT 1 FROM usage_stats_repair_control WHERE id = 1 AND timezone_name = $1)
		AND NOT EXISTS (SELECT 1 FROM usage_stats_repair_days
			WHERE state = 'quarantine' AND day_start < $3 AND day_end > $2)`,
		[]any{timezone.Name(), start, end}, &available); err != nil {
		return err
	}
	if !available {
		return service.ErrUsageStatsIncomplete
	}
	return nil
}
