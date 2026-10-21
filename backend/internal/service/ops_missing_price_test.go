package service

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// missingPriceRepoStub 只实现缺价聚合，其余方法由嵌入的接口兜底（未实现即 panic）。
type missingPriceRepoStub struct {
	OpsRepository
	gotFilter *MissingPriceRejectionFilter
	items     []*MissingPriceRejection
}

func (s *missingPriceRepoStub) AggregateMissingPriceRejections(_ context.Context, filter *MissingPriceRejectionFilter) ([]*MissingPriceRejection, error) {
	s.gotFilter = filter
	return s.items, nil
}

// TestIsMissingPriceRejection 缺价判定口径：503 + 缺价文案；其它错误不算缺价。
func TestIsMissingPriceRejection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		message string
		want    bool
	}{
		{"缺价：未找到价格", http.StatusServiceUnavailable, "no pricing available for model: gpt-x: pricing not found", true},
		{"缺价：不可用文案", http.StatusServiceUnavailable, "Model pricing is unavailable. Please retry later.", true},
		{"缺价：带上错误码", http.StatusServiceUnavailable, "pricing_unavailable", true},
		{"普通 503 不算缺价", http.StatusServiceUnavailable, "upstream returned 503", false},
		{"缺价文案但不是 503", http.StatusBadRequest, "pricing not found", false},
		{"空文案", http.StatusServiceUnavailable, "  ", false},
		{"其它错误", http.StatusInternalServerError, "boom", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMissingPriceRejection(tc.status, tc.message); got != tc.want {
				t.Fatalf("IsMissingPriceRejection(%d, %q) = %v, want %v", tc.status, tc.message, got, tc.want)
			}
		})
	}
}

// TestMissingPriceRejectionsUsesRepositoryAggregation 缺价清单来自既有错误日志的聚合。
func TestMissingPriceRejectionsUsesRepositoryAggregation(t *testing.T) {
	groupID := int64(7)
	stub := &missingPriceRepoStub{items: []*MissingPriceRejection{{
		Model:       "gpt-x",
		GroupID:     &groupID,
		GroupName:   "cn",
		Count:       3,
		FirstSeenAt: time.Now().Add(-time.Hour),
		LastSeenAt:  time.Now(),
		LastMessage: "no pricing available for model: gpt-x: pricing not found",
	}}}
	svc := &OpsService{opsRepo: stub}

	items, err := svc.MissingPriceRejections(context.Background(), nil)
	if err != nil {
		t.Fatalf("MissingPriceRejections 返回错误: %v", err)
	}
	if len(items) != 1 || items[0].Model != "gpt-x" || items[0].Count != 3 {
		t.Fatalf("聚合结果不符: %+v", items)
	}
	if stub.gotFilter == nil {
		t.Fatal("必须向仓储传入统计窗口")
	}
}

// TestMissingPriceRejectionFilterDefaults 默认窗口 24 小时、条数上限受控。
func TestMissingPriceRejectionFilterDefaults(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)

	defaults := normalizeMissingPriceRejectionFilter(nil, now)
	if got := defaults.EndTime; !got.Equal(now) {
		t.Fatalf("EndTime 应为 %v，实际 %v", now, got)
	}
	if got := defaults.StartTime; !got.Equal(now.Add(-DefaultMissingPriceRejectionWindow)) {
		t.Fatalf("StartTime 应为 %v，实际 %v", now.Add(-DefaultMissingPriceRejectionWindow), got)
	}
	if defaults.Limit != 50 {
		t.Fatalf("默认条数应为 50，实际 %d", defaults.Limit)
	}

	capped := normalizeMissingPriceRejectionFilter(&MissingPriceRejectionFilter{Limit: 1000}, now)
	if capped.Limit != 200 {
		t.Fatalf("条数上限应为 200，实际 %d", capped.Limit)
	}
}
