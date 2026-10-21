package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// buildMissingPriceRejectionQuery 构造缺价拒绝的聚合查询（纯函数，便于单测）。
//
// 口径与服务层 IsMissingPriceRejection 一致：
//   - 状态码固定为 503（缺价拒绝的固定状态码）；
//   - 文案命中同一批缺价特征（MissingPriceRejectionMarkers）；
//   - 聚合维度是「请求模型（优先）+ 分组」，与错误列表展示同义。
func buildMissingPriceRejectionQuery(filter *service.MissingPriceRejectionFilter) (string, []any) {
	args := []any{filter.StartTime, filter.EndTime}
	clauses := make([]string, 0, len(service.MissingPriceRejectionMarkers))
	for _, marker := range service.MissingPriceRejectionMarkers {
		args = append(args, "%"+strings.ToLower(marker)+"%")
		clauses = append(clauses, "e.error_message ILIKE $"+strconv.Itoa(len(args)))
	}

	query := "SELECT " +
		"COALESCE(NULLIF(TRIM(e.requested_model), ''), e.model, '') AS model, " +
		"e.group_id, " +
		"COALESCE(g.name, '') AS group_name, " +
		"COUNT(*) AS rejection_count, " +
		"MIN(e.created_at) AS first_seen_at, " +
		"MAX(e.created_at) AS last_seen_at, " +
		"(array_agg(COALESCE(e.error_message, '') ORDER BY e.created_at DESC))[1] AS last_message " +
		"FROM ops_error_logs e " +
		"LEFT JOIN groups g ON e.group_id = g.id " +
		"WHERE e.created_at >= $1 AND e.created_at < $2 " +
		"AND e.status_code = " + strconv.Itoa(service.MissingPriceRejectionStatusCode) + " " +
		"AND (" + strings.Join(clauses, " OR ") + ") " +
		"GROUP BY 1, 2, 3 " +
		"ORDER BY rejection_count DESC, last_seen_at DESC " +
		"LIMIT $" + strconv.Itoa(len(args)+1)

	args = append(args, filter.Limit)
	return query, args
}

// AggregateMissingPriceRejections 按「模型 + 分组」聚合缺价拒绝清单。
//
// 复用既有的 ops_error_logs（审计机制），不新增表、不新增迁移。
func (r *opsRepository) AggregateMissingPriceRejections(ctx context.Context, filter *service.MissingPriceRejectionFilter) ([]*service.MissingPriceRejection, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	if filter == nil {
		return nil, fmt.Errorf("nil missing price rejection filter")
	}
	query, args := buildMissingPriceRejectionQuery(filter)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.MissingPriceRejection, 0, filter.Limit)
	for rows.Next() {
		var item service.MissingPriceRejection
		var groupID sql.NullInt64
		if err := rows.Scan(
			&item.Model,
			&groupID,
			&item.GroupName,
			&item.Count,
			&item.FirstSeenAt,
			&item.LastSeenAt,
			&item.LastMessage,
		); err != nil {
			return nil, err
		}
		if groupID.Valid {
			value := groupID.Int64
			item.GroupID = &value
		}
		out = append(out, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
