package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// TestBuildMissingPriceRejectionQuery 聚合查询必须与缺价判定口径一致：
// 状态码 503 + 缺价文案特征 + 按「模型 + 分组」聚合 + 窗口与条数上限。
func TestBuildMissingPriceRejectionQuery(t *testing.T) {
	start := time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	query, args := buildMissingPriceRejectionQuery(&service.MissingPriceRejectionFilter{
		StartTime: start,
		EndTime:   end,
		Limit:     50,
	})

	if !strings.Contains(query, "e.status_code = 503") {
		t.Fatalf("查询必须只取 503 的缺价拒绝，实际 SQL: %s", query)
	}
	if !strings.Contains(query, "GROUP BY 1, 2, 3") {
		t.Fatalf("必须按模型 + 分组聚合，实际 SQL: %s", query)
	}
	if !strings.Contains(query, "ops_error_logs") {
		t.Fatalf("必须复用既有错误日志表，实际 SQL: %s", query)
	}
	for _, marker := range service.MissingPriceRejectionMarkers {
		if !strings.Contains(query, "ILIKE") {
			t.Fatalf("必须按缺价文案过滤，实际 SQL: %s", query)
		}
		_ = marker
	}

	if len(args) != len(service.MissingPriceRejectionMarkers)+3 {
		t.Fatalf("参数个数应为 窗口起止 + 文案特征 + 条数，实际 %d", len(args))
	}
	if args[0] != start || args[1] != end {
		t.Fatalf("前两个参数应是窗口起止，实际 %v %v", args[0], args[1])
	}
	if args[len(args)-1] != 50 {
		t.Fatalf("最后一个参数应是条数上限，实际 %v", args[len(args)-1])
	}
	for _, arg := range args[2 : len(args)-1] {
		pattern, ok := arg.(string)
		if !ok || !strings.HasPrefix(pattern, "%") || !strings.HasSuffix(pattern, "%") {
			t.Fatalf("文案参数应是 ILIKE 模式，实际 %v", arg)
		}
	}
}
