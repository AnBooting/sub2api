package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// groupUsageRollupSnapshot 是一次汇总水位的快照。
// closedBefore / retainedFrom 在水位无效时退化为 1970-01-01，
// 与此前 SQL 里 CASE WHEN valid 的语义一一对应。
type groupUsageRollupSnapshot struct {
	valid        bool
	closedBefore string    // date，历史日桶的右开边界
	retainedDate string    // retained_from 在服务端时区内的日期，历史日桶的左闭边界
	tailStart    time.Time // 尾段（还没进日桶的那部分 usage_logs）的起点
}

// readGroupUsageRollupSnapshot 读一次汇总水位并在 Go 侧判定有效性。
//
// 判定条件与原先 SQL 里的 state_values CTE 完全一致：
// 恰好一行、时区名与当前服务端配置相同、且水位不在未来。
func (r *usageLogRepository) readGroupUsageRollupSnapshot(ctx context.Context, timezoneName, todayDate string) (groupUsageRollupSnapshot, error) {
	epoch := time.Unix(0, 0).UTC()
	invalid := groupUsageRollupSnapshot{
		closedBefore: "1970-01-01",
		retainedDate: "1970-01-01",
		tailStart:    epoch,
	}

	var rowCount int
	var closedBefore sql.NullString
	var retainedFrom sql.NullTime
	var stateTimezone sql.NullString
	if err := scanSingleRow(ctx, r.sql, `
		SELECT
			COUNT(*),
			MAX(closed_before)::text,
			MAX(retained_from),
			MAX(timezone_name)
		FROM usage_group_rollup_state
		WHERE id = 1
	`, nil, &rowCount, &closedBefore, &retainedFrom, &stateTimezone); err != nil {
		return groupUsageRollupSnapshot{}, fmt.Errorf("读取分组用量汇总水位: %w", err)
	}
	if rowCount != 1 || !closedBefore.Valid || !retainedFrom.Valid ||
		!stateTimezone.Valid || closedBefore.String > todayDate {
		return invalid, nil
	}
	if stateTimezone.String != timezoneName {
		// 时区变更时不得用配置时区重新解释旧日桶：任何数字都是误导，
		// 必须 fail-closed，交由对账/补正流程处理。
		return groupUsageRollupSnapshot{}, fmt.Errorf(
			"group usage rollup timezone mismatch: state=%s configured=%s (reconciliation required)",
			stateTimezone.String, timezoneName)
	}

	tailStart, err := service.ParseGroupUsageDate(closedBefore.String)
	if err != nil {
		// 水位值解析不出来，按无效处理：宁可多扫一次，也不要算错钱。
		return invalid, nil //nolint:nilerr // 与上面的 valid 判定同语义，均降级为全量重算
	}
	return groupUsageRollupSnapshot{
		valid:        true,
		closedBefore: closedBefore.String,
		retainedDate: service.GroupUsageDate(retainedFrom.Time),
		tailStart:    tailStart.UTC(),
	}, nil
}

func (r *usageLogRepository) getAllGroupUsageSummaryFromRollups(ctx context.Context, todayStart time.Time) (results []usagestats.GroupUsageSummary, err error) {
	todayStart = service.GroupUsageTodayStart(todayStart)
	yesterdayStart := service.GroupUsageYesterdayStart(todayStart)
	timezoneName := service.GroupUsageTimezoneName()
	todayDate := service.GroupUsageDate(todayStart)
	yesterdayDate := service.GroupUsageDate(yesterdayStart)

	state, err := r.readGroupUsageRollupSnapshot(ctx, timezoneName, todayDate)
	if err != nil {
		return nil, err
	}

	// 尾段起点必须以**查询参数**的形式给进来。
	//
	// 此前它是在同一条 SQL 里由 state CTE 算出、再 CROSS JOIN 给 tail 用的，
	// 于是 created_at 的下界对 planner 来说是个运行期才知道的值：既进不了
	// index cond，也估不准选择性，只能退化成 usage_logs 全表扫。
	// 生产实测（1721 万行 / PostgreSQL 17）：
	//   Seq Scan on usage_logs … rows=17212870, Rows Removed by Join Filter: 17178082
	//   Execution Time: 48720 ms
	// 换成参数之后走 idx_usage_logs_created_at：
	//   Index Scan … Index Cond: (created_at >= $7)
	//   Execution Time: 34 ms
	const query = `
		WITH historical AS (
			SELECT
				rollup.group_id,
				COALESCE(SUM(rollup.actual_cost), 0) AS actual_cost,
				COALESCE(SUM(rollup.actual_cost) FILTER (
					WHERE rollup.bucket_date = $3::date
				), 0) AS yesterday_cost
			FROM usage_group_daily_rollups rollup
			WHERE $4::boolean
				AND rollup.bucket_date >= $5::date
				AND rollup.bucket_date < $6::date
			GROUP BY rollup.group_id
		),
		tail AS (
			SELECT
				ul.group_id,
				COALESCE(SUM(ul.actual_cost), 0) AS actual_cost,
				COALESCE(SUM(ul.actual_cost) FILTER (WHERE ul.created_at >= $1), 0) AS today_cost,
				COALESCE(SUM(ul.actual_cost) FILTER (
					WHERE ul.created_at >= $2
						AND ul.created_at < $1
				), 0) AS yesterday_cost
			FROM usage_logs ul
			WHERE ul.created_at >= $7
			GROUP BY ul.group_id
		)
		SELECT
			g.id AS group_id,
			COALESCE(historical.actual_cost, 0) + COALESCE(tail.actual_cost, 0) AS total_cost,
			COALESCE(tail.today_cost, 0) AS today_cost,
			COALESCE(historical.yesterday_cost, 0) + COALESCE(tail.yesterday_cost, 0) AS yesterday_cost
		FROM groups g
		LEFT JOIN historical ON historical.group_id = g.id
		LEFT JOIN tail ON tail.group_id = g.id
		ORDER BY g.id
	`

	rows, err := r.sql.QueryContext(
		ctx,
		query,
		todayStart,
		yesterdayStart,
		yesterdayDate,
		state.valid,
		state.retainedDate,
		state.closedBefore,
		state.tailStart,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]usagestats.GroupUsageSummary, 0)
	for rows.Next() {
		var row usagestats.GroupUsageSummary
		if err := rows.Scan(&row.GroupID, &row.TotalCost, &row.TodayCost, &row.YesterdayCost); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// SyncGroupUsageRollups 将服务端配置时区今日以前的用量发布为分组日桶。
func (r *dashboardAggregationRepository) SyncGroupUsageRollups(ctx context.Context, todayStart time.Time) error {
	if r == nil || r.sql == nil {
		return nil
	}
	todayStart = service.GroupUsageTodayStart(todayStart)
	if db, ok := r.sql.(*sql.DB); ok {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		txRepo := newDashboardAggregationRepositoryWithSQL(tx)
		if err := txRepo.syncGroupUsageRollupsInTx(ctx, todayStart); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	// 非事务执行器无法保证「水位+完整性控制+桶」的原子发布，fail-closed。
	return fmt.Errorf("cannot sync group usage rollups without a database transaction")
}

func (r *dashboardAggregationRepository) syncGroupUsageRollupsInTx(ctx context.Context, todayStart time.Time) error {
	// 同步必须在事务内执行：水位、完整性控制与桶三者原子发布。
	tx, ok := r.sql.(*sql.Tx)
	if !ok {
		return fmt.Errorf("cannot sync group usage rollups without a database transaction")
	}

	var closedBefore string
	var previousRetainedFrom time.Time
	var stateTimezoneName string
	if err := scanSingleRow(ctx, r.sql, `SELECT closed_before::text, retained_from, timezone_name
		FROM usage_group_rollup_state
		WHERE id = 1
		FOR UPDATE`, nil, &closedBefore, &previousRetainedFrom, &stateTimezoneName); err != nil {
		return fmt.Errorf("读取分组用量汇总水位: %w", err)
	}

	todayDate := service.GroupUsageDate(todayStart)
	timezoneName := service.GroupUsageTimezoneName()
	timezoneChanged := stateTimezoneName != timezoneName

	// 完整性控制行缺失或时区不匹配时不得发布/重算：宁可保持旧桶与旧水位，
	// 也不要用不完整明细覆盖已发布历史或推进水位到未知时区。
	control, err := readUsageRepairControl(ctx, tx)
	if err != nil {
		return fmt.Errorf("读取用量统计完整性控制: %w", err)
	}
	if control.timezoneName != timezoneName {
		return fmt.Errorf("用量统计完整性控制时区不匹配: %s != %s", control.timezoneName, timezoneName)
	}

	var closedTime time.Time
	if !timezoneChanged {
		var err error
		closedTime, err = service.ParseGroupUsageDate(closedBefore)
		if err != nil {
			return fmt.Errorf("解析分组用量汇总水位 %q: %w", closedBefore, err)
		}
		todayDateTime, err := service.ParseGroupUsageDate(todayDate)
		if err != nil {
			return err
		}
		if closedTime.After(todayDateTime) {
			return fmt.Errorf("分组用量汇总水位位于未来: %s", closedBefore)
		}
		if closedBefore == todayDate {
			return nil
		}
	}

	// 最早可重算日必须是 raw_safe_from 所在**完整日**的起点：
	// 若 raw_safe_from 恰在本地零点则当天可用，否则（迁移发生在当天中途）
	// 当天明细不完整，只能从下一天开始重建。
	safeFromDay := service.GroupUsageDate(control.rawSafeFrom)
	safeFromDayTime, err := service.ParseGroupUsageDate(safeFromDay)
	if err != nil {
		return err
	}
	earliestSafe := safeFromDayTime
	if control.rawSafeFrom.After(safeFromDayTime) {
		earliestSafe = service.GroupUsageTodayStart(safeFromDayTime.AddDate(0, 0, 1))
	}
	rebuildStartDate := service.GroupUsageDate(earliestSafe)
	if !timezoneChanged && closedTime.After(earliestSafe) {
		rebuildStartDate = closedBefore
	}
	rebuildStart, err := service.ParseGroupUsageDate(rebuildStartDate)
	if err != nil {
		return err
	}

	// 已发布水位与 safe 起点之间的不完整区间持久隔离，禁止重算覆盖。
	if !timezoneChanged && rebuildStart.After(closedTime) {
		quarantineStart := closedTime
		for day := quarantineStart; day.Before(rebuildStart); day = day.AddDate(0, 0, 1) {
			if err := quarantineUsageRepairDay(ctx, tx, day, day.AddDate(0, 0, 1), "raw_history_incomplete"); err != nil {
				return err
			}
		}
	}
	if timezoneChanged {
		// 跨时区：不重算旧桶、不推进水位，持久隔离待人工/对账确认。
		quarantineStart, err := service.ParseGroupUsageDate(closedBefore)
		if err != nil {
			return err
		}
		for day := quarantineStart; day.Before(todayStart); day = day.AddDate(0, 0, 1) {
			if err := quarantineUsageRepairDay(ctx, tx, day, day.AddDate(0, 0, 1), "timezone_changed"); err != nil {
				return err
			}
		}
		return nil
	}

	// 水位最多推进到范围内首个未证实完整的隔离日（不含）：已发布水位绝不能在
	// 未证实完整的区间上越过。DELETE/重建也只覆盖该上界之前的连续安全区间。
	publishEnd := todayStart
	publishEndDate := todayDate
	quarantinedNext, err := loadQuarantinedRepairDays(ctx, tx, rebuildStart, todayStart)
	if err != nil {
		return err
	}
	for day := rebuildStart; day.Before(todayStart); day = day.AddDate(0, 0, 1) {
		if _, blocked := quarantinedNext[day.UTC()]; blocked {
			publishEnd = day
			publishEndDate = service.GroupUsageDate(day)
			break
		}
	}

	if _, err := r.sql.ExecContext(ctx, `DELETE FROM usage_group_daily_rollups
		WHERE bucket_date >= $1::date AND bucket_date < $2::date`, rebuildStartDate, publishEndDate); err != nil {
		return fmt.Errorf("清理分组用量日桶: %w", err)
	}

	if _, err := r.sql.ExecContext(ctx, `INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost, computed_at)
		SELECT
			(created_at AT TIME ZONE $3::text)::date AS bucket_date,
			group_id,
			COALESCE(SUM(actual_cost), 0) AS actual_cost,
			NOW()
		FROM usage_logs
		WHERE group_id IS NOT NULL
			AND created_at >= $1
			AND created_at < $2
		GROUP BY 1, 2
		ON CONFLICT (bucket_date, group_id)
		DO UPDATE SET
			actual_cost = EXCLUDED.actual_cost,
			computed_at = EXCLUDED.computed_at`, rebuildStart.UTC(), publishEnd.UTC(), timezoneName); err != nil {
		return fmt.Errorf("重建分组用量日桶: %w", err)
	}

	if _, err := r.sql.ExecContext(ctx, `UPDATE usage_group_rollup_state
		SET closed_before = $1::date,
			retained_from = $2,
			timezone_name = $3,
			updated_at = NOW()
		WHERE id = 1`, publishEndDate, previousRetainedFrom, timezoneName); err != nil {
		return fmt.Errorf("更新分组用量汇总水位: %w", err)
	}
	return nil
}
func lockGroupUsageRollupState(ctx context.Context, tx *sql.Tx) error {
	var id int16
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM usage_group_rollup_state
		WHERE id = 1
		FOR UPDATE
	`).Scan(&id); err != nil {
		return fmt.Errorf("锁定分组用量汇总水位: %w", err)
	}
	return nil
}

func invalidateGroupUsageRollupsAt(ctx context.Context, tx *sql.Tx, affectedAt time.Time) error {
	timezoneName := service.GroupUsageTimezoneName()
	_, err := tx.ExecContext(ctx, `
		UPDATE usage_group_rollup_state
		SET closed_before = LEAST(
			closed_before,
			($1::timestamptz AT TIME ZONE $2::text)::date
		),
			updated_at = NOW()
		WHERE id = 1
	`, affectedAt.UTC(), timezoneName)
	return err
}
